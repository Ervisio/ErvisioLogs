# Security checklist

What the code does today, and what the owner must check before exposing an instance. Items marked *unverified* were
written but not tested.

## Before going live
- [ ] TLS reverse proxy in front of the app; the app itself listens on `127.0.0.1:8080`.
- [ ] `ERVISIO_LOGS_ENV=production`. It refuses placeholder secrets, `sslmode=disable` and a non-https public URL.
- [ ] PostgreSQL reached over TLS (external server, or TLS configured on the bundled one).
- [ ] `ERVISIO_LOGS_TRUST_PROXY=true` only behind a proxy that sets `X-Forwarded-For` itself.
- [ ] First administrator created; the setup code is then inert. `ERVISIO_LOGS_OPEN_REGISTRATION` left off.
- [ ] `.env` (Docker) mode 600 or the plugin's 2770 `root:docker` folder; `/etc/ervisio-logs/ervisio-logs.env` 0640.
- [ ] S3 credentials scoped to the archive bucket only.
- [ ] `ERVISIO_LOGS_WEBHOOK_ALLOW_PRIVATE` left off unless webhooks go to a trusted LAN.
- [ ] Backups of PostgreSQL and of the archive bucket (no restore drill has been done yet).

## What the code enforces
- No default credentials. Secrets come from the environment or `_FILE`; `scripts/gen-env.sh`, `install.sh` and the
  plugin generate them and never print them.
- Containers: read-only root filesystem for the app, non-root (uid 65532), capabilities dropped, databases not
  published to the host. Standalone unit: `ProtectSystem=strict`, `NoNewPrivileges`, empty capability set, syscall
  filter (*unverified* with the Go runtime).
- API tokens are stored hashed and scoped; every privileged action is audited, exports included.
- Webhooks: https only, no private or loopback targets checked on the dialled address, no redirects, HMAC-signed
  payloads with timestamp; the API never returns the URL.
- CSV export escapes formula prefixes.
- Release packages carry SHA-256 checksums; the plugin checks the standalone tarball against checksums shipped inside
  it before running its installer.
- Uninstall paths (plugin, `uninstall.sh`, Docker stop) never delete data.

## Known gaps
- Webhook URLs, which carry Slack and Discord secrets, are stored in clear in PostgreSQL.
- D008 (archive encryption at rest beyond what the bucket provides) is open.
- The release image and binaries are not signed; the plugin is not signed by the Ervisio team.
- OAuth providers, OTLP receiver and the FiveM resource are untested against the real systems.
- No external security review has been done.
