# Progress tracker

Status: **P0 to P7 merged into `main` (PR #1, PR #2). On 2026-11-03 the CI that runs on push passed all six jobs on `abdc1f9`: backend, web, openapi, integration, e2e, smoke. v0.1.0 released on 2026-11-03 (image `ghcr.io/ervisio/ervisio-logs:0.1.0`, repo, release and image public); plugin registered in Ervisio/plugins#35, waiting for a maintainer merge before the team signature. Not run: the archive provider suite (`make archive-test`), the SDK and Lua specs, any package installed on a host, the plugin loaded in Ervisio, OTLP, FiveM in game.**
Repository: https://github.com/Ervisio/ErvisioLogs (private).

| Phase | Status | Evidence |
|---|---|---|
| P0 Discovery/foundation | **Done except the D001/D002/D005 prototypes**, see open items | commits `675ad91`, `cd294a1`, ADRs in `docs/adr/`; test runs below |
| P1 Tenant/auth | **Backend done, frontend uncommitted, tested against the real API.** Real Google, Cognito and Discord never tested | commits `6a1c837`, `009ef1b`; P1 section below; web section below |
| P2 Ingestion/hot queries | **Implemented and tested, merged (PR #1).** DoD met except a measured capacity figure (P7) | P2 section below; ADRs 0003, 0006, 0007 |
| P3 UI | **Implemented and tested, merged (PR #1).** Explorer, live tail, event detail, first-event wizard, API tokens, Users. Wizard, tokens and Users not design-approved | P3 section below; `ervisio-logs-spec/design/UI_DECISIONS.md` |
| P4 Tiered archives | **Implemented, merged (PR #2). Integration tests pass in CI; the S3/SFTP provider suite was not run** | P4 section below; ADRs 0004, 0008, 0009 |
| P5 Integrations | **Implemented, merged (PR #2). Integration tests pass in CI.** OTLP not validated; FiveM never run in game; SDK and Lua specs not run | P5 section below; ADR 0010 |
| P6 Ervisio plugin | **Implemented, merged (PR #2), never loaded in Ervisio** | P6 section below; ADRs 0001, 0012 |
| P7 Scale/hardening | **v0.1.0 released by the tag workflow; no package installed on a host yet.** Capacity, load and restore tests not done | P7 section below; ADR 0011 |

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

## P5 to P7 delivered (uncommitted on `feat/p4-archive`, 2026-11-03, Windows 11). Nothing below was run.

Checks that did run: `go build ./...` and `go vet ./...` (also `GOOS=linux GOARCH=arm64 go build`), `tsc` and `eslint`
on `web/` and `sdk/typescript/`, `redocly lint api/openapi.yaml`, `bash -n` on every script, `node --check` on the
plugin's `index.js`, and the plugin stamping step of `release.sh` on a scratch copy. No unit, integration, e2e, Lua or
SDK test was executed; none of them should be read as passing.

**P5 (ADR 0010).** Saved queries; threshold alert rules with an evaluator under a Postgres advisory lock; webhook
channels (Ervisio signed JSON, Slack, Discord) with SSRF filtering on the dialled address; NDJSON and CSV export with a
row cap and trailers; OTLP/HTTP `/v1/logs` receiver (JSON and protobuf). UI pages Alerts and Channels follow the
approved design system (no new Variant Studio round). TypeScript SDK `@ervisio/logs` in `sdk/typescript`. FiveM
resource in `integrations/fivem`, syntax-checked only. Tests written: `internal/api/integrations_test.go`,
`internal/notify/webhook_test.go`, `internal/api/integrations_integration_test.go`, SDK vitest specs,
`integrations/fivem/tests/queue_spec.lua`.
Not done: OTLP against a real exporter and the Collector (the UI does not mention OTLP), FiveM in game (human
operator), webhook delivery to real Slack and Discord.

**P6 (ADR 0012, ADR 0001 revised).** Ervisio plugin in `integrations/ervisio-plugin`: one page, Docker mode (stack in
`/opt/stacks/ervisio-logs`, same argv as plugin-docker) or standalone mode (release tarball checked by SHA-256, then
`install.sh`). No SSO (Ervisio offers none), no data deletion on any path. Guide `docs/deploy/ervisio-plugin.md`.
Not done: loading it in Ervisio, unlock flow, stack visibility in plugin-docker, both installs, upgrade, removal.
Marketplace listing needs the Ervisio team's signature.

**P7 (ADR 0011).** Multi-arch Dockerfile, `scripts/release.sh` (standalone tarballs, Docker bundle, plugin package,
SHA256SUMS), tag workflow `.github/workflows/release.yml` (draft release), systemd unit with hardening,
`install.sh`/`uninstall.sh`, deploy guides, `docs/security.md`, `docs/releases/0.1.0.md`.
Done: the v0.1.0 workflow run and release. Not done: an install on a real host, a measured capacity figure, load and soak tests,
backup and restore drill, shared live-tail bus for several instances.

**Commit note.** Files were written on Windows: when committing, set the exec bit with
`git update-index --chmod=+x` on `scripts/release.sh`, `deploy/standalone/install.sh`,
`deploy/standalone/uninstall.sh` and `integrations/ervisio-plugin/bin/standalone.sh`.

## P4 delivered (uncommitted on `feat/p4-archive`, 2026-11-03, Windows 11, Go 1.27.3, ClickHouse 25.8)

- Archive providers: local folder, S3-compatible (checksum stored with the object, server-side copy on promote) and
  SFTP (temporary name, re-read for SHA-256, rename, pinned host key). Credentials come from files in the secrets
  folder, never from PostgreSQL or the browser.
- Catalog in PostgreSQL (migration 0003): providers, segments with the full state machine, retention policies per
  instance, workspace, project and stream. ClickHouse migration 0003 adds `inserted_at` as the coverage marker.
- Archiver loop: generations per stream-day cut on the ClickHouse clock, Parquet export, upload, verify, promote,
  catalog, retries with backoff, FAILED after max attempts, clock-going-back guard.
- Local retention eviction and global expiry (tombstones), disk budget with high and low watermarks, 503 on ingest
  when over the cap with nothing evictable (ADR 0004, ADR 0009).
- Search reads evicted segments through a verified local cache with `file()` and an explicit schema; `partial: true`
  when a segment is unavailable (ADR 0008).
- Orphan cleanup once a day per process (ADR 0008).
- Admin API (`/api/v1/admin/storage`, `/api/v1/admin/retention`) and the web Storage page (usage meter, project
  retention, destinations, add destination, server default). en/it strings.
- `make archive-test` runs the S3 and SFTP contract suite against throwaway SeaweedFS and OpenSSH containers;
  `make test-integration` now also runs the orphan tests.

Tests: integration tests exist for the store, the archiver (crash at each state, retries, eviction, expiry, budget),
the provider contract, orphan cleanup, the admin API and search after eviction, plus 4 web tests for the Storage page.
The CI run on push for `abdc1f9` passed the unit and integration tests. `make archive-test` (S3, SFTP) is not in CI
and was not run on the final tree.

Limits:
- D004/D005 benchmark (`docs/prototypes/d004/`) stopped before writing results. ADRs 0004 and 0009 are unmeasured.
- Over-capacity flag lives in process memory (one API instance only).
- Cache race: a segment evicted from the cache during a search gives a 503; retry succeeds.
- No compaction of small late generations; no size split of a very busy stream-day.
- D008 (archive encryption) and D009 (replicas) not built.
- Storage page not design-approved through Variant Studio.

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
1. Run `make archive-test`, the SDK tests and the Lua specs once the owner allows it.
2. Use the app from source with `make up` (the e2e job covers this path).
3. Merge Ervisio/plugins#35 (maintainer) so the registry signs and lists the plugin; install each package on a test host; load the plugin in Ervisio.
4. Validate OTLP with a real exporter; FiveM in game by the human operator.
5. Capacity measurement, restore drill, owner approval of the Proposed ADRs.
