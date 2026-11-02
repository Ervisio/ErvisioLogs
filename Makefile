SHELL := /bin/bash
COMPOSE := docker compose --env-file .env -f deploy/compose.yaml
COMPOSE_DEV := $(COMPOSE) -f deploy/compose.dev.yaml

.DEFAULT_GOAL := help
.PHONY: help env up down logs dev-up dev-down build lint test test-backend test-web test-integration smoke openapi-lint ci

help: ## List targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

env: ## Create .env with random passwords (never overwrites)
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

test-integration: env ## Migration tests against real PostgreSQL + ClickHouse (dev stack must be up)
	@set -a; . ./.env; set +a; \
	ERVISIO_LOGS_POSTGRES_DSN="postgres://ervisio_logs:$$POSTGRES_PASSWORD@127.0.0.1:$${DEV_POSTGRES_PORT:-5432}/ervisio_logs?sslmode=disable" \
	ERVISIO_LOGS_CLICKHOUSE_ADDR="127.0.0.1:$${DEV_CLICKHOUSE_NATIVE_PORT:-9000}" \
	ERVISIO_LOGS_CLICKHOUSE_DATABASE=ervisio_logs ERVISIO_LOGS_CLICKHOUSE_USER=ervisio_logs \
	ERVISIO_LOGS_CLICKHOUSE_PASSWORD="$$CLICKHOUSE_PASSWORD" \
	  go -C backend test -tags integration -count=1 ./internal/migrate/

smoke: ## Full end-to-end smoke test in an isolated Compose project
	scripts/smoke.sh

openapi-lint: ## Lint api/openapi.yaml
	npx --yes @redocly/cli@latest lint api/openapi.yaml

ci: lint test openapi-lint ## What CI runs (minus Docker jobs)
