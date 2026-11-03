# Ervisio Logs: standalone install (systemd)

One static binary with the web UI built in, run by systemd. You provide PostgreSQL and ClickHouse.
This procedure has not been tried on a real host yet (ADR 0011); review it before you rely on it.

## Requirements

- Linux with systemd, amd64 or arm64.
- PostgreSQL 15 or later, with TLS. Create a database and an owner role for it:
  `CREATE ROLE ervisio_logs LOGIN PASSWORD '...'; CREATE DATABASE ervisio_logs OWNER ervisio_logs;`
- ClickHouse 25.8 with a database `ervisio_logs` and a user that owns it. The archive tier needs ClickHouse on the
  same machine, because ClickHouse reads archived segments from the cache directory with `file()`. With a remote
  ClickHouse, mount the same cache directory on both machines or leave the archive off.
- A TLS reverse proxy (Caddy, nginx, Traefik) in front of `127.0.0.1:8080`.

## Install

```sh
tar xzf ervisio-logs-<version>-linux-amd64.tar.gz
cd ervisio-logs-<version>-linux-amd64
sha256sum -c --ignore-missing ../SHA256SUMS
sudo ./install.sh
```

The installer:

- creates the system user `ervisio-logs`;
- installs `/usr/local/bin/ervisio-logs` and `/etc/systemd/system/ervisio-logs.service`;
- creates `/etc/ervisio-logs/ervisio-logs.env` (mode 640) with a random `ERVISIO_LOGS_SECRET_KEY`, once;
- points the archive cache at `/var/lib/clickhouse/user_files/ervisio-archive` when it finds a local ClickHouse;
- enables the service, and starts it only when the env file has no placeholders left.

Edit `/etc/ervisio-logs/ervisio-logs.env`: the PostgreSQL DSN, the ClickHouse password and the public URL. Then:

```sh
sudo systemctl start ervisio-logs
journalctl -u ervisio-logs -f
```

Each start runs `config-check` and `migrate up` before `serve`, so a bad setting stops the service with the reason in
the journal. When `ERVISIO_LOGS_SETUP_CODE` is unset, the first start writes a setup code to the journal: open the
public URL and enter it to create the first administrator.

## Reverse proxy

Live tail uses Server-Sent Events: turn response buffering off for `/api/v1/logs/tail`. Caddy needs nothing extra:

```
logs.example.com {
  reverse_proxy 127.0.0.1:8080
}
```

With nginx, set `proxy_buffering off;` and `proxy_read_timeout 1h;` on that location. Keep
`ERVISIO_LOGS_TRUST_PROXY=true` only when the proxy sets `X-Forwarded-For` itself.

## Upgrade

Unpack the new release and run `sudo ./install.sh` again. It replaces the binary and the unit, keeps the env file and
restarts the service; migrations run on start. Read the release notes first: a migration that cannot be reverted says
so there.

## Back up

- PostgreSQL holds accounts, projects, tokens, the archive catalog and alerts: `pg_dump -Fc ervisio_logs`.
- ClickHouse holds the hot tier. Archived days are in the bucket or SFTP folder, listed in the PostgreSQL catalog.
- `/var/lib/ervisio-logs/spool` holds events acknowledged but not yet in ClickHouse. Stop the service before you copy it.
- `/etc/ervisio-logs/ervisio-logs.env`, above all `ERVISIO_LOGS_SECRET_KEY`: without it, every API token and webhook
  signing secret stops working.

## Uninstall

`sudo ./uninstall.sh` stops and removes the service and the binary. It deletes no data: it lists the configuration,
spool, cache and databases it left in place, so you can remove them yourself.
