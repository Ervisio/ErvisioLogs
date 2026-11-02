# Progress tracker

Status: **P0 complete (2026-11-02). P1 not started.**
Repository: https://github.com/Ervisio/ErvisioLogs (private).

| Phase | Status | Evidence |
|---|---|---|
| P0 Discovery/foundation | **Done**, with the open items below | commits `675ad91`, `cd294a1`, ADRs in `docs/adr/`; test runs below |
| P1 Tenant/auth | Not started | — |
| P2 Ingestion/hot queries | Not started | — |
| P3 UI | Not started (app shell only, from P0) | — |
| P4 Tiered archives | Not started | — |
| P5 Integrations | Not started | — |
| P6 Ervisio plugin | Blocked: contract partly discovered, see ADR 0001 | `docs/adr/0001-*.md` |
| P7 Scale/hardening | Not started | — |

## P0 delivered

Monorepo (`backend/`, `web/`, `api/`, `deploy/`, `docs/`), Go service with validated env configuration (`_FILE` secrets,
production guards), `/healthz` and `/readyz` (PostgreSQL, ClickHouse, pending migrations), RFC 7807 errors, request ids,
security headers, body limits, graceful shutdown, embedded SPA serving. Migration runner for both stores with checksums,
advisory lock (PostgreSQL) and down scripts. Provisional ClickHouse `events` table and PostgreSQL `instance_settings`.
OpenAPI 3.1 skeleton with a test that fails when documented and implemented routes differ. React/Vite/Tailwind shell
(Ervisio design tokens, en/it, light/dark, expandable rail, health page). Hardened Docker Compose (images pinned by
digest, read-only app, dropped capabilities, DBs not published), Makefile, CI workflow, `.env.example`, `gen-env.sh`.

## Tests actually run (2026-11-02, macOS arm64, Go 1.27.3, Node 24, Docker 29.4)

| Check | Result |
|---|---|
| `go vet ./...`, `gofmt -l` | clean |
| `go test ./...` (config, httpx, migrate, server incl. OpenAPI contract, webui) | all pass |
| `go test -tags integration ./internal/migrate/` against real PostgreSQL 17 and ClickHouse 25.8 (down, up, up again, verify, down, up) | pass |
| `npm run lint`, `npm run typecheck`, `npm test` (6 tests), `npm run build` | pass |
| `redocly lint api/openapi.yaml` | valid, 6 warnings (unused security schemes, license, missing 4xx on probes) |
| `scripts/smoke.sh`: build image, start stack, `/healthz` 200, `/readyz` 200 with 4 checks ok, problem+json on unknown API path, UI served, Postgres not reachable from host, `migrate up` is a no-op the second time, `instance_id` row and `events` table exist | `SMOKE OK` |

Not run: `go test -race` in CI (only locally via `make test-backend`), GitHub Actions workflow (never executed on GitHub),
Playwright, browser rendering check of the React shell (design reviewed only as Variant Studio mockups), amd64 images.

## Problems found and fixed
- ClickHouse init script needs `CAP_KILL`; with `cap_drop: ALL` the first start failed and `--wait` reported unhealthy.
- `grep -q` under `pipefail` made the idempotency check fail spuriously (output captured in a variable instead).
- Google Fonts import would break the CSP and leak requests from a self-hosted product; Figtree is bundled via `@fontsource`.
- Variant Studio audit flagged secondary text contrast (3.4 to 3.7:1); tokens `--ink2/--ink3` lifted.

## Decisions and open items
- UI direction: Variant Studio rounds 1 to 3. The user picked a remix of "Rail con etichette" (round 3 C) and
  "Progetti e stream" (round 3 B) without notes. Implemented: C's expandable labelled rail. B's project/stream column
  is **not built**: it needs projects (P1/P3). Confirm this reading when P3 starts.
- ADR 0001 (D006): contract discovered from real repos, adapter blocked until P6; unverified items listed there.
- ADR 0002 to 0004 (D001, D002, D005): **Proposed only. No prototype has been run.** The P0 rule "resolve ADRs
  D001/D002/D005 prototypes" is therefore **not satisfied**; schedule these spikes before P2 (WAL) and P4 (query engine, cache).
- D003, D004, D007, D008, D009 untouched. ClickHouse `events` partitioning and ORDER BY are provisional.
- shadcn/ui is not installed yet (only Tailwind, `cn`, lucide). Add components when P3 needs them.
- Repository was created **private** under the Ervisio organization. Licence is undecided (OpenAPI says TBD).
- Compose runs with `ERVISIO_LOGS_ENV=development` because PostgreSQL traffic inside Compose has no TLS.
- No authentication exists. Nothing was deployed anywhere; only local Docker was used.

## Next: P1 (tenant/auth/metadata)
1. Run the D002 WAL spike and the D004 partitioning check in parallel with P1, so P2 starts with measured numbers.
2. PostgreSQL migrations: users, sessions, workspaces, members, roles, projects, environments, streams, api_tokens, audit_events.
3. Argon2id local auth, HttpOnly cookie sessions with CSRF, one-time admin bootstrap, RBAC and scopes, keyed token hashes.
4. Cross-tenant and revoked-token integration tests (P1 definition of done), OpenAPI update.
