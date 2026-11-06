# sdk (TypeScript client)
Purpose: `@ervisio/logs` npm package (sdk/typescript, v0.1.0, node>=18, dual ESM+CJS) that batches events to the ingest API.
Entry points: sdk/typescript/src/index.ts (exports), src/client.ts:ErvisioLogs (log/flush/close/stats, deliver, attempt, encode), src/validate.ts:buildEvent + LIMITS, src/types.ts (ErvisioLogsOptions, ErvisioError, IngestResult).
Flow: log() -> buildEvent (validate/sanitize, JSON-serialize once into Item{id,json,bytes}) -> bounded queue (maxQueue, drops oldest/new -> `dropped`)
  -> setInterval flush (flushIntervalMs 2000) or when queue >= maxBatchSize (500) -> drain() takes batches (also capped by byte budget under 1 MiB)
  -> POST `${baseUrl}/api/v1/ingest/batch`, `Authorization: Bearer <token>`, JSON (optional gzip, Content-Encoding) -> deliver() switch on Outcome:
  200/202 ok (parse IngestResult accepted/rejected) | 413 split batch in half recursively, single event -> dropped | 4xx reject/fatal (401/403 fatal, no retry)
  | 429/5xx/network retry with exp backoff+jitter (retryBaseDelayMs 500..retryMaxDelayMs 30000, maxRetries 8), honours Retry-After capped 5 min.
Invariants:
- Wire format must match backend ingest (api/openapi.yaml, backend/internal/ingest): body {environment?, events:[...]}, response {request_id, accepted, rejected, status:"durably_queued"|"rejected", errors[]}.
- LIMITS in validate.ts mirror server limits (max 1000 events/batch, msg 32 KiB, attrs 64 KiB, depth 8, 256 keys, key chars [A-Za-z0-9_-]); change both sides together.
- Never throws from log(); failures surface via onError callback (ErvisioError.code: http|rejected|dropped|retries_exhausted) and Stats counters.
- Zero runtime deps; fetch/sleep/random injectable (FetchLike) for tests.
- Event ids (<=128 chars) are client-generated; used by server for idempotent dedupe on retry.
Gotchas:
- package.json "type":"module": `scripts/postbuild.mjs` writes dist/cjs/package.json {"type":"commonjs"} or the CJS build breaks. Build = tsc esm + tsc cjs + postbuild.
- close() sets closed -> later log() silently dropped; always await close() on shutdown.
- Retries are in-memory only: process crash loses queued events (no disk spool, unlike the backend).
- Config values are clamped (clampInt), not rejected; bad options are silently coerced.
- SDK version (0.1.0) is independent from the platform version (0.1.5); not bumped by release.sh and not in CI release.yml (publish is manual: `npm publish`, prepublishOnly runs typecheck+build).
Commands: cd sdk/typescript && npm ci && npm test (vitest) | npm run typecheck | npm run build. examples/ holds usage samples.
Log v2: EventOverrides tags (lowercase/dedupe/drop invalid with validation notice), actor/target Entity -> attributes.actor/target. InEvent tags, Entity exported; limits maxTags20/maxTagLen40 mirror backend.
Related: map/backend.md (ingest), map/integrations.md (FiveM re-implements the same protocol in Lua), map/deploy.md
