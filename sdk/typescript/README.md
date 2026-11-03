# @ervisio/logs

TypeScript client for the Ervisio Logs ingest API (`POST /api/v1/ingest/batch`).
It batches events, retries what is worth retrying, keeps a bounded queue and has no runtime dependencies.
Works on Node 18 or later and on runtimes with a global `fetch` (browsers, Deno, Bun, edge workers).

## Install

```sh
npm install @ervisio/logs
```

## Quick start

```ts
import { ErvisioLogs } from "@ervisio/logs";

const logs = new ErvisioLogs({
  baseUrl: "https://logs.example.com",
  token: process.env.ERVISIO_LOGS_TOKEN!, // API token with logs:ingest
  stream: "default",
  environment: "production",
  service: "checkout",
  onError: (err) => console.error(err.kind, err.message),
});

logs.info("order.created", "order accepted", { order_id: 42, items: 3 });
logs.error("payment.failed", "card declined", { reason: "insufficient_funds" });

// Before the process exits:
await logs.close();
```

`log(level, event, message?, attributes?, overrides?)` is the general form. `trace`, `debug`, `info`,
`notice`, `warn`, `error` and `fatal` take the same arguments without the level. `overrides` can set
`id`, `stream`, `environment`, `timestamp`, `service`, `host`, `resource`, `trace_id` and `span_id` for one event.
Every event gets an `id` (a random UUID unless you pass one), which makes resends idempotent on the server.

`log()` never throws and never waits for the network. `flush()` sends what is queued and resolves with
`{ accepted, rejected, failed, requests, errors }`. `close()` flushes and stops the timer; events logged
after it are dropped. `stats()` returns `{ queued, sent, rejected, dropped, retries, failed }`.

## Options

| Option | Default | Meaning |
| --- | --- | --- |
| `baseUrl` | required | Server URL, for example `https://logs.example.com`. |
| `token` | required | API token with `logs:ingest`. Read it from the environment. |
| `stream` | server default | Default stream, sent once per batch. |
| `environment` | none | Default environment, sent once per batch. |
| `service`, `host` | none | Default `service` and `host` of every event. |
| `maxBatchSize` | 500 | Events per request (1 to 1000). A full batch is sent without waiting for the timer. |
| `flushIntervalMs` | 2000 | Background flush period. `0` turns the timer off. The timer is `unref`ed on Node. |
| `maxQueue` | 10000 | Events held while waiting. When full, the **oldest** are dropped and counted in `stats().dropped`. |
| `maxRetries` | 8 | Retries per request after the first attempt. |
| `gzip` | false | Compress bodies with `CompressionStream` (Node 18+, modern browsers). Ignored if unavailable. |
| `onError` | none | Called with an `ErvisioError` for every problem. If it throws, the exception is swallowed. |
| `fetch` | global `fetch` | Custom fetch, for proxies or tests. |
| `timeoutMs` | 30000 | Per-request timeout. A timeout counts as a network error. |
| `retryBaseDelayMs`, `retryMaxDelayMs` | 500, 30000 | Backoff bounds. |

`ErvisioError.kind` is one of `validation`, `rejected`, `http`, `retries_exhausted`, `dropped`.
Without `onError` problems are silent; `stats()` and the `errors` of each `FlushResult` still show them.

## Retry semantics

Only one flush runs at a time. A request is one batch of at most `maxBatchSize` events and about 900 KiB.

| Response | What the SDK does |
| --- | --- |
| 202 | Done. Events listed in `errors` were **rejected** as invalid: they are counted in `rejected`, reported with `onError` and never retried. |
| 422 | No event was accepted. Counted as rejected, not retried. |
| 413 | The body was too large. The batch is split in half and each half is sent again. A single event that still gets 413 is dropped. |
| 429, 503 | Retried after the `Retry-After` seconds (or date) the server sent, up to 5 minutes. Without the header, backoff below. |
| Other 5xx, 408, network error, timeout | Retried with exponential backoff and full jitter: a random delay between 0 and `min(retryMaxDelayMs, retryBaseDelayMs * 2^attempt)`. |
| 400, 401, 403, 415 and other 4xx | Not retried. The batch is discarded and reported as `http`. |

When the retries run out the batch is discarded, counted in `stats().failed` and reported as `retries_exhausted`.

## Client-side validation

The SDK mirrors the server limits so one bad field does not cost you the event. Each fix is reported once per
event through `onError` with kind `validation`.

- `message` is cut to 32768 bytes (UTF-8). `event`, `id`, `service`, `host`, `resource` are cut to their limits;
  characters the server does not accept in `event` and `id` become `_`.
- Attribute keys must match `^[A-Za-z0-9_-]+$`: other characters become `_` (dots included). At most 256 keys,
  8 levels, 64 KiB. Deeper values become `"[max depth]"`, cycles `"[circular]"`; `Date` becomes an ISO string,
  `bigint` a string, functions and `undefined` are skipped. Over 64 KiB, the attributes are replaced by a marker.
- `trace_id` and `span_id` that are not hexadecimal are dropped.
- Unknown levels become `INFO`. `warning` is `WARN` and `critical` is `FATAL`.

The server also checks that the stream and environment exist and that the timestamp is not too old or in the future.
Those rejections come back as `rejected` errors.

## Security

An ingest token can only write events, but anyone who holds it can fill your project with junk and use up
your quota. Do not ship it in a browser bundle used by untrusted people. For browser or mobile apps, send events
to your own backend (or a proxy you control that adds the token) and let that call the SDK. Never hardcode the token:
read it from the environment or a secret store.

## Development

```sh
npm install
npm run typecheck   # tsc --noEmit
npm run build       # ESM in dist/esm, CommonJS in dist/cjs, both with .d.ts
npm test            # vitest
```
