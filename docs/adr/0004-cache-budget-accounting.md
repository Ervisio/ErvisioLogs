# ADR 0004: Cache accounting and disk budgets (D005)

Status: Proposed. No prototype has been run.

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
