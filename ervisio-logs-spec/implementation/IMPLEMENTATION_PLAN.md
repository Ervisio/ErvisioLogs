# Implementation plan — ordered, test-gated

## Working rules
Small PR-sized commits; run lint/unit/integration tests; do not mark tasks done without evidence. Build backend contract before UI; use feature flags for incomplete providers. Track ADRs and status in `implementation/PROGRESS.md`. Never ask to deploy or modify existing production infrastructure without explicit permission.

### P0 — Discovery and foundation
- Inventory repository and Ervisio plugin sources (if accessible); inspect licensing and existing patterns.
- Resolve ADRs D001/D002/D005 prototypes. Scaffold Go app, React app, migration runner, Docker Compose, CI, OpenAPI skeleton, Makefile, `.env.example`, secrets policy.
- DoD: clean install starts health endpoints, PostgreSQL and ClickHouse; smoke tests run.

### P1 — Tenant/auth/metadata
- DB migrations, local auth, sessions, workspace/project/environment/stream CRUD, RBAC, scoped tokens, audit.
- DoD: cross-tenant and revoked-token integration tests pass; OpenAPI updated.

### P2 — Durable ingestion and hot queries
- HTTP single/batch ingest, bounded WAL, replay, normalizer, hot ClickHouse writer, search AST, pagination, SSE, operational metrics.
- DoD: 202 only after durable ack; crash/restart test no accepted event lost; retry duplicates controlled; hot query verified.

### P3 — Core UI and onboarding
- Responsive shell, project wizard, overview, Log Explorer, Live Tail, token/role management, dark/light, i18n.
- DoD: Playwright end-to-end new project -> token -> ingest -> search; mobile layout checks.

### P4 — Remote archives and tiering (first-class requirement)
- Parquet segment writer and manifest catalog; S3 adapter; SFTP Storage Box adapter; verified upload; remote query prototype; cache GB/age/watermarks; retention jobs; failure reconciliation; admin storage settings.
- DoD: 3-day local / 14-day global simulated clock tests; 20GB cap simulation; archived events searchable after local eviction; remote outage does not cause loss; deletion eventually completes.

### P5 — Integrations and alerts
- TypeScript SDK, FiveM Lua resource and ESX examples, OTLP conformance, alerts/webhooks, saved views, export.
- DoD: mocked FiveM tests and integration tests; in-game validation delegated to user.

### P6 — Ervisio plugin
- Only after verifying real plugin contract: managed/external link, SSO, navigation, health, lifecycle and safe uninstall.
- DoD: integration tested against actual Ervisio installation/version; otherwise explicitly blocked.

### P7 — Production hardening and scale
- NATS JetStream optional profile, load tests, disaster recovery, migration/rollback drills, performance budgets, documentation, release artifacts.
- DoD: measured capacity report, restore exercise, security checklist, release notes.

## Scope control
If tasks are too large, deliver the smallest fully tested vertical slice. Avoid speculative scaffolding of all phases. Report blockers and tradeoffs instead of declaring unsupported features complete.
