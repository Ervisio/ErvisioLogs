#!/usr/bin/env bash
# Removes the Ervisio Logs service and binary. It never deletes data: the configuration, the spool, the
# archive cache and the PostgreSQL/ClickHouse databases stay, and the script lists them for manual removal.
set -euo pipefail

[[ $EUID -eq 0 ]] || { echo "uninstall.sh: run as root" >&2; exit 1; }

if systemctl list-unit-files ervisio-logs.service >/dev/null 2>&1; then
  systemctl disable --now ervisio-logs.service 2>/dev/null || true
fi
rm -f /etc/systemd/system/ervisio-logs.service /usr/local/bin/ervisio-logs
systemctl daemon-reload
echo "removed the ervisio-logs service and binary"

echo "kept (delete by hand once you no longer need them):"
for p in /etc/ervisio-logs /var/lib/ervisio-logs /var/lib/clickhouse/user_files/ervisio-archive; do
  if [[ -e "$p" ]]; then echo "  $p"; fi
done
echo "  the ervisio_logs databases on PostgreSQL and ClickHouse, and the archive bucket or SFTP folder"
echo "  the system user ervisio-logs (userdel ervisio-logs)"
echo "Unsent events may still be in /var/lib/ervisio-logs/spool: reinstalling the service delivers them."
