#!/usr/bin/env bash
# Runs the D001 benchmark in its own Compose project and removes everything afterwards (results stay in ./results).
# Credentials are random per run, live only in a temporary env file and are never printed.
# Optional env: EVENTS (default 10000000), LATENCY_MS (25), RUNS (3), KEEP=1 to leave the stack up,
# ONLY_ROBUSTNESS=1 to skip the timed benchmark and only run the damaged-segment checks.
set -euo pipefail
cd "$(dirname "$0")"
env_file="$(mktemp)"
compose() { docker compose -p ervlogs-d001 --env-file "$env_file" "$@"; }
cleanup() {
  [ "${KEEP:-0}" = 1 ] || compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$env_file"
}
trap cleanup EXIT
rand() { python -c 'import secrets; print(secrets.token_hex(20))'; }
{
  echo "S3_ACCESS_KEY=$(rand)"
  echo "S3_SECRET_KEY=$(rand)"
  echo "CLICKHOUSE_PASSWORD=$(rand)"
  echo "EVENTS=${EVENTS:-10000000}"
  echo "LATENCY_MS=${LATENCY_MS:-25}"
  echo "RUNS=${RUNS:-3}"
} >"$env_file"
mkdir -p results
compose build -q
compose up -d s3 sftp clickhouse
compose run --rm --no-deps bench python gen.py
[ "${ONLY_ROBUSTNESS:-0}" = 1 ] || compose run --rm --no-deps bench python bench.py
compose run --rm --no-deps bench python robustness.py
