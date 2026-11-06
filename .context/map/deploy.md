# deploy
Purpose: build, package and run Ervisio Logs (single Go binary + embedded web UI, PostgreSQL + ClickHouse) as Compose stack or systemd service; release pipeline.
Entry points: Dockerfile, Makefile, deploy/compose.yaml (+compose.dev.yaml, archive-test/), deploy/standalone/{install.sh,uninstall.sh,ervisio-logs.service,ervisio-logs.env.example}, scripts/{gen-env,release,smoke,e2e,archive-test}.sh, .github/workflows/{ci,release}.yml, docs/deploy/{docker,standalone,archive-sftp,ervisio-plugin}.md, docs/releases/<ver>.md.

Local flow: `make env` (scripts/gen-env.sh: creates .env mode 600 from .env.example with random POSTGRES_PASSWORD, CLICKHOUSE_PASSWORD, ERVISIO_LOGS_SECRET_KEY(48), PUBLIC_URL; idempotent, only appends missing keys) -> `make up` (compose build+--wait) | `make dev-up` (DBs on 127.0.0.1, compose.dev.yaml) | down/dev-down/logs.
CI `.github/workflows/ci.yml`: push main, PR e avvio manuale; concorrenza per branch/PR. Job backend/web/client/OpenAPI, PG+CH integration, archive S3/SFTP, smoke, real E2E, Collector. Artifacts: release.sh + SDK/FiveM + immagine OCI amd64/arm64 senza push; upload14giorni. `verified` richiede tutti i job success. Docker locale fermato volutamente; non avviarlo.
Checks: `make ci` = lint (go vet, gofmt, eslint, tsc) + test + openapi-lint (redocly pinned). `make test-integration` DESTRUCTIVE (wipes dev DBs, needs dev stack up, fails on any skipped test). `make smoke`, `make e2e` (Playwright), `make archive-test` (SeaweedFS S3 + OpenSSH SFTP containers).
Image: Dockerfile 3 stages (node:24 web build -> golang:1.27 build, web dist copied into backend/internal/webui/dist for go:embed -> distroless static nonroot uid 65532). Base images pinned by digest. ldflags set internal/version.{Version,Commit} from ARG VERSION/COMMIT (default 0.0.0-dev). Entrypoint `ervisio-logs serve`; subcommands: healthcheck, tls-cert, version, migrate.
Compose services: pg-tls (one-shot self-signed cert, volume pg-tls), postgres, clickhouse, migrate (one-shot; app not ready until done), app (127.0.0.1:${ERVISIO_LOGS_PORT:-8080}->8080, read_only, cap_drop ALL, volumes spool/cache/pg-tls, archive secrets dir ERVISIO_LOGS_ARCHIVE_SECRETS_DIR). Network edge `ervisiologs_edge` for Caddy-in-Docker.
Required env (compose fails with "run scripts/gen-env.sh"): POSTGRES_PASSWORD, CLICKHOUSE_PASSWORD, ERVISIO_LOGS_SECRET_KEY (>=32 chars), ERVISIO_LOGS_PUBLIC_URL. Optional: ERVISIO_LOGS_ENV, _PORT, _TRUST_PROXY, _OPEN_REGISTRATION, _SETUP_CODE, OAuth (GOOGLE/COGNITO/DISCORD), _SPOOL_MAX_BYTES, _METRICS_TOKEN, ERVISIO_LOGS_IMAGE.

Release / versioning (ADR 0011/0012/0014):
1. Single source of version = git tag `vX.Y.Z`; backend/image get it via ldflags, nothing in Go code is edited.
2. Manual bump files for X.Y.Z: integrations/ervisio-plugin/package.json "version", integrations/ervisio-plugin/plugin/manifest.json "version" (both MUST equal), and new docs/releases/X.Y.Z.md (release notes, required). Package-lock of plugin not checked. (0.1.5 commit "prepare 0.1.5" touched exactly these.)
3. Merge to main, then either push tag `vX.Y.Z` or run workflow_dispatch `release` with version X.Y.Z (must run from main, stable semver; it tags the checkout after artifacts build).
4. release.yml: validates versions+notes -> buildx multi-arch (amd64,arm64) image pushed to ghcr.io/ervisio/ervisio-logs:X.Y.Z (no `latest` tag) -> scripts/release.sh -> `gh release create --draft` with dist/X.Y.Z/*  (draft: publish manually).
5. scripts/release.sh (VERSION=, IMAGE=, SKIP_WEB=1) writes dist/<v>/: linux-{amd64,arm64} tarballs (binary + systemd unit + install.sh), -docker bundle (compose.yaml with `build:` stripped and image pinned to :version), plugin tarball ervisio-logs-<v>.tar.gz(+.sha256) and -ervisio-plugin copy, SHA256SUMS. Plugin build embeds compose+env into src/bundle.generated.ts and standalone checksums into bin/release.sha256.
Standalone install: `install.sh` in tarball, config /etc/ervisio-logs/ervisio-logs.env, service ervisio-logs.service, secrets /etc/ervisio-logs/secrets.
Invariants: plugin archive reproducible (sorted, owner 0, mtime=commit time, gzip -n); never ship manifest.sig; the docker bundle must not contain `build:`; compose.yaml is the single source for both dev and bundle (sed-transformed).
Gotchas:
- release.sh uses GNU tar flags (--sort, --owner): run on Linux/CI, not macOS, and `sha256sum`.
- Plugin git SDK dependency needs the ssh-to-https insteadOf rewrite on runners. Shared packages/admin-client must stay at repository-relative path in isolated builds; release bundles preserve dependency notices.
- Default VERSION without tag is `git describe` (may include -dirty); plugin check compares only the part before `+`.
- SDK (sdk/typescript) is not part of the release pipeline.
Related: map/backend.md, map/web.md, map/integrations.md, map/sdk.md
