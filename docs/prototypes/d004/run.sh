#!/usr/bin/env bash
# Runs the D004/D005 prototype in its own Compose project and removes it afterwards (results stay in ./results).
# The ClickHouse password is random per run and never printed.
# Optional env: PROJECTS (24), DAYS (4), ROWS_PER_PROJECT_DAY (200000), KEEP=1 to leave the stack up.
set -euo pipefail
cd "$(dirname "$0")"
env_file="$(mktemp)"
compose() { docker compose -p ervlogs-d004 --env-file "$env_file" "$@"; }
cleanup() {
  [ "${KEEP:-0}" = 1 ] || compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$env_file"
}
trap cleanup EXIT
{
  echo "CLICKHOUSE_PASSWORD=$(python -c 'import secrets; print(secrets.token_hex(20))')"
  echo "PROJECTS=${PROJECTS:-24}"
  echo "DAYS=${DAYS:-4}"
  echo "ROWS_PER_PROJECT_DAY=${ROWS_PER_PROJECT_DAY:-200000}"
} >"$env_file"
mkdir -p results
compose build -q
compose up -d clickhouse
compose run --rm --no-deps bench python bench.py
