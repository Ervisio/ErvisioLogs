SHELL := /bin/bash
COMPOSE := docker compose --env-file .env -f deploy/compose.yaml
COMPOSE_DEV := $(COMPOSE) -f deploy/compose.dev.yaml
REDOCLY_VERSION := 2.72.1

.DEFAULT_GOAL := help
.PHONY: help env up down logs dev-up dev-down build lint test test-backend test-web test-integration smoke e2e openapi-lint ci

help: ## List targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

env: ## Create .env, or add missing required keys to it (never changes existing lines)
	@scripts/gen-env.sh

up: env ## Build and start the stack
	$(COMPOSE) up -d --build --wait

down: ## Stop the stack (keeps volumes)
	$(COMPOSE) down

logs: ## Follow app logs
	$(COMPOSE) logs -f app

dev-up: env ## Start with databases published on 127.0.0.1
	$(COMPOSE_DEV) up -d --build --wait

dev-down: ## Stop the dev stack
	$(COMPOSE_DEV) down

lint: ## go vet, gofmt check, eslint, tsc
	cd backend && go vet ./... && test -z "$$(gofmt -l .)"
	cd web && npm run lint && npm run typecheck

test-backend: ## Go unit tests
	cd backend && go test -race ./...

test-web: ## Frontend unit tests
	cd web && npm test

test: test-backend test-web ## All unit tests

# DESTRUCTIVE: both packages wipe the local dev databases and share them, hence -p 1.
# -v plus the grep turns any skipped test into a failure: this target must never be a false green.
test-integration: env ## API, ingest and migration tests against real PostgreSQL + ClickHouse (dev stack must be up; wipes it)
	@set -euo pipefail; set -a; . ./.env; set +a; \
	log="$$(mktemp)"; trap 'rm -f "$$log"' EXIT; \
	ERVISIO_LOGS_ENV=development \
	ERVISIO_LOGS_POSTGRES_DSN="postgres://ervisio_logs:$$POSTGRES_PASSWORD@127.0.0.1:$${DEV_POSTGRES_PORT:-5432}/ervisio_logs?sslmode=disable" \
	ERVISIO_LOGS_CLICKHOUSE_ADDR="127.0.0.1:$${DEV_CLICKHOUSE_NATIVE_PORT:-9000}" \
	ERVISIO_LOGS_CLICKHOUSE_DATABASE=ervisio_logs ERVISIO_LOGS_CLICKHOUSE_USER=ervisio_logs \
	ERVISIO_LOGS_CLICKHOUSE_PASSWORD="$$CLICKHOUSE_PASSWORD" \
	ERVISIO_LOGS_SECRET_KEY="$$ERVISIO_LOGS_SECRET_KEY" \
	ERVISIO_LOGS_PUBLIC_URL="$$ERVISIO_LOGS_PUBLIC_URL" \
	  go -C backend test -v -tags integration -count=1 -p 1 ./internal/api/ ./internal/ingest/ ./internal/migrate/ 2>&1 | tee "$$log"; \
	if grep -Eq -- '--- SKIP|no test files' "$$log"; then echo "test-integration: skipped tests are not allowed" >&2; exit 1; fi

smoke: ## Full end-to-end smoke test in an isolated Compose project
	scripts/smoke.sh

e2e: ## Browser tests (Playwright) against a fresh stack in an isolated Compose project
	scripts/e2e.sh

openapi-lint: ## Lint api/openapi.yaml
	npx --yes @redocly/cli@$(REDOCLY_VERSION) lint api/openapi.yaml

ci: lint test openapi-lint ## What CI runs (minus Docker jobs)
