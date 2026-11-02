#!/usr/bin/env bash
# Creates .env from .env.example with freshly generated random passwords. Never overwrites.
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ -e .env ]]; then echo ".env already exists; leaving it untouched" >&2; exit 0; fi
rand() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 32; }
umask 077
sed -e "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$(rand)/" \
    -e "s/^CLICKHOUSE_PASSWORD=.*/CLICKHOUSE_PASSWORD=$(rand)/" .env.example > .env
echo "created .env (mode 600)"
