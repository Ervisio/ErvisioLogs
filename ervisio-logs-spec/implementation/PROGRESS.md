# Progress tracker

Status: **P0 complete except the D001/D005 prototypes. P1 backend committed. P1 frontend, P2 and P3 implemented and tested, uncommitted on `feat/p2-ingest` (2026-11-03). P4, P5 and P7 not started; P6 blocked.**
Repository: https://github.com/Ervisio/ErvisioLogs (private).

| Phase | Status | Evidence |
|---|---|---|
| P0 Discovery/foundation | **Done except the D001/D002/D005 prototypes**, see open items | commits `675ad91`, `cd294a1`, ADRs in `docs/adr/`; test runs below |
| P1 Tenant/auth | **Backend done, frontend uncommitted, tested against the real API.** Real Google, Cognito and Discord never tested | commits `6a1c837`, `009ef1b`; P1 section below; web section below |
| P2 Ingestion/hot queries | **Implemented and tested, uncommitted.** DoD met except a measured capacity figure (P7) | P2 section below; ADRs 0003, 0006, 0007 |
| P3 UI | **Implemented and tested, uncommitted.** Explorer, live tail, event detail, first-event wizard, API tokens, Users. Wizard, tokens and Users not design-approved | P3 section below; `ervisio-logs-spec/design/UI_DECISIONS.md` |
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
(Ervisio design tokens, en/it, light/dark, expandable rail, health page). Hardened Docker Compose (Postgres, ClickHouse
and Dockerfile base images pinned by digest, read-only app, dropped capabilities, DBs not published; the smoke test
checks only Postgres from the host), Makefile, CI workflow, `.env.example`, `gen-env.sh`.

## Tests actually run (2026-11-02, macOS arm64, Go 1.27.3, Node 24, Docker 29.4)

| Check | Result |
|---|---|
| `go vet ./...`, `gofmt -l` | clean |
| `go test ./...` (config, httpx, migrate, server incl. OpenAPI contract, webui) | all pass |
| `go test -tags integration ./internal/migrate/` against real PostgreSQL 17 and ClickHouse 25.8 (down, up, up again, verify, down, up) | pass |
| `npm run lint`, `npm run typecheck`, `npm test` (6 tests), `npm run build` | pass |
| `redocly lint api/openapi.yaml` | valid, 6 warnings (unused security schemes, license, missing 4xx on probes) |
| `scripts/smoke.sh`: build image, start stack, `/healthz` 200, `/readyz` 200 with 4 checks ok, problem+json on unknown API path, UI served, Postgres not reachable from host, `migrate up` is a no-op the second time, `instance_id` row and `events` table exist | `SMOKE OK` |

Not run in P0: Playwright, browser rendering check of the React shell (design reviewed only as Variant Studio mockups).
CI now runs `go test -race`, integration tests and the smoke test on GitHub Actions (ubuntu-latest, x64).

## P3 delivered (uncommitted, 2026-11-03, Windows 11, Node 24, Docker)

Log Explorer with free text and `key=value` filters, level and time range, keyset paging and an event detail dialog;
Live mode over SSE with pause and resume (`last_event_id`); first-event wizard for empty projects (creates a token,
shows a curl command, sends a test event and waits for it on the tail); API tokens page (create, copy once, revoke);
Users page (list, change role, remove, leave, add by email, role help) with the owner rules the API enforces.

| Check | Result |
|---|---|
| `npm run lint`, `npm run typecheck`, `npm run build` | clean |
| `npm test` (vitest, stubbed API) | 47 passed |
| `scripts/e2e.sh`: Playwright against a fresh Compose stack, random secrets, no stubs | 3 of 3 pass |

The end-to-end suite (`web/e2e/first-run.e2e.ts`) covers: first administrator with the setup code, workspace and
project, wizard token (`evl_` prefix), test event visible on the live tail, a second event sent with the token only,
search by `user=ada` plus free text, detail dialog, the token on the tokens page; at 375 px no sideways scroll, drawer
navigation to Settings and Users, 44 px touch targets; dark theme survives a reload. CI runs it as the `e2e` job.

Found and fixed during the end-to-end run:
- Typing the same query again does not search again; the test clears the box first. Live mode covers refreshing.
- Tokens and Users appear in the drawer only inside Settings; the test now navigates there first.

Limits:
- No row virtualization: a page holds at most the page size the API returns.
- No histogram over time.
- The wizard forgets its state (token shown once) if you leave the page before the first event arrives.
- Wizard, tokens and Users pages were not designed through Variant Studio; they await owner approval.
- npm audit reports 11 moderate findings in the eslint toolchain (dev only), not addressed.

## P2 backend delivered (uncommitted, 2026-11-03, Windows 11, Go 1.27.3, Docker, ClickHouse 25.8)

Single and batch ingest (`/api/v1/ingest`, `/api/v1/ingest/batch`, gzip, strict JSON, per-event errors), segmented WAL
with group commit (`internal/wal`), normalizer and limits (`internal/ingest/event.go`), ClickHouse writer with replay
and insert deduplication (`internal/ingest/writer.go`), search with filters, keyset pagination and totals
(`/api/v1/logs/search`), SSE live tail with resume (`/api/v1/logs/tail`), Prometheus metrics (`/metrics`), migration
`0002_events_dedup`. OpenAPI 0.3.0. Compose passes the ingest settings; `.env.example` documents them.

| Check | Result |
|---|---|
| `go vet` with and without the integration tag, `gofmt` on every Go file | clean |
| `go test -count=1 ./...` | 11 packages pass |
| Integration (`api`, `ingest`, `migrate`) against real PostgreSQL and ClickHouse, `-v` | exit 0, 118 PASS lines, 0 SKIP, 0 FAIL |
| `scripts/smoke.sh`: P0 checks, then admin, workspace, project, token; ingest; attribute search finds the event; clean restart keeps one copy; **ClickHouse stopped, batch answered 202, app killed with SIGKILL, restart: event stored exactly once** | `SMOKE OK` |
| `redocly lint api/openapi.yaml` | valid, 10 warnings not examined |

Found and fixed during testing:
- A zero-valued `Validator` rejected every event ("older than 0s"). `MaxPastAge` now falls back to 7 days.
- Insert deduplication tokens were `wal-<first>-<last>`. A fresh spool restarts at sequence 1, so ClickHouse dropped
  new events as copies of old blocks and the first API test stored nothing. Tokens now carry a random spool id.
- `exists` on an object path (`player`) never matched: ClickHouse reads such paths as NULL. It now checks `JSONAllPaths`.
- The docs said filter fields need no `attributes.` prefix but the code required it. The prefix is now optional.

Limits:
- Live tail works within one instance only (ADR 0007). Run a single API instance until a shared bus exists (P7).
- No throughput or latency benchmark of the ingest path or of search on a large table. The WAL figures in ADR 0003
  come from this Windows desktop, not the target disk.
- Race detector not run here (no cgo toolchain on this machine); CI runs `go test -race` on Linux.
- Power loss was not simulated; SIGKILL leaves the OS page cache intact.
- D004 (retention partitioning) stays open for P4. `events` partitioning and ORDER BY are still provisional.

## P1 backend delivered

Tenancy, RBAC, API tokens, audit, local accounts (Argon2id), cookie sessions with CSRF, and sign-in through Google,
AWS Cognito and Discord (authorization code with PKCE).

Tests run locally before commit `009ef1b`: `gofmt`, `go vet` with and without the integration tag, `go test ./...`,
`make test-integration` (exit 0, 102 PASS lines, no skips, api and migrate packages), `make smoke` (`SMOKE OK`),
`make test-web` (6 passed), lint and typecheck. Redocly reports the OpenAPI file valid, with 10 warnings not examined.

Limits:
- Google, Cognito and Discord were never tested for real: no credentials. Tests use fake providers.
- Mutation checks were not run for review points 4, 5, 9, 10, 11 and 14.
- `addMember` keeps a known TOCTOU race. With several tabs, only the last OAuth start stays valid. The rate limiter
  lives in memory per instance.
- Existing named volumes stay root-owned: recreate or `chown` them.
- Missing: dedicated secrets policy document, README auth section, auth ADR.

Decisions waiting for the owner: email verification and SMTP, invitations with acceptance, service accounts, MFA, token
rotation, per-tenant rate limit, JSON depth limit, `subject_id`, `/audit` route shape, environment and stream update.

## Web implementation (uncommitted working tree, 2026-11-02, Windows 11)

Sign-in, registration, first run and OAuth errors; auth guard and CSRF header; project rail, section column, streams,
account menu, command palette, project and workspace creation, status page; en/it, dark/light.

| Check | Result |
|---|---|
| `npm run lint`, `npm run typecheck` | clean |
| `npm test` | 30 passed |
| `npm run build` | pass |
| Playwright audit, stubbed API, not in the repo: 1280, 390 and 320 px, it/en, dark/light; overflow, cut and wrapped labels, targets, AA contrast, console errors, edge margin on mobile | 132 scenes, 0 issues |

| Playwright end to end against the real Compose stack (2026-11-03, Docker 29.2.1, fresh volumes, no stubs): anonymous redirect, wrong setup code (403 message), first administrator, workspace, project with `default` stream, reload keeps session, sign out, wrong password (401 message), sign in, status page | 10 of 10 pass; workspace, project and logout requests carry `X-CSRF-Token` |

Found by the end-to-end run and fixed: Vite inlined small woff2 files as `data:` URIs, which the server CSP blocks.
`vite.config.ts` now never inlines fonts, and the strict CSP stays.

Limits:
- Done in P3: the end-to-end suite now lives in `web/e2e/` and runs in CI.
- Google, Discord and Cognito buttons were not exercised: no provider credentials.
- Project and workspace creation use a provisional form with no approved design: needs a Variant Studio round.
- Dashboards, Alerts, Integrations, Storage, Usage, Audit and the Settings overview show a "coming soon" notice. The audit API exists (P1) but has no page yet.

## Problems found and fixed
- ClickHouse init script needs `CAP_KILL`; with `cap_drop: ALL` the first start failed and `--wait` reported unhealthy.
- `grep -q` under `pipefail` made the idempotency check fail spuriously (output captured in a variable instead).
- Google Fonts import would break the CSP and leak requests from a self-hosted product; Figtree is bundled via `@fontsource`.
- Variant Studio audit flagged secondary text contrast (3.4 to 3.7:1); tokens `--ink2/--ink3` lifted.

## Decisions and open items
- UI direction: rounds 4 to 6 replace rounds 1 to 3, see `ervisio-logs-spec/design/UI_DECISIONS.md`.
- ADR 0001 (D006): contract discovered from real repos, adapter blocked until P6; unverified items listed there.
- ADR 0003 (D002 WAL): prototype run and implemented, awaiting approval. ADRs 0002 and 0004 (D001, D005): **Proposed
  only, no prototype run**; both are needed before P4.
- ADR 0006 (D007) and 0007 (P2 search and tail): implemented and tested, awaiting approval.
- D003, D004, D008, D009 untouched. ClickHouse `events` partitioning and ORDER BY are provisional.
- shadcn/ui is not installed yet (only Tailwind, `cn`, lucide). Add components when P3 needs them.
- Repository was created **private** under the Ervisio organization. Licence is undecided (OpenAPI says TBD).
- Compose runs with `ERVISIO_LOGS_ENV=development` because PostgreSQL traffic inside Compose has no TLS.
- Nothing was deployed anywhere; only local Docker and GitHub Actions were used.

## Next
1. Commit `web/` on the owner's request. Move the end-to-end script into the repo as a Playwright suite. Run a Variant Studio round for project and workspace creation.
2. Write the README auth section and the auth ADR, then settle the owner's open decisions and provider credentials.
3. P3: Explorer table on `/logs/search`, Live Tail view on `/logs/tail`, token management, onboarding wizard, Playwright suite in CI.
4. Before P4: D001 and D005 prototypes, D004 partitioning check.
