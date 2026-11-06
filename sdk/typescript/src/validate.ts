import type { Attributes, EventOverrides, Level, LevelInput } from "./types.js";

/** Server limits, mirrored from backend/internal/ingest/event.go. */
export const LIMITS = {
  maxBatchEvents: 1000,
  maxTags: 20,
  maxTagLen: 40,
  maxIdLen: 128,
  maxNameLen: 128,
  maxMessageBytes: 32 * 1024,
  maxAttrBytes: 64 * 1024,
  maxAttrDepth: 8,
  maxAttrKeys: 256,
  maxAttrKeyLen: 128,
  maxSourceFieldLen: 256,
  maxTraceIdLen: 64,
  maxSpanIdLen: 32,
  /** Stay under the default 1 MiB request limit of the server. */
  maxPayloadBytes: 900 * 1024,
} as const;

const encoder = new TextEncoder();

export function utf8Length(s: string): number {
  return encoder.encode(s).length;
}

/** Cut `s` to at most `max` UTF-8 bytes without splitting a character. */
export function truncateUtf8(s: string, max: number): string {
  if (s.length * 3 <= max) return s;
  if (utf8Length(s) <= max) return s;
  let bytes = 0;
  let end = 0;
  for (const ch of s) {
    const cp = ch.codePointAt(0) as number;
    const n = cp < 0x80 ? 1 : cp < 0x800 ? 2 : cp < 0x10000 ? 3 : 4;
    if (bytes + n > max) break;
    bytes += n;
    end += ch.length;
  }
  return s.slice(0, end);
}

const LEVELS: Record<string, Level> = {
  TRACE: "TRACE",
  DEBUG: "DEBUG",
  INFO: "INFO",
  NOTICE: "NOTICE",
  WARN: "WARN",
  WARNING: "WARN",
  ERROR: "ERROR",
  FATAL: "FATAL",
  CRITICAL: "FATAL",
};

export function normalizeLevel(level: LevelInput | string): Level | undefined {
  return typeof level === "string" ? LEVELS[level.toUpperCase()] : undefined;
}

export function newId(): string {
  const c = (globalThis as { crypto?: Crypto }).crypto;
  if (c && typeof c.randomUUID === "function") return c.randomUUID();
  const b = new Uint8Array(16);
  if (c && typeof c.getRandomValues === "function") c.getRandomValues(b);
  else for (let i = 0; i < b.length; i++) b[i] = Math.floor(Math.random() * 256);
  b[6] = ((b[6] as number) & 0x0f) | 0x40;
  b[8] = ((b[8] as number) & 0x3f) | 0x80;
  const h = Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

/** RFC 3339 string for a Date, string or epoch milliseconds; "" when the value is not a valid date. */
export function toTimestamp(ts: Date | string | number | undefined): string {
  if (ts === undefined) return new Date().toISOString();
  const d = ts instanceof Date ? ts : new Date(ts);
  if (Number.isNaN(d.getTime())) return "";
  return d.toISOString();
}

const SKIP = Symbol("skip");

interface AttrCtx {
  keys: number;
  keysCapped: boolean;
  keysRenamed: boolean;
  depthCapped: boolean;
  circular: boolean;
}

function sanitizeKey(key: string): string {
  const k = key.replace(/[^A-Za-z0-9_-]/gu, "_").slice(0, LIMITS.maxAttrKeyLen);
  return k === "" ? "_" : k;
}

function sanitizeValue(v: unknown, depth: number, ctx: AttrCtx, ancestors: Set<object>, viaToJSON = false): unknown {
  switch (typeof v) {
    case "string":
    case "boolean":
      return v;
    case "number":
      return Number.isFinite(v) ? v : null;
    case "bigint":
      return v.toString();
    case "undefined":
    case "function":
    case "symbol":
      return SKIP;
  }
  if (v === null) return null;
  const obj = v as object;
  if (obj instanceof Date) return Number.isNaN(obj.getTime()) ? null : obj.toISOString();
  if (ancestors.has(obj)) {
    ctx.circular = true;
    return "[circular]";
  }
  if (!viaToJSON && typeof (obj as { toJSON?: unknown }).toJSON === "function") {
    try {
      return sanitizeValue((obj as { toJSON: () => unknown }).toJSON(), depth, ctx, ancestors, true);
    } catch {
      return SKIP;
    }
  }
  // Scalars may sit at depth 8; a container there would hold children at depth 9.
  if (depth >= LIMITS.maxAttrDepth) {
    ctx.depthCapped = true;
    return "[max depth]";
  }
  ancestors.add(obj);
  try {
    if (Array.isArray(obj)) {
      return obj.map((item) => {
        const out = sanitizeValue(item, depth + 1, ctx, ancestors);
        return out === SKIP ? null : out;
      });
    }
    if (obj instanceof Set) return sanitizeValue(Array.from(obj), depth, ctx, ancestors, true);
    let source: Record<string, unknown>;
    if (obj instanceof Error) {
      source = { name: obj.name, message: obj.message, stack: obj.stack };
      for (const k of Object.keys(obj)) source[k] = (obj as unknown as Record<string, unknown>)[k];
    } else if (obj instanceof Map) {
      source = {};
      for (const [k, val] of obj) source[String(k)] = val;
    } else {
      source = obj as Record<string, unknown>;
    }
    const out: Record<string, unknown> = {};
    for (const key of Object.keys(source)) {
      if (ctx.keys >= LIMITS.maxAttrKeys) {
        ctx.keysCapped = true;
        break;
      }
      const val = sanitizeValue(source[key], depth + 1, ctx, ancestors);
      if (val === SKIP) continue;
      ctx.keys++;
      const safe = sanitizeKey(key);
      if (safe !== key) ctx.keysRenamed = true;
      out[safe] = val;
    }
    return out;
  } finally {
    ancestors.delete(obj);
  }
}

/**
 * Make attributes acceptable to the server: valid keys, depth and key count within limits, at most 64 KiB.
 * Returns undefined when nothing is left to send. Problems are appended to `problems`.
 */
export function sanitizeAttributes(attrs: unknown, problems: string[]): Attributes | undefined {
  if (attrs === undefined || attrs === null) return undefined;
  if (typeof attrs !== "object" || Array.isArray(attrs)) {
    problems.push("attributes must be an object; they were dropped");
    return undefined;
  }
  const ctx: AttrCtx = { keys: 0, keysCapped: false, keysRenamed: false, depthCapped: false, circular: false };
  const out = sanitizeValue(attrs, 1, ctx, new Set());
  if (out === SKIP || typeof out !== "object" || out === null || Array.isArray(out)) return undefined;
  if (ctx.keysRenamed) problems.push("attribute keys outside A-Z a-z 0-9 _ - were rewritten with _");
  if (ctx.keysCapped) problems.push(`attributes have more than ${LIMITS.maxAttrKeys} keys; the rest were dropped`);
  if (ctx.depthCapped) problems.push(`attributes nest deeper than ${LIMITS.maxAttrDepth} levels; deeper values were replaced`);
  if (ctx.circular) problems.push("attributes contain a circular reference; it was replaced");
  const result = out as Attributes;
  if (Object.keys(result).length === 0) return undefined;
  if (utf8Length(JSON.stringify(result)) > LIMITS.maxAttrBytes) {
    problems.push(`attributes are larger than ${LIMITS.maxAttrBytes} bytes; they were dropped`);
    return { _dropped: "attributes exceeded 64 KiB" };
  }
  return result;
}

function sanitizeToken(s: string, forbidden: RegExp, max: number, field: string, problems: string[]): string {
  let out = s;
  if (out.length > max) {
    out = out.slice(0, max);
    problems.push(`${field} was longer than ${max} characters and was truncated`);
  }
  const clean = out.replace(forbidden, "_");
  if (clean !== out) problems.push(`${field} contained characters the server rejects; they were replaced with _`);
  return clean;
}

function truncateField(s: string, max: number, field: string, problems: string[]): string {
  const t = truncateUtf8(s, max);
  if (t.length !== s.length) problems.push(`${field} was longer than ${max} bytes and was truncated`);
  return t;
}

export interface BuildDefaults {
  stream?: string | undefined;
  environment?: string | undefined;
  service?: string | undefined;
  host?: string | undefined;
}

export interface BuiltEvent {
  /** JSON of the event, ready to be joined into a request body. */
  json: string;
  /** UTF-8 size of `json`. */
  bytes: number;
  /** Idempotency key sent with the event. */
  id: string;
  problems: string[];
}

const HEX = /^[0-9a-fA-F]*$/;

/**
 * Build the wire form of one event, applying client defaults and mirroring the server limits.
 * `stream` and `environment` equal to the client defaults are left out: they travel once per batch.
 */
export function buildEvent(
  defaults: BuildDefaults,
  level: LevelInput | string,
  event: string,
  message: string | undefined,
  attributes: unknown,
  overrides: EventOverrides | undefined,
): BuiltEvent {
  const problems: string[] = [];
  const o = overrides ?? {};

  let lvl = normalizeLevel(level);
  if (!lvl) {
    problems.push(`unknown level ${JSON.stringify(String(level)).slice(0, 40)}; INFO was used`);
    lvl = "INFO";
  }

  let id = o.id === undefined || o.id === "" ? newId() : sanitizeToken(String(o.id), /[^A-Za-z0-9._:-]/g, LIMITS.maxIdLen, "id", problems);
  if (id === "") id = newId();

  let timestamp = toTimestamp(o.timestamp);
  if (timestamp === "") {
    problems.push("timestamp is not a valid date; the current time was used");
    timestamp = new Date().toISOString();
  }

  const wire: Record<string, unknown> = { id, timestamp, level: lvl };

  const eventName = sanitizeToken(String(event ?? ""), /[^A-Za-z0-9._:/-]/g, LIMITS.maxNameLen, "event", problems);
  if (eventName !== "") wire.event = eventName;

  if (message !== undefined && message !== null) {
    const text = typeof message === "string" ? message : String(message);
    wire.message = truncateField(text, LIMITS.maxMessageBytes, "message", problems);
  }

  if (o.stream !== undefined && o.stream !== defaults.stream) wire.stream = o.stream;
  if (o.environment !== undefined && o.environment !== defaults.environment) wire.environment = o.environment;

  const service = o.service ?? defaults.service;
  if (service) wire.service = truncateField(service, LIMITS.maxSourceFieldLen, "service", problems);
  const host = o.host ?? defaults.host;
  if (host) wire.host = truncateField(host, LIMITS.maxSourceFieldLen, "host", problems);
  if (o.resource) wire.resource = truncateField(o.resource, LIMITS.maxSourceFieldLen, "resource", problems);

  if (o.trace_id) {
    if (o.trace_id.length <= LIMITS.maxTraceIdLen && HEX.test(o.trace_id)) wire.trace_id = o.trace_id;
    else problems.push("trace_id must be hexadecimal of at most 64 characters; it was dropped");
  }
  if (o.span_id) {
    if (o.span_id.length <= LIMITS.maxSpanIdLen && HEX.test(o.span_id)) wire.span_id = o.span_id;
    else problems.push("span_id must be hexadecimal of at most 32 characters; it was dropped");
  }

  if (o.tags !== undefined) {
    const tags = [...new Set(o.tags.map((tag) => String(tag).trim().toLowerCase()))];
    wire.tags = tags.filter((tag) => /^[a-z0-9][a-z0-9._:/-]*$/.test(tag) && tag.length <= LIMITS.maxTagLen).slice(0, LIMITS.maxTags);
    if (tags.length > LIMITS.maxTags || (wire.tags as string[]).length !== tags.length) problems.push("tags must contain at most 20 lowercase values of 1 to 40 characters; invalid values were dropped");
  }
  let eventAttributes = attributes;
  if (o.actor !== undefined || o.target !== undefined) {
    eventAttributes = { ...(typeof attributes === "object" && attributes !== null && !Array.isArray(attributes) ? attributes : {}),
      ...(o.actor === undefined ? {} : { actor: o.actor }),
      ...(o.target === undefined ? {} : { target: o.target }) };
  }
  const attrs = sanitizeAttributes(eventAttributes, problems);
  if (attrs) wire.attributes = attrs;

  const json = JSON.stringify(wire);
  return { json, bytes: utf8Length(json), id, problems };
}
