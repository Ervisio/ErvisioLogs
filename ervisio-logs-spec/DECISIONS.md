# Architecture decision register

## Accepted baseline
- Go + Chi modular monolith; React/TypeScript/Vite/Tailwind/shadcn; PostgreSQL metadata; ClickHouse hot logs; SSE; at-least-once ingestion; Docker Compose.
- Local-only and remote archive provider abstractions. Hetzner Storage Box SFTP and S3 are both first-class planned providers.

## Decisions requiring benchmark/prototype
- D001: remote Parquet query engine: DuckDB vs DataFusion vs ClickHouse table functions; prove selective query, streaming, memory and file count behavior for both SFTP and S3.
- D002: WAL implementation: evaluate mature embeddable durable queue; define fsync policy, ack boundary, disk full behavior and replay ordering.
- D003: storage catalog transactional publishing, crash recovery, compaction and tombstone semantics.
- D004: tenant-specific retention with shared ClickHouse partitions; avoid excessive mutations; test time partition strategy.
- D005: cache byte accounting (hot ClickHouse vs downloaded archive files) and exact scope of cap; reserve independent WAL and DB operational disk.
- D006: Ervisio plugin manifest, lifecycle, auth and API contract: inspect actual repos before implementation; no assumptions.
- D007: timestamps older/future than retention; ingest-time vs event-time handling and late arrivals.
- D008: encryption at rest for archive files and key management; password/SSH host key handling.
- D009: remote replica quorum and read failover, deferred until proven single-provider path.

Every decision needs ADR with context, alternatives, risks, tests and final approval. Do not silently substitute a simpler architecture that violates user-visible behavior.
