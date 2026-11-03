# ADR 0009: Tenant retention on shared ClickHouse partitions (D004)

Status: Proposed. The benchmark in `docs/prototypes/d004/` was started on 2026-11-03 and stopped before it wrote any
result, on the owner's instruction not to run tests or benchmarks. Nothing below is measured.

## Context
Projects get different local and global retention (STORAGE_AND_RETENTION.md, P4 retention policies). The `events`
table is partitioned by day and every tenant shares each partition. Expiring one project's oldest day therefore means
deleting rows inside a partition, not dropping it. DECISIONS.md asks to avoid excessive mutations and to test the
partition strategy.

## Options
1. **Day partition, `ALTER TABLE ... DELETE IN PARTITION` with `mutations_sync = 1`.** One mutation per day per pass,
   covering every stream-day that leaves hot storage on that day. Bytes are freed when the mutation finishes.
2. **Day partition, lightweight `DELETE FROM`.** Cheap to issue, but rows are only masked; disk space comes back after
   a merge, so the disk budget (ADR 0004) cannot see the effect on the next pass.
3. **Partition by (retention class, day)**, so expiry becomes `DROP PARTITION`. Cheapest delete. Costs: more parts,
   retention changes move a project between classes (rewrite), and eviction under the disk cap still happens per
   stream-day, so mutations do not go away.
4. **Table per tenant or per class.** Rejected: unbounded table count, migrations per tenant.

## Decision (provisional)
Option 1, as implemented in `backend/internal/archive/evict.go` (`deleteHot`):
- The archiver groups the stream-days to delete by UTC day and issues one `ALTER TABLE events DELETE IN PARTITION
  <yyyymmdd>` per day, ordered oldest first.
- A stream-day that has archived generations deletes only `inserted_at < max(cutoff_to)` (ADR 0008 coverage rule). A
  stream-day past global retention deletes everything.
- Values in the statement are UUIDs and formatted times from internal types, never user input.
- The catalog marks the segments `hot_evicted_at` before the delete, so a crash between the two leaves search reading
  the remote copy, never a gap.

## Risks
- Mutations rewrite every part they touch in the partition. With many tenants on one day this can be heavy; the
  benchmark exists to size it.
- A mutation that fails stays queued in `system.mutations`. The archiver retries on the next pass; nobody alerts yet.
- `ORDER BY` and partitioning of `events` stay provisional (ADR 0005).

## Test gate (not run)
`docs/prototypes/d004/run.sh` with defaults (24 projects in 4 retention classes, 4 days, 200k rows per project-day):
insert cost and part counts for option 1 and option 3, query cost, expiry time and bytes freed for options 1, 2 and 3.
The same run feeds ADR 0004 (`system.parts` against `du` during a merge). Acceptance needs that run plus owner
approval.
