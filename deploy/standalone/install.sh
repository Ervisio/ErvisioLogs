#!/usr/bin/env bash
# Installs or upgrades Ervisio Logs as a systemd service (ADR 0011). Run as root from the unpacked release
# directory. PostgreSQL and ClickHouse are not installed: point the env file at existing servers.
#
#   sudo ./install.sh            install or upgrade, start only if the configuration is complete
#   sudo ./install.sh --no-start install or upgrade, never start
#
# Idempotent. An existing /etc/ervisio-logs/ervisio-logs.env is never changed. Secrets are never printed.
# Not tried on a real host yet.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
start=1
for arg in "$@"; do
  case "$arg" in
    --no-start) start=0 ;;
    -h|--help) sed -n '2,9p' "$0"; exit 0 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

[[ "$(uname -s)" == Linux ]] || { echo "install.sh: Linux with systemd only" >&2; exit 1; }
[[ $EUID -eq 0 ]] || { echo "install.sh: run as root" >&2; exit 1; }
command -v systemctl >/dev/null || { echo "install.sh: systemd not found" >&2; exit 1; }
for f in ervisio-logs ervisio-logs.service ervisio-logs.env.example; do
  [[ -f "$here/$f" ]] || { echo "install.sh: $here/$f is missing" >&2; exit 1; }
done

user=ervisio-logs
etc=/etc/ervisio-logs
env_file="$etc/ervisio-logs.env"
bin=/usr/local/bin/ervisio-logs
unit=/etc/systemd/system/ervisio-logs.service
ch_files=/var/lib/clickhouse/user_files

if ! id -u "$user" >/dev/null 2>&1; then
  useradd --system --user-group --no-create-home --home-dir /var/lib/ervisio-logs --shell /usr/sbin/nologin "$user"
  echo "created system user $user"
fi

# Binary: write next to the target, then rename, so a running service never sees a half-written file.
install -m 0755 -o root -g root "$here/ervisio-logs" "$bin.new"
mv -f "$bin.new" "$bin"
echo "installed $("$bin" version)"

install -m 0644 -o root -g root "$here/ervisio-logs.service" "$unit"

install -d -m 0750 -o root -g "$user" "$etc"
created_env=0
if [[ ! -e "$env_file" ]]; then
  umask 077
  secret="$(head -c 1024 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9' | cut -c 1-48)"
  tmp="$(mktemp "$etc/.env.XXXXXX")"
  trap 'rm -f "$tmp"' EXIT
  sed -e "s/^ERVISIO_LOGS_SECRET_KEY=.*/ERVISIO_LOGS_SECRET_KEY=$secret/" "$here/ervisio-logs.env.example" > "$tmp"
  unset secret
  # A local ClickHouse can read the archive cache only from its user_files directory (ADR 0002).
  if [[ -d "$ch_files" ]] && getent group clickhouse >/dev/null; then
    install -d -m 2750 -o "$user" -g clickhouse "$ch_files/ervisio-archive"
    sed -i -e "s|^# ERVISIO_LOGS_ARCHIVE_CACHE_DIR=.*|ERVISIO_LOGS_ARCHIVE_CACHE_DIR=$ch_files/ervisio-archive|" "$tmp"
    echo "archive cache: $ch_files/ervisio-archive (readable by the clickhouse group)"
  fi
  chown root:"$user" "$tmp"
  chmod 0640 "$tmp"
  mv "$tmp" "$env_file"
  trap - EXIT
  created_env=1
  echo "created $env_file with a random ERVISIO_LOGS_SECRET_KEY"
else
  echo "kept existing $env_file"
fi

systemctl daemon-reload
systemctl enable ervisio-logs.service >/dev/null
echo "enabled ervisio-logs.service"

# Placeholders left in the env file mean the owner still has to fill it in.
incomplete=0
if grep -Eq '^ERVISIO_LOGS_CLICKHOUSE_PASSWORD=$|CHANGE_ME|logs\.example\.com' "$env_file"; then
  incomplete=1
fi

if [[ $start -eq 0 ]]; then
  echo "not started (--no-start)"
elif [[ $incomplete -eq 1 ]]; then
  echo "not started: edit $env_file (PostgreSQL DSN, ClickHouse password, public URL), then run:"
  echo "  systemctl start ervisio-logs && journalctl -u ervisio-logs -f"
elif [[ $created_env -eq 0 ]] && systemctl is-active --quiet ervisio-logs.service; then
  systemctl restart ervisio-logs.service
  echo "restarted ervisio-logs.service on the new binary"
else
  systemctl start ervisio-logs.service
  echo "started ervisio-logs.service: journalctl -u ervisio-logs -f"
fi
