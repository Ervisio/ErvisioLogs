# backend (Go) + API contract
Purpose: single Go binary `ervisio-logs` (module github.com/Ervisio/ErvisioLogs/backend) = HTTP API + ingest pipeline + archiver + alert evaluator + embedded web UI; contract in `api/openapi.yaml` (3.7k lines, single file, no separate api map).
Stores: Postgres (tenancy/auth/catalog/config) + ClickHouse (hot events) + remote Parquet archive (s3/sftp/local).
Entry points:
- backend/cmd/ervisio-logs/main.go:run (cmds: serve, migrate up|down|status, version, config-check, healthcheck, tls-cert)
- backend/internal/config/config.go:Load (all env `ERVISIO_LOGS_*`, `_FILE` suffix = read secret from file)
- backend/internal/server/server.go (/healthz /readyz /metrics, OTLP mount, webui fallback)
- backend/internal/api/api.go:Routes (all /api/v1 routes + `authenticate`), otlp.go:OTLPRoutes (POST /v1/logs, outside /api/v1)
Layout: api (handlers) | store (pgx SQL, one file per domain) | ingest (Validator, Writer, Hub) | wal | archive | auth | notify | oauth | migrate | httpx (RFC7807 problems, request id) | webui (go:embed dist) | migrations/{postgres,clickhouse} (embed.go)
Flow ingest: POST /ingest|/ingest/batch|/v1/logs → api/logs.go:ingestToken (bearer token, scope logs:ingest) → queueEvents → ingest.Validator.Normalize → ingest.EncodeRecord → wal.Segmented.Append (fsync group commit) → 202 "durably_queued" → ingest.Writer.Run reads WAL → ClickHouse insert (dedup token from spool id+seq range; inflight file for crash resend) → wal.Commit; ingest.Hub.Publish feeds live tail.
Flow read: POST /logs/search (api/logs_search.go) = hot ClickHouse UNION archived Parquet via archive.ReadSources (dedup by event_id); GET /logs/tail (SSE, logs_tail.go, Hub ring + `after` cursor); POST /logs/export; saved /queries.
Flow archive: archive.Archiver.Run (interval; Postgres advisory lock → one pass per instance) → closed stream-days → Parquet (chexport.go) → provider (s3.go/sftp.go/localfs.go) → catalog `segments` (store/archive.go, states incl. REMOTE_EXPIRED/DELETING/DELETED/FAILED) → evict.go:maintain (global expiry, covered-hot eviction, cache budget watermarks) → orphans.go:ReconcileOrphans (>1 day old).
Flow alerts: api/alerts_eval.go:RunAlerts → evaluateDue → countRule (ClickHouse) → notify (webhook.go, channels with rotate-secret/test).
Auth: api.go:authenticate resolves Bearer API token (HMAC hash with SECRET_KEY) OR session cookie; never rejects itself, handlers call requireUser/requireAdmin/workspaceAccess(perm). Permissions in auth/perms.go (roles owner/admin/developer/analyst/viewer). OAuth google/discord/cognito (oauth/, oauth_handlers.go). First run: EnsureSetupCode logs a one-time code to create instance admin.
Invariants:
- Isolation: every ClickHouse query filters workspace_id (+project_id); API tokens are bound to one workspace+project (token.WorkspaceID/ProjectID); cookie users go through workspaceAccess.
- Non-member of a workspace gets 404 (not 403) so existence is hidden; missing perm = 403.
- Instance admin (is_instance_admin) grants NO tenant data access; /admin/* only.
- Token scopes limited to logs:ingest|read|export and never exceed creator perms.
- Cookie-authed unsafe requests need X-CSRF header == session CSRFToken AND Origin == PUBLIC_URL origin; Bearer requests skip both.
- Ack (202) only after WAL durable. WAL full → 503 + Retry-After 5; oversize → 413; archiver OverCapacity() → ingest 503 (uncovered hot rows never evicted).
- Event limits (attributes depth/keys, clip, future skew/past age) live in ingest/event.go + config (INGEST_MAX_FUTURE_SKEW/PAST_AGE).
- Retention policy scopes instance>workspace>project>stream, NULL inherits (retention_policies table); no CH TTL, retention manager alone deletes.
- Evict order: mark segment as read source FIRST, then delete hot rows (else search briefly misses rows).
- Migrations: NNNN_name.up/down.sql, checksum recorded; editing an applied migration errors; no down file = irreversible. PG 0001-0005 (instance, tenancy_auth, archive, integrations, settings), CH 0001-0003 (events, dedup, inserted_at). Add new ones with next number, never edit.
- Webhooks refuse private/loopback/CGNAT/link-local dests unless alerts.webhook_allow_private (notify.NewClient takes a func, read per dial).
- Runtime settings (ADR 0018): internal/settings Service (snapshot, reload 30s, OnLoad sets slog LevelVar); handlers read a.Settings.Bool/Int/Duration/ProjectInt, never cfg, for keys in settings.defs. Env-set key = locked (cfg.FromEnv). Routes in api/settings_handlers.go. api.New(store, hasher, cfgAuth, sets, providers, log).
- Ingest auto-creates unknown streams (logs.go:streamCreator) per project setting + max_streams; createStream 409 at the limit.
- Archive credentials never in DB: storage_providers.secret_file names a file in ARCHIVE_SECRETS_DIR (storage_secrets.go writes them); only one active provider (unique partial index).
Gotchas:
- Integration archive tests fail on Docker Desktop macOS (bind mount shows 0-byte Parquet to CH); also on clean HEAD. Run them on Linux.
- OTLP is at /v1/logs not /api/v1 (exporters append /v1/logs).
- CH day partition holds all tenants → deletes use ALTER DELETE per partition, never DROP PARTITION (ADR 0009).
- Spool dir (SPOOL_DIR) must persist across restarts; spool id stabilises dedup tokens (ingest/writer.go:loadSpoolID).
- Registration limited (OPEN_REGISTRATION, register IP limiter 10/h); password hashing bounded by auth.Hasher queue (Argon2 concurrency), Burn() equalises timing.
- webui/dist is embedded; backend builds without frontend only with the .gitkeep placeholder.
- ADRs in docs/adr/0001-0018 explain WAL (0003), cache budget (0004), archive catalog (0008), OTLP/alerts/export (0010), secrets (0015,0017), settings (0018).
Related: map/web.md, map/sdk.md, map/integrations.md, map/deploy.md
