# ADR 0013: TLS for the bundled PostgreSQL in Compose

Status: Proposed. Written in 0.1.1. Checked with `go build`, `go vet` and `docker compose config` only. No stack
was started with it.

## Context
Production mode refuses `sslmode=disable`. Up to 0.1.0 the bundled PostgreSQL had no TLS, so a Compose stack in
production mode exited at start with "sslmode=disable is refused in production". The owner hit this through the
Ervisio plugin.

## Decision
- A new subcommand, `ervisio-logs tls-cert --dir DIR [--host NAMES] [--days N]`, writes a self-signed ECDSA P-256
  certificate and its key. It keeps the files if both exist, so the certificate stays the same across restarts.
- A one-shot `pg-tls` service runs it from the app image into the `pg-tls` volume, as uid 65532.
- PostgreSQL's entrypoint copies the key into `/var/lib/postgresql/tls`, owned by postgres with mode 0600 (the
  server refuses a key anyone else can read), then starts with `ssl=on`.
- The app and the migrate job mount the volume read-only. Their default DSN uses
  `sslmode=verify-full&sslrootcert=/var/lib/ervisio-logs/tls/server.crt`, which checks the server's name
  (`postgres`) against a certificate that is its own root.
- `pg_hba.conf` keeps the image default, so local tools that connect from the host with `sslmode=disable` (the
  integration tests, `make dev`) still work. Only the Compose network sees the database.

## Consequences
- Upgrading a 0.1.0 stack: the `pg-tls` job creates the certificate on first start; nothing else changes.
- The certificate lasts ten years. To rotate it, remove `server.crt` and `server.key` from the volume and restart.
- An external database still needs its own DSN.
