# System architecture

## Modules
- API gateway: REST, auth, quotas, validation, request IDs, size limits.
- Ingestion: JSON single/batch; OTLP adapter; bounded buffering and durable WAL; replay; admission control.
- Processor: redact secrets before durable persistence where possible; normalize, enrich, route, batch, idempotency.
- Hot writer: ClickHouse batch insert, retry and DLQ for irrecoverable malformed events.
- Archive writer: immutable compressed Parquet segments, checksum, atomic catalog publish; S3 or SFTP provider.
- Catalog: PostgreSQL segment manifests, provider state, time range, stream/project scope, checksum, byte count, schema version, lifecycle status.
- Query planner: authorize tenant, select hot/remote segments, push down filters, merge sorted results, dedupe by stable event ID, enforce cost budgets.
- Tail broadcaster: SSE for live events after hot write; heartbeat, replay cursor, backpressure and reconnect.
- Retention manager: hot expiry, capacity eviction, remote expiry, retries, tombstones, metrics.
- Admin: users, workspaces, roles, tokens, projects, streams, policies, settings, audit.

## Delivery contract
- API returns 202 only after event batch is durably recorded (fsync/remote broker ack); ClickHouse query visibility is asynchronous.
- At-least-once: replay can duplicate; stable event ID and query-time dedupe; no claim of exactly-once physical storage.
- Batch response describes accepted/rejected and request ID. Retryable errors use Retry-After where appropriate.
- Bound WAL by disk budget; fail closed with 503 when durable capacity exhausted; never ack memory-only events.

## Scale profile
Compact: Go server+worker, PostgreSQL, ClickHouse, durable spool. Scale: separate workers, NATS JetStream with durable producer confirmation, horizontally scaled gateways. No Kubernetes requirement.

## Failure cases
SFTP outage: continue until WAL/hot capacity budget; alert; do not evict unarchived data. S3 outage: same. PostgreSQL unavailable: reject new requests needing metadata authorization unless safe bounded cached credentials with explicit expiry; no unsafe fallback. Crash between remote upload and catalog commit: reconcile orphan files. Crash between catalog commit and local eviction: safe duplicate copies.
