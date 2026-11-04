# ADR 0020: Ingest limits and quotas, admin health, manual purge

Status: Proposed. Written for 0.2. Checked with `go test`, `go vet` and the API integration tests, including a purge
against a real ClickHouse.

## Context
One misbehaving token could fill the write-ahead log for every tenant, operators had no single place to see whether
events were reaching ClickHouse and the archive, and the only way to remove logs was retention or deleting the project.

## Decision
**Limits and quotas.** Runtime settings (ADR 0018), all defaulting to 0 = unlimited:
`ingest.rate_per_token`, `ingest.rate_per_project`, `ingest.rate_per_workspace` (events per second) with a shared
`ingest.rate_burst_seconds` (default 5), and `ingest.{workspace,project}_quota_{events,bytes}` per UTC day. Rates and
quotas are settable at instance level and below (workspace for the workspace keys, project for the project and token
keys), so an operator sets a default and an owner tightens it.
- The check runs in `queueEvents`, after validation and encoding, so only accepted events count and the byte quota
  counts the encoded WAL record. A refused batch spends nothing; if the WAL append fails the spend is returned.
- Refusals answer 429 with `Retry-After`: the time to refill the bucket, or until UTC midnight for a quota. OTLP
  shares the path.
- A batch larger than the bucket capacity is judged against the capacity, so a limit never makes a batch impossible.
- State is in memory, per process: with several instances the effective limit is the sum, and a restart resets the
  counters.

**Admin health.** `GET /admin/health` (instance administrator) returns the WAL usage and pending records, the upload
queue (waiting and failed segments, last verified time), the active archive destination status and last error,
PostgreSQL and ClickHouse reachability, and the oldest hot event no cataloged segment covers. It reads state the
process already keeps; the only new query scans the hot table once and is cached for 30 seconds. `status` is
`degraded` when a database is down, a segment failed or the destination reports an error.

**Manual purge.** `POST /projects/{id}/purges` with `{"confirm": <slug>}`, optional `stream`, `environment`, `from`
and `to`; needs `logs:delete` and is audited as `logs.purge`. It answers 202 and a job, because deleting can take
minutes and a request should not hold a connection open for it.
- Jobs live in `purge_jobs` (migration 0010). A worker in the archiver process polls every 5 seconds and runs jobs
  under the archiver lock, so a purge never overlaps an archive pass that could write the rows it deletes. A job found
  `running` after a crash is requeued; every step is safe to repeat. One open purge per project (409).
- Hot rows: one synchronous `ALTER TABLE events DELETE IN PARTITION` per day, never `DROP PARTITION`, because a
  partition holds every tenant (ADR 0009).
- Archived data: segments are immutable Parquet files, so a segment is removed whole or not at all. Segments entirely
  inside the selection are moved to `REMOTE_EXPIRED` and deleted by the existing retention path. A selection that
  cuts a segment, or any environment filter that touches one, is refused with 422 before the job is created; the
  message names the day. Segments still being written are cancelled.
- The endpoint answers 503 when the archiver is off in the process that serves it.

## Risks
- A purged stream-day keeps its `DELETED` tombstone, which makes the archiver skip that stream-day for good: rows
  ingested for it later stay hot until global retention removes them.
- Events acknowledged but still in the WAL when the purge runs are not covered; they reach ClickHouse afterwards.
- A purge holds the archiver lock, so archive passes wait for it.
- Limits and quotas are per instance and reset on restart; a multi-instance deployment needs shared state first.
- The oldest-unarchived value is computed per stream-day from the first event, so it can be earlier than the oldest
  unarchived row.
- Purged data is gone. There is no undo and the audit entry records the selection, not the data.
