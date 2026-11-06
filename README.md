# Ervisio Logs

## In-app documentation

After signing in, open **Documentation** in the main navigation or the command palette (`Ctrl K` / `⌘K`). The bundled
English and Italian guides cover setup, ingestion, search, tokens and roles, storage and retention, FiveM, alerts,
scoped settings, account security, dashboards, deployment and troubleshooting. Search works across article text and
examples. No project or administrator role is required; share a topic using its `/docs/<topic>` address.

Self-hosted, API-first, multi-tenant log management. Runs standalone (systemd), as a Docker Compose stack, or from an
Ervisio plugin that installs either of the two.

Status: **0.2.0 prepared in this checkout.** The source includes the scoped management UI, Log v2, optional account
security and the Ervisio management plugin. See [0.2.0 notes](docs/releases/0.2.0.md) for changes and validation limits.
This work does not publish a release or deploy a host.

## What you can manage

- Switch workspace and project from the rail. Project settings contain environments, streams, tokens, event
  titles/templates, field rules and retention. Workspace settings contain members, invitations, notification channels
  and workspace defaults.
- Search tags and nested attributes, inspect actor/target context, select a range in the histogram and follow matching
  events with Live. Build dashboard panels from saved queries and inspect project usage.
- Use Administration for instance users, archive destinations, runtime settings, operator tokens and service health.
  Enable optional authenticator codes or passkeys from Account; choose Light, Dark or AMOLED.
- Manage the instance through the plugin’s nine sections, using a local admin socket or an approved remote HTTPS host
  with a scoped operator token. Host environment changes and restarts stay in Server.

## Deploy

| Mode                                                        | Guide                                                            |
| ----------------------------------------------------------- | ---------------------------------------------------------------- |
| Standalone (systemd, you provide PostgreSQL and ClickHouse) | [`docs/deploy/standalone.md`](docs/deploy/standalone.md)         |
| Docker Compose (app and databases)                          | [`docs/deploy/docker.md`](docs/deploy/docker.md)                 |
| Ervisio plugin (picks one of the two)                       | [`docs/deploy/ervisio-plugin.md`](docs/deploy/ervisio-plugin.md) |

`VERSION=x.y.z scripts/release.sh` builds all packages into `dist/x.y.z/`; a `v*` tag runs the same in
`.github/workflows/release.yml` and opens a draft release. Before exposing an instance, go through
[`docs/security.md`](docs/security.md).

## Quick start

Requirements: Docker with Compose v2.

```sh
make up                 # creates or completes .env with random secrets, builds, starts, waits for health
open http://localhost:8080
make down
```

`make smoke` runs the full end-to-end check in an isolated Compose project and removes it afterwards. `make e2e` (or
`scripts/e2e.sh`) does the same with a browser: it starts a fresh stack with random secrets, runs the Playwright suite
in `web/e2e/` (first administrator, project, token, ingest, search, phone layout, dark theme) and removes the stack.
Pass Playwright arguments through, for example `scripts/e2e.sh --headed`.

## First run and sign-in

The first person to open the app enters the setup code and becomes the instance administrator; afterwards the code does
nothing. Set it with `ERVISIO_LOGS_SETUP_CODE`, or leave it unset and read the generated one from the app log
(`make logs`). Enable self-registration in Administration, or set `ERVISIO_LOGS_OPEN_REGISTRATION=true` on the host.
Google, AWS Cognito and Discord sign-in turn on when you set their client id and secret (see `.env.example`); they have
not been tested against the real providers yet.

Members belong to a workspace. Owners and admins add an existing account or invite an email address from **Workspace
settings → Members**. SMTP sends invitations and password-reset links; without SMTP, copy the returned invitation link.
Servers send events with an API token (`evl_...`) from **Project settings → Tokens**:

```sh
curl -X POST http://localhost:8080/api/v1/ingest   -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json"   -d '{"level":"info","event":"player.joined","message":"ada joined","attributes":{"user":"ada"}}'
```

## Layout

| Path                     | Content                                                                                        |
| ------------------------ | ---------------------------------------------------------------------------------------------- |
| `backend/`               | Go service (`chi`, `pgx`, `clickhouse-go`), migrations in `backend/migrations/`                |
| `web/`                   | React, TypeScript, Vite, Tailwind v4                                                           |
| `api/openapi.yaml`       | OpenAPI 3.1, checked against the real routes by a Go test                                      |
| `deploy/`                | `compose.yaml`, the development overlay, the systemd unit and installer (`deploy/standalone/`) |
| `sdk/typescript/`        | `@ervisio/logs` ingest client                                                                  |
| `integrations/`          | FiveM resource, Ervisio plugin                                                                 |
| `packages/admin-client/` | Shared typed management client for the web app and plugin                                      |
| `scripts/`               | `gen-env.sh`, `release.sh`, test helpers                                                       |
| `docs/adr/`              | Architecture decision records                                                                  |
| `ervisio-logs-spec/`     | The development kit (source of truth)                                                          |

## Development

```sh
make dev-up             # databases published on 127.0.0.1 only
cd backend && set -a && . ../.env && set +a   # then export ERVISIO_LOGS_* (see below)
cd web && npm run dev   # Vite on :5173, proxies /api, /healthz, /readyz to :8080
make lint test          # unit tests and linters
make test-integration   # API and migrations against real PostgreSQL and ClickHouse (dev stack up; WIPES its data)
```

Configure infrastructure through `ERVISIO_LOGS_*` environment variables; secrets also accept `_FILE`. Edit runtime
settings in Administration, Workspace settings or Project settings. Each setting shows its source; an environment
override locks the corresponding runtime value until you change it on the host and restart. Run
`ervisio-logs config-check` to validate. Required: `ERVISIO_LOGS_POSTGRES_DSN`, `ERVISIO_LOGS_CLICKHOUSE_PASSWORD`,
`ERVISIO_LOGS_SECRET_KEY` (at least 32 characters). `ERVISIO_LOGS_PUBLIC_URL` is required by Compose. Environment
settings and defaults are listed in `.env.example`. There are no default credentials. `.env` is git-ignored;
`.env.example` holds placeholders only. `scripts/gen-env.sh` (also `make env`) is safe to re-run: it only appends
required keys missing from an existing `.env` and never changes the other lines.

The server never migrates on its own. `ervisio-logs migrate up` (the Compose `migrate` job) must finish first, and
`/readyz` stays at 503 while a migration is pending. `ervisio-logs migrate down --store postgres|clickhouse [-n N]`
reverts one store at a time; `--store` is mandatory because the stores have independent histories.

## Security notes

- Databases are not published to the host in the base Compose file; the app listens on loopback only. Put a TLS reverse
  proxy in front for anything else.
- Containers run read-only (app), non-root, with capabilities dropped.
- `ERVISIO_LOGS_ENV=production` refuses placeholder secrets, `sslmode=disable` and a non-https public URL. The bundled
  PostgreSQL serves TLS with a self-signed certificate from the `pg-tls` job, which the app verifies (`verify-full`), so
  production works with it once the public URL is https. `.env.example` still starts in `development` for
  `http://localhost`.
- Do not expose the service to untrusted networks without the TLS reverse proxy; set `ERVISIO_LOGS_TRUST_PROXY=true`
  only behind one that sets `X-Forwarded-For` itself.

## Validation before deployment

The 0.2 work includes unit and mocked browser checks, a virtual passkey authenticator and an isolated Collector fixture
(`python3 scripts/collector-test.py`). The Docker daemon did not respond during local validation, so this session did
not run database integration, archive, full-stack smoke or real Collector tests. The plugin’s actual Ervisio host/socket
grants, physical passkeys, external OAuth providers and in-game FiveM hooks need host testing.

Usage bytes measure uncompressed text payload. Daily quota counters live in each running process; restarting resets
them, and replicas do not share their counts. These counters are not a durable billing ledger.

## Builds on GitHub Actions

The `ci` workflow runs unit and integration checks, real browser E2E, archive-provider contracts,
smoke and a real OpenTelemetry Collector on Linux runners. Docker is not required on the development machine.
Pull requests start the workflow automatically; after merging the workflow it can also be started manually.

The `artifacts` job uploads build candidates for 14 days: Linux amd64/arm64 standalone packages,
Compose bundle, Ervisio plugin, TypeScript SDK, FiveM resource, a multi-platform OCI image archive and checksums.
Download them from the workflow run's **Artifacts** section. The `verified` job succeeds only when every check
and build passes. Build candidates do not publish a GitHub release or push an image to GHCR; the separate
`release` workflow performs publication from an approved version tag or a manual run on main.
