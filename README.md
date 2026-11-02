# Ervisio Logs

Self-hosted, API-first, multi-tenant log management. Standalone first, optional Ervisio plugin later.

Status: **phase P0 (foundation) complete, P1 not started.** The service starts, reports health, migrates its
databases and serves an app shell. It cannot ingest or search logs yet. See
[`ervisio-logs-spec/implementation/PROGRESS.md`](ervisio-logs-spec/implementation/PROGRESS.md).

## Quick start

Requirements: Docker with Compose v2.

```sh
make up                 # creates .env with random passwords, builds, starts, waits for health
open http://127.0.0.1:8080
make down
```

`make smoke` runs the full end-to-end check in an isolated Compose project and removes it afterwards.

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
make test-integration   # migrations against real PostgreSQL and ClickHouse (dev stack up)
```

Backend configuration is environment only (`ERVISIO_LOGS_*`, each secret also accepts `_FILE`). Run
`ervisio-logs config-check` to validate. Required: `ERVISIO_LOGS_POSTGRES_DSN`, `ERVISIO_LOGS_CLICKHOUSE_PASSWORD`.
There are no default credentials. `.env` is git-ignored; `.env.example` holds placeholders only.

The server never migrates on its own. `ervisio-logs migrate up` (the Compose `migrate` job) must finish first, and
`/readyz` stays at 503 while a migration is pending.

## Security notes for P0

- Databases are not published to the host in the base Compose file; the app listens on loopback only. Put a TLS
  reverse proxy in front for anything else.
- Containers run read-only (app), non-root, with capabilities dropped.
- `ERVISIO_LOGS_ENV=production` refuses placeholder passwords and `sslmode=disable`. Compose defaults to
  `development` because PostgreSQL inside the Compose network has no TLS yet.
- No authentication exists in P0. Do not expose the service to untrusted networks.
