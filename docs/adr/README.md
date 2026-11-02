# Architecture decision records

Format: context, options, decision, risks, tests, status. Status is `Proposed`, `Accepted` or `Blocked`.
A record only becomes `Accepted` after the listed tests have run and the project owner approves it.

| ADR | Decision | Status |
|---|---|---|
| [0001](0001-ervisio-plugin-contract-discovery.md) | D006 Ervisio plugin contract (discovery) | Proposed (discovery done, adapter blocked until P6) |
| [0002](0002-remote-parquet-query-engine.md) | D001 Remote Parquet query engine | Proposed (prototype not run) |
| [0003](0003-wal-implementation.md) | D002 Durable WAL | Proposed (prototype not run) |
| [0004](0004-cache-budget-accounting.md) | D005 Cache byte accounting and disk budgets | Proposed (prototype not run) |
| [0005](0005-p0-technical-baseline.md) | P0 baseline choices (ids, versions, migrations) | Accepted for P0, revisit in P1 |

D003, D004, D007, D008 and D009 stay open; the spec places them in P2 to P4.
