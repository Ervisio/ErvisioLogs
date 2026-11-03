# ADR 0002: Remote Parquet query engine (D001)

Status: Proposed. The prototype ran on 2026-11-03 (`docs/prototypes/d001`, results in `results/`). The decision
below waits for owner approval.

## Context
Archived events are immutable Parquet segments on S3 or on a Hetzner Storage Box over SFTP. Queries must filter by
tenant/project/stream/time and by common attributes, stream results, and stay inside memory budgets.
SFTP has no range reads or server-side filtering we can rely on, so it needs staging into the local cache.

## Options
1. **ClickHouse table functions** (`s3`, `file` over a cache directory). Same SQL dialect as the hot tier; the planner
   already exists.
2. **DuckDB** embedded in the Go process (or a sidecar). Strong Parquet pushdown, httpfs for S3. Adds CGO or a sidecar.
3. **Apache DataFusion** (Rust sidecar). A second language and runtime to operate.

## What the prototype measured
10,000,000 synthetic events in one project-day, written in three layouts: one file per day (237 MB), per hour
(24 files, 239 MB) and per 5 minutes (288 files, 260 MB). Four queries: a needle (one trace id), an attribute
filter, a count by level, and the first page of a time window. Median of 3 cold runs. S3 was SeaweedFS with 25 ms
added to every request; SFTP was atmoz/sftp on the same machine, downloaded to a cache and then read locally.

- **Correctness.** All 108 engine/layout/query runs returned the expected rows. A file of another project returned
  0 rows on every engine.
- **Speed on S3.** ClickHouse was the fastest or close to it everywhere except the day-layout attribute query
  (1.95 s against 0.70 s for DataFusion). DuckDB paid about 1 s of fixed cost per query. DataFusion fell apart on
  288 small files (17-18 s), because it registers each file separately.
- **Bytes read.** ClickHouse reads more than the others: the level count on the day file read 151 MB against
  3.5 MB for DuckDB, still in 0.59 s. On metered S3 this costs money; we accept it for now and will watch it.
- **Memory.** ClickHouse peaked at 554 MB (day file, attribute query) and below 400 MB elsewhere. DuckDB peaked at
  715 MB and DataFusion at 920 MB.
- **SFTP.** The download dominates: 2.0 s for the day file, 14 s for 288 small files with one connection. After
  download every engine answered in under 1 s.
- **Damaged files.** ClickHouse and DuckDB returned a clear error for truncated, garbage, empty and lying-footer
  files, and the ClickHouse server stayed up. DataFusion panicked on the lying footer.

Not measured: 100M events, segment sizes other than these layouts, a real WAN to AWS or Hetzner, first-row latency,
behaviour under a hard memory limit. These limits apply to the decision.

## Decision (proposed)
- **Engine: ClickHouse.** It was the fastest in most cases, it is already in the stack, it survived every damaged
  file, and its memory stayed lowest. DataFusion is out (small-file collapse and a panic on bad input). DuckDB would
  add a CGO or sidecar runtime with no measured gain for our queries.
- **Read path: download to the archive cache, then `file()`, for both S3 and SFTP.** Direct `s3()` reads were fast
  in the prototype, but they need provider credentials inside ClickHouse, either in the query text (visible in
  `system.query_log`) or as a named collection that ClickHouse would have to share with the API. The cache path keeps
  credentials in the API, gives one code path, and lets the API verify each segment's SHA-256 before ClickHouse
  touches it. Direct `s3()` through a named collection stays a later optimization.
- **Layout: one segment per stream-day, as ADR 0008 says.** Small files hurt every engine and SFTP the most. The
  cost is that a needle query on SFTP downloads the whole day once; the cache then serves later queries.
- **Safety:** the reader runs with `max_memory_usage` and `max_execution_time`, and a Parquet error marks only that
  segment as unreadable, with the search returning `partial=true`.

## Risks
Attribute filters on JSON columns defeat row-group pruning; ClickHouse reads more bytes than it needs. A busy
stream-day of several GB makes the first archive query slow on SFTP. If that shows up in practice, split segments
by size inside the day (the catalog already allows several segments per day via generations).

## Tests
Re-run `docs/prototypes/d001/run.sh` after a ClickHouse upgrade. The archive integration tests (P4) cover the read
path, the checksum check before reading, and the partial result when a segment is damaged or the provider is down.
