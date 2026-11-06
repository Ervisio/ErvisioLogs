/** Event severity, as accepted by the ingest API. */
export type Level = "TRACE" | "DEBUG" | "INFO" | "NOTICE" | "WARN" | "ERROR" | "FATAL";

/** Any spelling `log()` accepts: the canonical levels in any case, plus WARNING and CRITICAL. */
export type LevelInput =
  | Level
  | Lowercase<Level>
  | "WARNING"
  | "warning"
  | "CRITICAL"
  | "critical";

/** Free-form event attributes. Sanitized before sending (see the README). */
export interface Entity {
  type: string;
  id: string | number;
  name?: string;
}

export type Attributes = Record<string, unknown>;

/** The wire format of one event (`InEvent` in api/openapi.yaml). */
export interface InEvent {
  tags?: string[];
  id?: string;
  stream?: string;
  environment?: string;
  timestamp?: string;
  level?: Level;
  event?: string;
  message?: string;
  service?: string;
  host?: string;
  resource?: string;
  trace_id?: string;
  span_id?: string;
  attributes?: Attributes;
}

/** Per-event values that replace the client defaults. */
export interface EventOverrides {
  tags?: string[];
  actor?: Entity;
  target?: Entity;
  /** Idempotency key. Defaults to a random UUID. Keep it stable if you resend the same event. */
  id?: string;
  stream?: string;
  environment?: string;
  /** Defaults to the time of the `log()` call. */
  timestamp?: Date | string | number;
  service?: string;
  host?: string;
  resource?: string;
  /** Hexadecimal, at most 64 characters. */
  trace_id?: string;
  /** Hexadecimal, at most 32 characters. */
  span_id?: string;
}

/** One rejected event in an `IngestResult`. */
export interface IngestResultError {
  /** Position of the event in the request. */
  index: number;
  reason: string;
}

/** Body of a 202 (and sometimes 422) response. */
export interface IngestResult {
  request_id: string;
  accepted: number;
  rejected: number;
  status: "durably_queued" | "rejected";
  errors: IngestResultError[];
}

/** A rejected event, with the idempotency key the SDK assigned. */
export interface RejectedEvent {
  index: number;
  id?: string;
  reason: string;
}

export type ErrorKind =
  /** An event was changed or partly dropped before sending (truncated message, bad key, ...). */
  | "validation"
  /** The server accepted the request but rejected some or all events. Not retried. */
  | "rejected"
  /** A non-retryable HTTP error (400, 401, 403, 415, ...). The batch was discarded. */
  | "http"
  /** A request kept failing and the retries ran out. The batch was discarded. */
  | "retries_exhausted"
  /** Events were discarded: queue overflow, a single event the server found too large, or logging after close. */
  | "dropped";

export interface ErvisioErrorInit {
  status?: number;
  requestId?: string;
  events?: number;
  rejected?: RejectedEvent[];
  cause?: unknown;
}

/** Passed to `onError` and collected in `FlushResult.errors`. */
export class ErvisioError extends Error {
  readonly kind: ErrorKind;
  /** HTTP status of the last response, when there was one. */
  readonly status?: number;
  /** `request_id` from the server, when there was one. */
  readonly requestId?: string;
  /** Number of events concerned. */
  readonly events?: number;
  /** Per-event reasons, for kind "rejected". */
  readonly rejected?: RejectedEvent[];

  constructor(kind: ErrorKind, message: string, init: ErvisioErrorInit = {}) {
    super(message);
    this.name = "ErvisioError";
    this.kind = kind;
    if (init.status !== undefined) this.status = init.status;
    if (init.requestId !== undefined) this.requestId = init.requestId;
    if (init.events !== undefined) this.events = init.events;
    if (init.rejected !== undefined) this.rejected = init.rejected;
    if (init.cause !== undefined) (this as { cause?: unknown }).cause = init.cause;
  }
}

/** Minimal `fetch` shape the client needs. The global `fetch` satisfies it. */
export type FetchLike = (input: string, init: RequestInit) => Promise<Response>;

export interface ErvisioLogsOptions {
  /** Base URL of the server, for example `https://logs.example.com`. No trailing path. */
  baseUrl: string;
  /** API token with `logs:ingest`. Read it from the environment, never hardcode it. */
  token: string;
  /** Default stream for events that do not set one. */
  stream?: string;
  /** Default environment. */
  environment?: string;
  /** Default `service` field of every event. */
  service?: string;
  /** Default `host` field of every event. */
  host?: string;
  /** Events per request, 1 to 1000. Default 500. */
  maxBatchSize?: number;
  /** Background flush period in milliseconds. 0 disables the timer. Default 2000. */
  flushIntervalMs?: number;
  /** Events held while waiting to be sent. When full, the oldest are dropped. Default 10000. */
  maxQueue?: number;
  /** Retries per request after the first attempt. Default 8. */
  maxRetries?: number;
  /** Compress request bodies with gzip when the runtime has `CompressionStream`. Default false. */
  gzip?: boolean;
  /** Called for every problem. Exceptions it throws are swallowed. */
  onError?: (error: ErvisioError) => void;
  /** Custom `fetch`, for proxies, tests or runtimes without a global one. */
  fetch?: FetchLike;
  /** Per-request timeout in milliseconds. Default 30000. */
  timeoutMs?: number;
  /** First backoff step in milliseconds. Default 500. */
  retryBaseDelayMs?: number;
  /** Backoff ceiling in milliseconds. Default 30000. */
  retryMaxDelayMs?: number;
  /** Replaces the timer used between retries. Meant for tests. */
  sleep?: (ms: number) => Promise<void>;
  /** Replaces `Math.random` in the backoff jitter. Meant for tests. */
  random?: () => number;
}

export interface Stats {
  /** Events waiting in the queue or in a request that is being sent. */
  queued: number;
  /** Events the server accepted. */
  sent: number;
  /** Events the server rejected as invalid. */
  rejected: number;
  /** Events discarded before they reached the server (queue overflow, 413 on a single event, logging after close). */
  dropped: number;
  /** Request retries made so far. */
  retries: number;
  /** Events given up on after a non-retryable HTTP error or exhausted retries. */
  failed: number;
}

export interface FlushResult {
  /** Events the server accepted during this flush. */
  accepted: number;
  /** Events the server rejected during this flush. */
  rejected: number;
  /** Events given up on during this flush. */
  failed: number;
  /** HTTP requests made, retries included. */
  requests: number;
  /** Problems met during this flush (at most 100). */
  errors: ErvisioError[];
}
