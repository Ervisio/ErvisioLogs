# Ervisio Logs

Self-hosted, API-first, multi-tenant log management. Standalone first, optional Ervisio plugin later.

Status: **P0 to P3 implemented; P4 to P7 not started.** You can create the first administrator, a workspace and a
project, issue an API token, send events over HTTP and find them in the Log Explorer or the live tail. Archives,
retention, SDKs, alerts and the Ervisio plugin do not exist yet. See
[`ervisio-logs-spec/implementation/PROGRESS.md`](ervisio-logs-spec/implementation/PROGRESS.md).

## Quick start

Requirements: Docker with Compose v2.

```sh
make up                 # creates or completes .env with random secrets, builds, starts, waits for health
open http://localhost:8080
make down
```

`make smoke` runs the full end-to-end check in an isolated Compose project and removes it afterwards.
`make e2e` (or `scripts/e2e.sh`) does the same with a browser: it starts a fresh stack with random secrets, runs the
Playwright suite in `web/e2e/` (first administrator, project, token, ingest, search, phone layout, dark theme) and
removes the stack. Pass Playwright arguments through, for example `scripts/e2e.sh --headed`.

## First run and sign-in

The first person to open the app enters the setup code and becomes the instance administrator; afterwards the code does
nothing. Set it with `ERVISIO_LOGS_SETUP_CODE`, or leave it unset and read the generated one from the app log
(`make logs`). Self-registration stays off unless
you set `ERVISIO_LOGS_OPEN_REGISTRATION=true`. Google, AWS Cognito and Discord sign-in turn on when you set their client id and
secret (see `.env.example`); they have not been tested against the real providers yet.

Members belong to a workspace. Owners and admins add people by email under Settings, Users; the person needs an
account first. Servers send events with an API token (`evl_...`) created under Settings, API tokens:

```sh
curl -X POST http://localhost:8080/api/v1/ingest   -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json"   -d '{"level":"info","event":"player.joined","message":"ada joined","attributes":{"user":"ada"}}'
```

## Layout

| Path | Content |
|---|---|
| `backend/` | Go service (`chi`, `pgx`, `clickhouse-go`), migrations in `backend/migrations/` |
| `web/` | React, TypeScript, Vite, Tailwind v4 |
| `api/openapi.yaml` | OpenAPI 3.1, checked against the real routes by a Go test |
| `deploy/` | `compose.yaml` and the development overlay |
| `docs/adr/` | Architecture decision records |
| `ervisio-logs-spec/` | The development kit (source of truth) |

## Development

```sh
make dev-up             # databases published on 127.0.0.1 only
cd backend && set -a && . ../.env && set +a   # then export ERVISIO_LOGS_* (see below)
cd web && npm run dev   # Vite on :5173, proxies /api, /healthz, /readyz to :8080
make lint test          # unit tests and linters
make test-integration   # API and migrations against real PostgreSQL and ClickHouse (dev stack up; WIPES its data)
```

Backend configuration is environment only (`ERVISIO_LOGS_*`, each secret also accepts `_FILE`). Run
`ervisio-logs config-check` to validate. Required: `ERVISIO_LOGS_POSTGRES_DSN`, `ERVISIO_LOGS_CLICKHOUSE_PASSWORD`,
`ERVISIO_LOGS_SECRET_KEY` (at least 32 characters). `ERVISIO_LOGS_PUBLIC_URL` is required by Compose. Optional settings
(registration, setup code, session TTL, proxy trust, OAuth providers) are listed in `.env.example`. There are no default
credentials. `.env` is git-ignored; `.env.example` holds placeholders only. `scripts/gen-env.sh` (also `make env`) is
safe to re-run: it only appends required keys missing from an existing `.env` and never changes the other lines.

The server never migrates on its own. `ervisio-logs migrate up` (the Compose `migrate` job) must finish first, and
`/readyz` stays at 503 while a migration is pending. `ervisio-logs migrate down --store postgres|clickhouse [-n N]`
reverts one store at a time; `--store` is mandatory because the stores have independent histories.

## Security notes for P0

- Databases are not published to the host in the base Compose file; the app listens on loopback only. Put a TLS
  reverse proxy in front for anything else.
- Containers run read-only (app), non-root, with capabilities dropped.
- `ERVISIO_LOGS_ENV=production` refuses placeholder secrets, `sslmode=disable` and a non-https public URL. Compose
  defaults to `development` because its bundled PostgreSQL has no TLS yet: for production set
  `ERVISIO_LOGS_POSTGRES_DSN` to a TLS-enabled server (or configure TLS on the bundled one and set `POSTGRES_SSLMODE`),
  and `ERVISIO_LOGS_CLICKHOUSE_ADDR` / `ERVISIO_LOGS_CLICKHOUSE_TLS` as needed.
- Do not expose the service to untrusted networks without the TLS reverse proxy; set `ERVISIO_LOGS_TRUST_PROXY=true`
  only behind one that sets `X-Forwarded-For` itself.
