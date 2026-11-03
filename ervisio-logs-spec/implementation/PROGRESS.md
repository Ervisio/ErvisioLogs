# Progress tracker

Status: **P0 complete except the ADR prototypes. P1 backend complete. P1 frontend and P3 shell written in `web/`, uncommitted, tested end to end against the real stack (2026-11-03).**
Repository: https://github.com/Ervisio/ErvisioLogs (private).

| Phase | Status | Evidence |
|---|---|---|
| P0 Discovery/foundation | **Done except the D001/D002/D005 prototypes**, see open items | commits `675ad91`, `cd294a1`, ADRs in `docs/adr/`; test runs below |
| P1 Tenant/auth | **Backend done, frontend uncommitted, tested against the real API.** Real Google, Cognito and Discord never tested | commits `6a1c837`, `009ef1b`; P1 section below; web section below |
| P2 Ingestion/hot queries | Not started | — |
| P3 UI | Sign-in and navigation implemented, uncommitted. Explorer table waits for the P2 events API | `ervisio-logs-spec/design/UI_DECISIONS.md`; web section below |
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
- The end-to-end script lives outside the repository. It should become a Playwright suite in CI.
- Google, Discord and Cognito buttons were not exercised: no provider credentials.
- Project and workspace creation use a provisional form with no approved design: needs a Variant Studio round.
- The Explorer shows an empty state until the P2 events API exists. Sections without a backend say they are unavailable.

## Problems found and fixed
- ClickHouse init script needs `CAP_KILL`; with `cap_drop: ALL` the first start failed and `--wait` reported unhealthy.
- `grep -q` under `pipefail` made the idempotency check fail spuriously (output captured in a variable instead).
- Google Fonts import would break the CSP and leak requests from a self-hosted product; Figtree is bundled via `@fontsource`.
- Variant Studio audit flagged secondary text contrast (3.4 to 3.7:1); tokens `--ink2/--ink3` lifted.

## Decisions and open items
- UI direction: rounds 4 to 6 replace rounds 1 to 3, see `ervisio-logs-spec/design/UI_DECISIONS.md`.
- ADR 0001 (D006): contract discovered from real repos, adapter blocked until P6; unverified items listed there.
- ADR 0002 to 0004 (D001, D002, D005): **Proposed only. No prototype has been run.** The P0 rule "resolve ADRs
  D001/D002/D005 prototypes" is therefore **not satisfied**; schedule these spikes before P2 (WAL) and P4 (query engine, cache).
- D003, D004, D007, D008, D009 untouched. ClickHouse `events` partitioning and ORDER BY are provisional.
- shadcn/ui is not installed yet (only Tailwind, `cn`, lucide). Add components when P3 needs them.
- Repository was created **private** under the Ervisio organization. Licence is undecided (OpenAPI says TBD).
- Compose runs with `ERVISIO_LOGS_ENV=development` because PostgreSQL traffic inside Compose has no TLS.
- Nothing was deployed anywhere; only local Docker and GitHub Actions were used.

## Next
1. Commit `web/` on the owner's request. Move the end-to-end script into the repo as a Playwright suite. Run a Variant Studio round for project and workspace creation.
2. Write the README auth section and the auth ADR, then settle the owner's open decisions and provider credentials.
3. Run the D002 WAL spike and the D004 partitioning check before P2.
