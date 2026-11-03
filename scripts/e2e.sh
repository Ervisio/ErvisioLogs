#!/usr/bin/env bash
# Browser end-to-end test: builds and starts the whole stack in an isolated Compose project with
# fresh databases and a random setup code, runs Playwright against it, then removes it (volumes included).
set -euo pipefail
cd "$(dirname "$0")/.."

export COMPOSE_PROJECT_NAME="ervlogs-e2e-$$"
ENVFILE="$(mktemp)"
PORT="${E2E_PORT:-18090}"
rand() { head -c 1024 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9' | cut -c "1-$1"; }
SETUP_CODE="$(rand 24)"
{
  echo "POSTGRES_PASSWORD=$(rand 24)"
  echo "CLICKHOUSE_PASSWORD=$(rand 24)"
  echo "ERVISIO_LOGS_SECRET_KEY=$(rand 48)"
  echo "ERVISIO_LOGS_PUBLIC_URL=http://127.0.0.1:$PORT"
  echo "ERVISIO_LOGS_PORT=$PORT"
  echo "ERVISIO_LOGS_SETUP_CODE=$SETUP_CODE"
} > "$ENVFILE"
dc() { docker compose --env-file "$ENVFILE" -f deploy/compose.yaml "$@"; }
cleanup() {
  status=$?
  if [[ $status != 0 ]]; then dc logs --tail=60 app >&2 || true; fi
  dc down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$ENVFILE"
  exit "$status"
}
trap cleanup EXIT

dc up -d --build --wait --wait-timeout 300

cd web
[[ -d node_modules ]] || npm ci
# CI runners also need the browser's system libraries.
npx playwright install ${CI:+--with-deps} chromium >/dev/null
# The setup code reaches Playwright through the environment only; it is never printed.
E2E_BASE_URL="http://127.0.0.1:$PORT" E2E_SETUP_CODE="$SETUP_CODE" npx playwright test "$@"
