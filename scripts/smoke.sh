#!/usr/bin/env bash
# End-to-end smoke test: builds and starts the whole stack in an isolated Compose project,
# checks the health endpoints and the migrations, then tears everything down (volumes included).
set -euo pipefail
cd "$(dirname "$0")/.."

export COMPOSE_PROJECT_NAME="ervlogs-smoke-$$"
ENVFILE="$(mktemp)"
PORT="${SMOKE_PORT:-18080}"
rand() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 24; }
{ echo "POSTGRES_PASSWORD=$(rand)"; echo "CLICKHOUSE_PASSWORD=$(rand)"; echo "ERVISIO_LOGS_PORT=$PORT"; } > "$ENVFILE"
dc() { docker compose --env-file "$ENVFILE" -f deploy/compose.yaml "$@"; }
cleanup() { dc down -v --remove-orphans >/dev/null 2>&1 || true; rm -f "$ENVFILE"; }
trap cleanup EXIT

fail() { echo "SMOKE FAIL: $*" >&2; dc logs --tail=40 >&2 || true; exit 1; }
base="http://127.0.0.1:$PORT"

dc up -d --build --wait --wait-timeout 240 || fail "stack did not become healthy"

code=$(curl -s -o /dev/null -w '%{http_code}' "$base/healthz"); [[ $code == 200 ]] || fail "/healthz returned $code"
body=$(curl -s "$base/readyz"); code=$(curl -s -o /dev/null -w '%{http_code}' "$base/readyz")
[[ $code == 200 ]] || fail "/readyz returned $code: $body"
for c in postgres clickhouse migrations_postgres migrations_clickhouse; do
  grep -q "\"$c\":\"ok\"" <<<"$body" || fail "check $c not ok: $body"
done
ctype=$(curl -s -o /dev/null -w '%{content_type}' "$base/api/v1/nope")
[[ $ctype == application/problem+json* ]] || fail "unknown API path content-type: $ctype"
curl -s "$base/" | grep -qi '<div id="root"' || fail "web UI not served"
# Databases must not be reachable from the host in the base stack.
if (exec 3<>/dev/tcp/127.0.0.1/5432) 2>/dev/null; then fail "postgres is published on the host"; fi
# Migrations are idempotent.
dc run --rm migrate migrate up | grep -q 'applied 0' || fail "second migrate up was not a no-op"
# Tables really exist.
dc exec -T postgres psql -U ervisio_logs -d ervisio_logs -tAc "select count(*) from instance_settings where key='instance_id'" | grep -qx 1 || fail "pg instance_id missing"
dc exec -T clickhouse clickhouse-client --user ervisio_logs --password "$(grep ^CLICKHOUSE_PASSWORD "$ENVFILE" | cut -d= -f2)" -q "EXISTS TABLE ervisio_logs.events" | grep -qx 1 || fail "ch events table missing"
echo "SMOKE OK"
