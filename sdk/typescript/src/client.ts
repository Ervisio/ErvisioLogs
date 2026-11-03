import {
  ErvisioError,
  type EventOverrides,
  type FetchLike,
  type FlushResult,
  type IngestResult,
  type LevelInput,
  type RejectedEvent,
  type ErvisioLogsOptions,
  type Stats,
  type Attributes,
} from "./types.js";
import { buildEvent, LIMITS } from "./validate.js";

const MAX_RETRY_AFTER_MS = 5 * 60 * 1000;
const MAX_ERRORS_PER_FLUSH = 100;
const MAX_ERROR_BODY = 64 * 1024;

interface Item {
  id: string;
  json: string;
  bytes: number;
}

type Outcome =
  | { type: "ok"; result: IngestResult | undefined }
  | { type: "split" }
  | { type: "reject"; status: number; result: IngestResult | undefined; message: string }
  | { type: "fatal"; status: number; message: string }
  | { type: "retry"; message: string; status?: number; delayMs?: number; cause?: unknown };

interface Resolved {
  baseUrl: string;
  token: string;
  stream: string | undefined;
  environment: string | undefined;
  service: string | undefined;
  host: string | undefined;
  maxBatchSize: number;
  flushIntervalMs: number;
  maxQueue: number;
  maxRetries: number;
  gzip: boolean;
  timeoutMs: number;
  retryBaseDelayMs: number;
  retryMaxDelayMs: number;
}

function clampInt(value: number | undefined, def: number, min: number, max: number): number {
  if (value === undefined || !Number.isFinite(value)) return def;
  return Math.min(max, Math.max(min, Math.floor(value)));
}

/** Parse a Retry-After header (delta-seconds or HTTP date) into milliseconds. */
export function parseRetryAfter(value: string | null | undefined, now: number = Date.now()): number | undefined {
  if (!value) return undefined;
  const t = value.trim();
  if (/^\d+(\.\d+)?$/.test(t)) return Math.min(MAX_RETRY_AFTER_MS, Number(t) * 1000);
  const when = Date.parse(t);
  if (Number.isNaN(when)) return undefined;
  return Math.min(MAX_RETRY_AFTER_MS, Math.max(0, when - now));
}

function parseIngestResult(text: string): IngestResult | undefined {
  try {
    const v = JSON.parse(text) as Partial<IngestResult> | null;
    if (v && typeof v === "object" && typeof v.accepted === "number" && typeof v.rejected === "number") {
      return {
        request_id: typeof v.request_id === "string" ? v.request_id : "",
        accepted: v.accepted,
        rejected: v.rejected,
        status: v.status === "rejected" ? "rejected" : "durably_queued",
        errors: Array.isArray(v.errors) ? v.errors : [],
      };
    }
  } catch {
    /* not JSON */
  }
  return undefined;
}

/** Best-effort summary of an application/problem+json body or plain text. */
function describeBody(text: string): string {
  if (!text) return "";
  try {
    const v = JSON.parse(text) as { title?: unknown; detail?: unknown; message?: unknown };
    const parts = [v.title, v.detail ?? v.message].filter((x): x is string => typeof x === "string" && x !== "");
    if (parts.length) return parts.join(": ").slice(0, 300);
  } catch {
    /* not JSON */
  }
  return text.slice(0, 300);
}

export class ErvisioLogs {
  private readonly o: Resolved;
  private readonly url: string;
  private readonly fetchFn: FetchLike;
  private readonly sleepFn: (ms: number) => Promise<void>;
  private readonly random: () => number;
  private readonly onErrorCb: ((error: ErvisioError) => void) | undefined;

  private queue: Item[] = [];
  private inflightCount = 0;
  private inflight: Promise<FlushResult> | null = null;
  private followUp: Promise<FlushResult> | null = null;
  private timer: ReturnType<typeof setInterval> | undefined;
  private closed = false;
  private sent = 0;
  private rejected = 0;
  private dropped = 0;
  private retries = 0;
  private failed = 0;

  constructor(options: ErvisioLogsOptions) {
    if (!options || typeof options.baseUrl !== "string" || options.baseUrl.trim() === "") {
      throw new TypeError("ErvisioLogs: baseUrl is required");
    }
    if (typeof options.token !== "string" || options.token === "") {
      throw new TypeError("ErvisioLogs: token is required");
    }
    this.o = {
      baseUrl: options.baseUrl.trim().replace(/\/+$/, ""),
      token: options.token,
      stream: options.stream,
      environment: options.environment,
      service: options.service,
      host: options.host,
      maxBatchSize: clampInt(options.maxBatchSize, 500, 1, LIMITS.maxBatchEvents),
      flushIntervalMs: clampInt(options.flushIntervalMs, 2000, 0, 24 * 3600 * 1000),
      maxQueue: clampInt(options.maxQueue, 10000, 1, 10_000_000),
      maxRetries: clampInt(options.maxRetries, 8, 0, 100),
      gzip: options.gzip ?? false,
      timeoutMs: clampInt(options.timeoutMs, 30000, 1, 3600 * 1000),
      retryBaseDelayMs: clampInt(options.retryBaseDelayMs, 500, 0, 3600 * 1000),
      retryMaxDelayMs: clampInt(options.retryMaxDelayMs, 30000, 0, 3600 * 1000),
    };
    this.url = `${this.o.baseUrl}/api/v1/ingest/batch`;
    const globalFetch = (globalThis as { fetch?: FetchLike }).fetch;
    const f = options.fetch ?? (globalFetch ? globalFetch.bind(globalThis) : undefined);
    if (!f) throw new TypeError("ErvisioLogs: no global fetch; pass options.fetch");
    this.fetchFn = f;
    this.sleepFn = options.sleep ?? ((ms) => new Promise<void>((resolve) => setTimeout(resolve, ms)));
    this.random = options.random ?? Math.random;
    this.onErrorCb = options.onError;

    if (this.o.flushIntervalMs > 0) {
      const t = setInterval(() => {
        if (this.queue.length > 0) void this.flush();
      }, this.o.flushIntervalMs);
      (t as unknown as { unref?: () => void }).unref?.();
      this.timer = t;
    }
  }

  /** Queue one event. Never throws. */
  log(level: LevelInput, event: string, message?: string, attributes?: Attributes, overrides?: EventOverrides): void {
    try {
      if (this.closed) {
        this.dropped++;
        this.report(new ErvisioError("dropped", "event dropped: the client is closed", { events: 1 }));
        return;
      }
      const built = buildEvent(
        { stream: this.o.stream, environment: this.o.environment, service: this.o.service, host: this.o.host },
        level,
        event,
        message,
        attributes,
        overrides,
      );
      if (built.problems.length > 0) {
        this.report(
          new ErvisioError("validation", `event ${built.id}: ${built.problems.join("; ")}`, { events: 1 }),
        );
      }
      this.queue.push({ id: built.id, json: built.json, bytes: built.bytes });
      const over = this.queue.length - this.o.maxQueue;
      if (over > 0) {
        this.queue.splice(0, over);
        this.dropped += over;
        this.report(
          new ErvisioError("dropped", `queue is full (${this.o.maxQueue}); dropped the ${over} oldest event(s)`, {
            events: over,
          }),
        );
      }
      if (this.queue.length >= this.o.maxBatchSize && !this.inflight) void this.flush();
    } catch (err) {
      this.failed++;
      this.report(new ErvisioError("validation", `event could not be queued: ${String(err)}`, { events: 1, cause: err }));
    }
  }

  trace(event: string, message?: string, attributes?: Attributes, overrides?: EventOverrides): void {
    this.log("TRACE", event, message, attributes, overrides);
  }
  debug(event: string, message?: string, attributes?: Attributes, overrides?: EventOverrides): void {
    this.log("DEBUG", event, message, attributes, overrides);
  }
  info(event: string, message?: string, attributes?: Attributes, overrides?: EventOverrides): void {
    this.log("INFO", event, message, attributes, overrides);
  }
  notice(event: string, message?: string, attributes?: Attributes, overrides?: EventOverrides): void {
    this.log("NOTICE", event, message, attributes, overrides);
  }
  warn(event: string, message?: string, attributes?: Attributes, overrides?: EventOverrides): void {
    this.log("WARN", event, message, attributes, overrides);
  }
  error(event: string, message?: string, attributes?: Attributes, overrides?: EventOverrides): void {
    this.log("ERROR", event, message, attributes, overrides);
  }
  fatal(event: string, message?: string, attributes?: Attributes, overrides?: EventOverrides): void {
    this.log("FATAL", event, message, attributes, overrides);
  }

  /** Counters since the client was created. */
  stats(): Stats {
    return {
      queued: this.queue.length + this.inflightCount,
      sent: this.sent,
      rejected: this.rejected,
      dropped: this.dropped,
      retries: this.retries,
      failed: this.failed,
    };
  }

  /**
   * Send everything queued now. Only one flush runs at a time: a call made while one is in flight
   * waits for it, then sends what was queued in the meantime. Resolves; never rejects.
   */
  flush(): Promise<FlushResult> {
    if (this.inflight) {
      if (!this.followUp) {
        this.followUp = this.inflight.then(() => {
          this.followUp = null;
          // Re-check: a size-triggered flush may have started in the meantime.
          return this.flush();
        });
      }
      return this.followUp;
    }
    return this.start();
  }

  /** Flush what is left and stop the timer. Later `log()` calls are dropped. */
  async close(): Promise<void> {
    this.closed = true;
    if (this.timer !== undefined) {
      clearInterval(this.timer);
      this.timer = undefined;
    }
    await this.flush();
  }

  private start(): Promise<FlushResult> {
    const p: Promise<FlushResult> = this.drain().finally(() => {
      if (this.inflight === p) this.inflight = null;
      if (!this.closed && !this.inflight && this.queue.length >= this.o.maxBatchSize) void this.flush();
    });
    this.inflight = p;
    return p;
  }

  private report(err: ErvisioError, result?: FlushResult): void {
    if (result && result.errors.length < MAX_ERRORS_PER_FLUSH) result.errors.push(err);
    if (!this.onErrorCb) return;
    try {
      this.onErrorCb(err);
    } catch {
      /* a failing callback must not break logging */
    }
  }

  private takeBatch(limit: number): Item[] {
    const max = Math.min(this.o.maxBatchSize, limit);
    let bytes = 0;
    let n = 0;
    while (n < this.queue.length && n < max) {
      const item = this.queue[n] as Item;
      if (n > 0 && bytes + item.bytes + 1 > LIMITS.maxPayloadBytes) break;
      bytes += item.bytes + 1;
      n++;
    }
    return this.queue.splice(0, n);
  }

  private async drain(): Promise<FlushResult> {
    const result: FlushResult = { accepted: 0, rejected: 0, failed: 0, requests: 0, errors: [] };
    let budget = this.queue.length;
    while (budget > 0 && this.queue.length > 0) {
      const batch = this.takeBatch(budget);
      budget -= batch.length;
      this.inflightCount = batch.length;
      try {
        await this.deliver(batch, result);
      } catch (err) {
        this.failed += batch.length;
        result.failed += batch.length;
        this.report(
          new ErvisioError("http", `unexpected error while sending ${batch.length} event(s): ${String(err)}`, {
            events: batch.length,
            cause: err,
          }),
          result,
        );
      } finally {
        this.inflightCount = 0;
      }
    }
    return result;
  }

  private async deliver(items: Item[], result: FlushResult): Promise<void> {
    for (let attempt = 0; ; attempt++) {
      const out = await this.attempt(items);
      result.requests++;
      switch (out.type) {
        case "ok": {
          const accepted = out.result ? out.result.accepted : items.length;
          const rejected = out.result ? out.result.rejected : 0;
          this.sent += accepted;
          result.accepted += accepted;
          if (rejected > 0 && out.result) this.noteRejected(items, out.result, 202, result);
          return;
        }
        case "split": {
          if (items.length === 1) {
            this.dropped += 1;
            this.report(
              new ErvisioError("dropped", "a single event is larger than the server accepts (413); it was dropped", {
                status: 413,
                events: 1,
              }),
              result,
            );
            return;
          }
          const mid = Math.ceil(items.length / 2);
          await this.deliver(items.slice(0, mid), result);
          await this.deliver(items.slice(mid), result);
          return;
        }
        case "reject": {
          if (out.result) {
            this.noteRejected(items, out.result, out.status, result);
          } else {
            this.rejected += items.length;
            result.rejected += items.length;
            this.report(new ErvisioError("rejected", out.message, { status: out.status, events: items.length }), result);
          }
          return;
        }
        case "fatal": {
          this.failed += items.length;
          result.failed += items.length;
          this.report(new ErvisioError("http", out.message, { status: out.status, events: items.length }), result);
          return;
        }
        case "retry": {
          if (attempt >= this.o.maxRetries) {
            this.failed += items.length;
            result.failed += items.length;
            const init = { events: items.length, cause: out.cause, ...(out.status !== undefined ? { status: out.status } : {}) };
            this.report(
              new ErvisioError(
                "retries_exhausted",
                `giving up on ${items.length} event(s) after ${attempt + 1} attempt(s): ${out.message}`,
                init,
              ),
              result,
            );
            return;
          }
          this.retries++;
          await this.sleepFn(out.delayMs ?? this.backoff(attempt));
        }
      }
    }
  }

  /** Full jitter: a random delay in [0, min(max, base * 2^attempt)). */
  private backoff(attempt: number): number {
    const ceiling = Math.min(this.o.retryMaxDelayMs, this.o.retryBaseDelayMs * 2 ** Math.min(attempt, 30));
    return Math.floor(this.random() * ceiling);
  }

  private noteRejected(items: Item[], res: IngestResult, status: number, result: FlushResult): void {
    this.rejected += res.rejected;
    result.rejected += res.rejected;
    const details: RejectedEvent[] = res.errors.map((e) => {
      const d: RejectedEvent = { index: e.index, reason: e.reason };
      const id = items[e.index]?.id;
      if (id !== undefined) d.id = id;
      return d;
    });
    const first = details[0];
    const sample = first ? ` (first: ${first.id ?? `#${first.index}`}: ${first.reason})` : "";
    this.report(
      new ErvisioError("rejected", `server rejected ${res.rejected} of ${items.length} event(s)${sample}`, {
        status,
        events: res.rejected,
        rejected: details,
        ...(res.request_id ? { requestId: res.request_id } : {}),
      }),
      result,
    );
  }

  private async encode(items: Item[]): Promise<{ body: string | ArrayBuffer; headers: Record<string, string> }> {
    const head: string[] = [];
    if (this.o.stream) head.push(`"stream":${JSON.stringify(this.o.stream)}`);
    if (this.o.environment) head.push(`"environment":${JSON.stringify(this.o.environment)}`);
    head.push(`"events":[${items.map((i) => i.json).join(",")}]`);
    const text = `{${head.join(",")}}`;
    const headers: Record<string, string> = {
      Authorization: `Bearer ${this.o.token}`,
      "Content-Type": "application/json",
      Accept: "application/json",
    };
    const g = globalThis as { CompressionStream?: typeof CompressionStream };
    if (this.o.gzip && typeof g.CompressionStream === "function") {
      const stream = new Blob([text]).stream().pipeThrough(new g.CompressionStream("gzip"));
      headers["Content-Encoding"] = "gzip";
      return { body: await new Response(stream).arrayBuffer(), headers };
    }
    return { body: text, headers };
  }

  private async attempt(items: Item[]): Promise<Outcome> {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), this.o.timeoutMs);
    let status = 0;
    let retryAfter: number | undefined;
    let text = "";
    try {
      const { body, headers } = await this.encode(items);
      const res = await this.fetchFn(this.url, { method: "POST", headers, body, signal: ctrl.signal });
      status = res.status;
      retryAfter = parseRetryAfter(res.headers.get("Retry-After"));
      text = (await res.text().catch(() => "")).slice(0, MAX_ERROR_BODY);
    } catch (err) {
      const why = ctrl.signal.aborted ? `request timed out after ${this.o.timeoutMs} ms` : `network error: ${String(err)}`;
      return { type: "retry", message: why, cause: err };
    } finally {
      clearTimeout(timer);
    }

    if (status === 202 || status === 200) return { type: "ok", result: parseIngestResult(text) };

    const detail = describeBody(text);
    const message = `HTTP ${status}${detail ? `: ${detail}` : ""}`;
    switch (status) {
      case 413:
        return { type: "split" };
      case 422:
        return { type: "reject", status, result: parseIngestResult(text), message };
      case 429:
      case 503:
        return { type: "retry", message, status, ...(retryAfter !== undefined ? { delayMs: retryAfter } : {}) };
      case 408:
        return { type: "retry", message, status };
    }
    if (status >= 500) return { type: "retry", message, status };
    return { type: "fatal", status, message };
  }
}
