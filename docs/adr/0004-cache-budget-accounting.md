# ADR 0004: Cache accounting and disk budgets (D005)

Status: Proposed. Implemented in P4 (see below). No prototype result exists.

## Context
`cache_max_gb` (example 20) with high/low watermarks 90/75. The spec requires separate budgets for WAL, PostgreSQL,
query temp space and the cache, and asks what exactly the cache cap covers.

## Proposal
- Cache budget counts: hot ClickHouse data of events that are already remote-backed, plus downloaded archive files held
  for queries. It excludes WAL, PostgreSQL, ClickHouse system tables and merges overhead.
- Each budget has its own volume (Compose already declares `spool` and `cache`; ClickHouse and PostgreSQL have theirs).
  Budgets are enforced by a size accountant that reads `system.parts` (hot) and tracks files (archive cache); it does not
  trust `df` alone.
- Eviction order: oldest VERIFIED or CATALOGED hot partitions first, until the low watermark. Never evict a segment without a verified remote copy.
- When still above the cap with nothing evictable: 503 (capacity) or 429 (quota) on ingest, never silent drop.

## Open questions for the prototype
Measure ClickHouse part size versus on-disk usage during merges (temporary doubling), and whether partition-level `DROP`
keeps the accounting accurate when tenants have different retention (links to D004).

## Test gate
20 GB cap simulation at 90/75, 3 day local and 14 day global simulated clock, outage during upload.

## Implemented in P4 (2026-11-03), still Proposed
`backend/internal/archive/evict.go` (`enforceBudget`) differs from the proposal above in two ways:
- The budget counts **all** active hot parts of `events` (`sum(bytes_on_disk)` from `system.parts`), not only the
  remote-backed ones, plus the `.parquet` files in the archive cache. Counting only covered data would let uncovered
  hot data grow past the disk without the cap noticing.
- Above the high watermark the archiver first deletes cached segments, least recently read first (they can be
  downloaded again), then evicts the oldest covered stream-days until usage is under the low watermark. Uncovered rows
  are never evicted.
- If usage is still above the cap, the archiver sets an over-capacity flag and ingest answers 503. The flag lives in
  memory of the process that ran the pass: it resets on restart until the next pass, and a second API instance does
  not see it. Quota (429) is not built.
- Separate budgets for WAL, PostgreSQL and query temp space are not enforced by the app; they rely on separate volumes.

The prototype (`docs/prototypes/d004/`, shared with ADR 0009) was started and stopped before writing results, on the
owner's instruction not to run benchmarks. The merge-doubling question above is still unanswered.
