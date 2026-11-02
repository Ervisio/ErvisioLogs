# Acceptance and performance test matrix

## Functional
- Workspace/project CRUD, member roles, token scope, revocation, multi-tenant isolation.
- Ingest 1/batch; schema validation; search filter/time/cursor; live tail reconnect; export authorization.
- SFTP and S3 provider connection, upload verify, read, delete, failure recovery, checksum mismatch.
- Retention: local 3 days, global 14 days; early eviction at 90%, target 75%, never evict unarchived; late event; time skew; deletion backlog.
- Query: hot-only, remote-only, overlap dedup, unavailable archive produces explicit partial/error.
- WAL crash at every transition; no accepted event lost; no unbounded memory/disk growth.
- FiveM resource mocked asynchronous HTTP and retries; human handles in-game tests.

## Performance
Benchmark 1k, 10k, 50k events/s as exploratory loads on documented hardware; record throughput, P50/P95/P99 ingestion ack and query latency, memory, CPU, disk IO, object requests, cost. No throughput guarantees until measured. Datasets: 1M, 100M, 1B synthetic events with realistic cardinality and JSON attributes; run only where resources allow. Test remote SFTP cold read separately from S3 range-read. Soak tests and chaos outages.

## Release gates
Security tests; migrations and backup/restore; no critical data loss; published OpenAPI; responsive and accessible UI; reproducible Docker install; honest limitations.
