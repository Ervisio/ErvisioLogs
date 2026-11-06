# Ervisio Logs: Docker Compose

The Compose bundle runs the app, PostgreSQL, ClickHouse and a one-shot migration job on one host, from the published
image. Requirements: Docker Engine with Compose 2.23.1 or later. Not tried from a published image yet (ADR 0011).

ClickHouse starts with a low-CPU configuration defined at the bottom of `compose.yaml` (`configs:`): the `system.*`
log tables are off, the background pools are small and queries use at most 4 threads. On a large host, raise
`background_pool_size` and `max_threads` there.

## Install

```sh
tar xzf ervisio-logs-<version>-docker.tar.gz
cd ervisio-logs-<version>-docker
scripts/gen-env.sh          # creates .env (mode 600) with random passwords and secret key; prints no secrets
docker compose --env-file .env -f compose.yaml up -d --wait
```

The app listens on `127.0.0.1:8080` (`ERVISIO_LOGS_PORT` changes the port). The databases are not published to the
host. Put a TLS reverse proxy in front for anything beyond local use, then set in `.env`:

```sh
ERVISIO_LOGS_ENV=production
ERVISIO_LOGS_PUBLIC_URL=https://logs.example.com
ERVISIO_LOGS_TRUST_PROXY=true
```

Production refuses `sslmode=disable`. The bundled PostgreSQL serves TLS: the one-shot `pg-tls` job writes a
self-signed certificate into the `pg-tls` volume on first start and keeps it afterwards, and the app checks the
server against it (`sslmode=verify-full`). To use another PostgreSQL, set `ERVISIO_LOGS_POSTGRES_DSN`.

The first administrator signs in with the setup code: set `ERVISIO_LOGS_SETUP_CODE` in `.env`, or read the generated
code with `docker compose -f compose.yaml logs app`.

## Image

`compose.yaml` uses `ghcr.io/ervisio/ervisio-logs:<version>`. Set `ERVISIO_LOGS_IMAGE` in `.env` to use a mirror or a
digest (`ghcr.io/ervisio/ervisio-logs@sha256:...`). The image is distroless, runs as uid 65532 with a read-only root
filesystem and no capabilities, and has a `HEALTHCHECK`.

## Upgrade

```sh
# unpack the new bundle over the old directory, keeping .env
docker compose --env-file .env -f compose.yaml pull
docker compose --env-file .env -f compose.yaml up -d --wait
```

The migration job runs before the app starts.

## Back up

The volumes are `pg-data`, `ch-data`, `spool` and `cache` in the `ervisio-logs` project. Back up PostgreSQL with
`docker compose -f compose.yaml exec postgres pg_dump -U ervisio_logs -Fc ervisio_logs > ervisio_logs.dump`, and keep
`.env`: without `ERVISIO_LOGS_SECRET_KEY` every API token and webhook signing secret stops working.

## Remove

`docker compose -f compose.yaml down` stops the stack and keeps the volumes. Only `down -v` deletes the data.
