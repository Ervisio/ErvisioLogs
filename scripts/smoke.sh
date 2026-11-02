#!/usr/bin/env bash
# End-to-end smoke test: builds and starts the whole stack in an isolated Compose project,
# checks the health endpoints and the migrations, then tears everything down (volumes included).
set -euo pipefail
cd "$(dirname "$0")/.."

export COMPOSE_PROJECT_NAME="ervlogs-smoke-$$"
ENVFILE="$(mktemp)"
PORT="${SMOKE_PORT:-18080}"
# head ends on its own, so no pipeline stage dies of SIGPIPE under pipefail.
rand() { head -c 1024 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9' | cut -c "1-$1"; }
CH_PASSWORD="$(rand 24)"
{
  echo "POSTGRES_PASSWORD=$(rand 24)"
  echo "CLICKHOUSE_PASSWORD=$CH_PASSWORD"
  echo "ERVISIO_LOGS_SECRET_KEY=$(rand 48)"
  echo "ERVISIO_LOGS_PUBLIC_URL=http://127.0.0.1:$PORT"
  echo "ERVISIO_LOGS_PORT=$PORT"
} > "$ENVFILE"
dc() { docker compose --env-file "$ENVFILE" -f deploy/compose.yaml "$@"; }
cleanup() { dc down -v --remove-orphans >/dev/null 2>&1 || true; rm -f "$ENVFILE"; }
trap cleanup EXIT

fail() { echo "SMOKE FAIL: $*" >&2; dc logs --tail=40 >&2 || true; exit 1; }
base="http://127.0.0.1:$PORT"

# Asks Docker about this project's container, not the host port: another stack (such as the dev overlay)
# may legitimately listen on the same port.
assert_unpublished() {
  local service=$1 port=$2 id bindings
  id="$(dc ps -q "$service")"
  [[ -n $id ]] || fail "no container for $service"
  bindings="$(docker inspect --format "{{with index .NetworkSettings.Ports \"$port/tcp\"}}{{json .}}{{end}}" "$id")"
  [[ -z $bindings ]] || fail "$service port $port is published on the host: $bindings"
}

dc up -d --build --wait --wait-timeout 240 || fail "stack did not become healthy"

code=$(curl -s -o /dev/null -w '%{http_code}' "$base/healthz"); [[ $code == 200 ]] || fail "/healthz returned $code"
body=$(curl -s "$base/readyz"); code=$(curl -s -o /dev/null -w '%{http_code}' "$base/readyz")
[[ $code == 200 ]] || fail "/readyz returned $code: $body"
for c in postgres clickhouse migrations_postgres migrations_clickhouse; do
  grep -q "\"$c\":\"ok\"" <<<"$body" || fail "check $c not ok: $body"
done
ctype=$(curl -s -o /dev/null -w '%{content_type}' "$base/api/v1/nope")
[[ $ctype == application/problem+json* ]] || fail "unknown API path content-type: $ctype"
# Output is captured first: with pipefail, `curl | grep -q` fails when grep exits early and curl gets SIGPIPE.
page=$(curl -fsS "$base/") || fail "web UI request failed"
grep -qi '<div id="root"' <<<"$page" || fail "web UI not served"
# Databases must not be reachable from the host in the base stack.
assert_unpublished postgres 5432
assert_unpublished clickhouse 8123
assert_unpublished clickhouse 9000
# Migrations are idempotent.
out=$(dc run --rm -T migrate migrate up 2>&1) || fail "second migrate up failed: $out"
[[ $(grep -c 'applied 0 migration' <<<"$out") == 2 ]] || fail "second migrate up was not a no-op: $out"
# Tables really exist.
out=$(dc exec -T postgres psql -U ervisio_logs -d ervisio_logs -tAc "select count(*) from instance_settings where key='instance_id'") || fail "pg query failed: $out"
[[ $(tr -d '[:space:]' <<<"$out") == 1 ]] || fail "pg instance_id missing"
out=$(dc exec -T clickhouse clickhouse-client --user ervisio_logs --password "$CH_PASSWORD" -q "EXISTS TABLE ervisio_logs.events") || fail "ch query failed: $out"
[[ $(tr -d '[:space:]' <<<"$out") == 1 ]] || fail "ch events table missing"
echo "SMOKE OK"
