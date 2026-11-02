#!/usr/bin/env bash
# Creates .env from .env.example with random secrets, or appends the required keys missing from an
# existing .env. Idempotent: existing lines are never changed. Secret values are never printed.
set -euo pipefail
cd "$(dirname "$0")/.."

# head ends on its own, so no pipeline stage dies of SIGPIPE under pipefail.
rand() { head -c 1024 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9' | cut -c "1-$1"; }

umask 077
if [[ ! -e .env ]]; then
  tmp="$(mktemp .env.XXXXXX)"
  trap 'rm -f "$tmp"' EXIT
  sed -e "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$(rand 32)/" \
      -e "s/^CLICKHOUSE_PASSWORD=.*/CLICKHOUSE_PASSWORD=$(rand 32)/" \
      -e "s/^ERVISIO_LOGS_SECRET_KEY=.*/ERVISIO_LOGS_SECRET_KEY=$(rand 48)/" .env.example > "$tmp"
  mv "$tmp" .env
  trap - EXIT
  echo "created .env (mode 600)"
  exit 0
fi

has_key() { grep -Eq "^(export[[:space:]]+)?$1=" .env; }
value_of() { sed -nE "s/^(export[[:space:]]+)?$1=//p" .env | tail -n 1; }

add() {
  # A last line without a newline would otherwise be merged with the new key.
  if [[ -s .env && -n "$(tail -c 1 .env)" ]]; then echo >> .env; fi
  printf '%s=%s\n' "$1" "$2" >> .env
  echo "added $1 to .env"
}

port="$(value_of ERVISIO_LOGS_PORT)"
has_key POSTGRES_PASSWORD || add POSTGRES_PASSWORD "$(rand 32)"
has_key CLICKHOUSE_PASSWORD || add CLICKHOUSE_PASSWORD "$(rand 32)"
has_key ERVISIO_LOGS_SECRET_KEY || add ERVISIO_LOGS_SECRET_KEY "$(rand 48)"
has_key ERVISIO_LOGS_PUBLIC_URL || add ERVISIO_LOGS_PUBLIC_URL "http://localhost:${port:-8080}"

# Present but unusable values are the owner's to fix: report the key, not the value.
for key in POSTGRES_PASSWORD CLICKHOUSE_PASSWORD ERVISIO_LOGS_SECRET_KEY ERVISIO_LOGS_PUBLIC_URL; do
  [[ -n "$(value_of "$key")" ]] || echo "warning: $key is empty in .env; left untouched" >&2
done
secret="$(value_of ERVISIO_LOGS_SECRET_KEY)"
if [[ -n "$secret" && ${#secret} -lt 32 ]]; then
  echo "warning: ERVISIO_LOGS_SECRET_KEY is shorter than 32 characters in .env; left untouched" >&2
fi
echo ".env is complete; existing lines left untouched"
