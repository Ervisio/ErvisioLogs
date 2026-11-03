# Architecture decision records

Format: context, options, decision, risks, tests, status. Status is `Proposed`, `Accepted` or `Blocked`.
A record only becomes `Accepted` after the listed tests have run and the project owner approves it.

| ADR | Decision | Status |
|---|---|---|
| [0001](0001-ervisio-plugin-contract-discovery.md) | D006 Ervisio plugin contract (discovery) | Superseded in part by 0012 |
| [0002](0002-remote-parquet-query-engine.md) | D001 Remote Parquet query engine | Proposed (prototype run 2026-11-03, awaiting approval) |
| [0003](0003-wal-implementation.md) | D002 Durable WAL | Proposed (implemented and tested, awaiting approval) |
| [0004](0004-cache-budget-accounting.md) | D005 Cache byte accounting and disk budgets | Proposed (implemented in P4, prototype stopped without results) |
| [0005](0005-p0-technical-baseline.md) | P0 baseline choices (ids, versions, migrations) | Accepted for P0, revisit in P1 |
| [0006](0006-event-time-limits.md) | D007 Late and future timestamps | Proposed (implemented and tested, awaiting approval) |
| [0007](0007-hot-search-and-live-tail.md) | P2 hot search and live tail | Proposed (implemented and tested, awaiting approval) |
| [0008](0008-archive-catalog.md) | D003 Archive segments, catalog and publish protocol | Proposed (implemented in P4, awaiting approval) |
| [0009](0009-retention-partitioning.md) | D004 Tenant retention on shared day partitions | Proposed (implemented, benchmark not run) |
| [0010](0010-alerts-webhooks-export-otlp.md) | P5 saved queries, alerts, webhooks, export, OTLP receiver | Proposed (implemented, tests not run, OTLP not validated) |
| [0011](0011-packaging-and-release.md) | P7 release packages, image, standalone systemd, release workflow | Proposed (implemented, never run) |
| [0012](0012-ervisio-plugin.md) | P6 Ervisio plugin: Docker stack or standalone deploy | Proposed (implemented, not loaded in a real Ervisio) |

D008 (archive encryption) and D009 (replica quorum) stay open.
