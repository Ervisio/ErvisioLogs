#!/usr/bin/env bash
# Runs the archive provider contract tests against real S3 (SeaweedFS) and SFTP (OpenSSH) servers.
# Credentials and keys are random per run, live in a temporary directory and are never printed.
# KEEP=1 leaves the servers up.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
# Inside the repository (gitignored) so Docker Desktop can bind-mount it on Windows too.
keys="$root/.archive-test/$(date +%s)-$$"
mkdir -p "$keys"
export KEYS_DIR="$keys"
command -v cygpath >/dev/null && KEYS_DIR="$(cygpath -m "$keys")"
s3_port="${ARCHIVE_TEST_S3_PORT:-18333}"
sftp_port="${ARCHIVE_TEST_SFTP_PORT:-12222}"
compose() { docker compose -f "$root/deploy/archive-test/compose.yaml" "$@"; }
cleanup() {
  [ "${KEEP:-0}" = 1 ] || compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$keys"
}
trap cleanup EXIT

ssh-keygen -q -t ed25519 -N '' -C archive-test-host -f "$keys/ssh_host_ed25519_key"
ssh-keygen -q -t ed25519 -N '' -C archive-test-client -f "$keys/client"
chmod 644 "$keys/ssh_host_ed25519_key" "$keys/client" # the sftp container copies them to 0600
access="$(python -c 'import secrets; print(secrets.token_hex(10))')"
secret="$(python -c 'import secrets; print(secrets.token_hex(20))')"
printf '{"identities":[{"name":"test","credentials":[{"accessKey":"%s","secretKey":"%s"}],"actions":["Admin","Read","List","Write"]}]}' \
  "$access" "$secret" >"$keys/s3.json"
printf '{"access_key":"%s","secret_key":"%s"}' "$access" "$secret" >"$keys/s3-secret.json"

compose up -d --wait
export ERVISIO_LOGS_TEST_S3_ENDPOINT="http://127.0.0.1:$s3_port"
export ERVISIO_LOGS_TEST_S3_SECRET_FILE="$keys/s3-secret.json"
export ERVISIO_LOGS_TEST_SFTP_ADDR="127.0.0.1:$sftp_port"
export ERVISIO_LOGS_TEST_SFTP_KEY_FILE="$keys/client"
export ERVISIO_LOGS_TEST_SFTP_HOST_KEY_FILE="$keys/ssh_host_ed25519_key.pub"

log="$(mktemp)"
go -C "$root/backend" test -v -tags integration -count=1 -run 'Contract' ./internal/archive/ 2>&1 | tee "$log"
status=${PIPESTATUS[0]}
if grep -Eq -- '--- SKIP|no test files' "$log"; then echo "archive-test: skipped tests are not allowed" >&2; status=1; fi
rm -f "$log"
exit "$status"
