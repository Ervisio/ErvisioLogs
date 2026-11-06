# backend (Go) + API contract
Purpose: single Go binary `ervisio-logs` (module github.com/Ervisio/ErvisioLogs/backend) = HTTP API + ingest pipeline + archiver + alert evaluator + embedded web UI; contract in `api/openapi.yaml` (3.7k lines, single file, no separate api map).
Stores: Postgres (tenancy/auth/catalog/config) + ClickHouse (hot events) + remote Parquet archive (s3/sftp/local).
Entry points:
- backend/cmd/ervisio-logs/main.go:run (cmds: serve, migrate up|down|status, version, config-check, healthcheck, tls-cert)
- backend/internal/config/config.go:Load (all env `ERVISIO_LOGS_*`, `_FILE` suffix = read secret from file)
- backend/internal/server/server.go (/healthz /readyz /metrics, OTLP mount, webui fallback)
- backend/internal/api/api.go:Routes (all /api/v1 routes + `authenticate`), otlp.go:OTLPRoutes (POST /v1/logs, outside /api/v1)
Layout: api (handlers) | mail (SMTP Sender, Build) | store (pgx SQL, one file per domain) | ingest (Validator, Writer, Hub) | wal | archive | auth | notify | oauth | migrate | httpx (RFC7807 problems, request id) | webui (go:embed dist) | migrations/{postgres,clickhouse} (embed.go)
Flow ingest: POST /ingest|/ingest/batch|/v1/logs → api/logs.go:ingestToken (bearer token, scope logs:ingest) → queueEvents → ingest.Validator.Normalize → ingest.EncodeRecord → wal.Segmented.Append (fsync group commit) → 202 "durably_queued" → ingest.Writer.Run reads WAL → ClickHouse insert (dedup token from spool id+seq range; inflight file for crash resend) → wal.Commit; ingest.Hub.Publish feeds live tail.
Flow read: POST /logs/search (api/logs_search.go) = hot ClickHouse UNION archived Parquet via archive.ReadSources (dedup by event_id); GET /logs/tail (SSE, logs_tail.go, Hub ring + `after` cursor); POST /logs/export; saved /queries.
Flow archive: archive.Archiver.Run (interval; Postgres advisory lock → one pass per instance) → closed stream-days → Parquet (chexport.go) → provider (s3.go/sftp.go/localfs.go) → catalog `segments` (store/archive.go, states incl. REMOTE_EXPIRED/DELETING/DELETED/FAILED) → evict.go:maintain (global expiry, covered-hot eviction, cache budget watermarks) → orphans.go:ReconcileOrphans (>1 day old).
Flow alerts: api/alerts_eval.go:RunAlerts → evaluateDue → countRule (ClickHouse) → notify (webhook.go, channels with rotate-secret/test).
Auth: api.go:authenticate resolves Bearer API token (HMAC hash with SECRET_KEY) OR session cookie; never rejects itself, handlers call requireUser/requireAdmin/workspaceAccess(perm). Permissions in auth/perms.go (roles owner/admin/developer/analyst/viewer). OAuth google/discord/cognito (oauth/, oauth_handlers.go). First run: EnsureSetupCode logs a one-time code to create instance admin.
Invariants:
- Isolation: every ClickHouse query filters workspace_id (+project_id); API tokens are bound to one workspace+project (token.WorkspaceID/ProjectID); cookie users go through workspaceAccess.
- Non-member of a workspace gets 404 (not 403) so existence is hidden; missing perm = 403.
- Instance admin (is_instance_admin) grants NO tenant data access; /admin/* only.
- Always >=1 active instance admin (store.ensureOtherAdmin, advisory lock) and >=1 owner per workspace: UpdateUser/DeleteUser refuse (409). Admins can't change/delete/reset themselves via /admin/users (422). Users + own account/sessions handlers: api/users_handlers.go.
- Sessions: ip + last_seen_at written at most once a minute in SessionByHash; password change keeps the current session, admin reset/disable revoke all. Tokens of a deleted/disabled creator stop working (TokenByHash join).
- Token scopes limited to logs:ingest|read|export and never exceed creator perms.
- Cookie-authed unsafe requests need X-CSRF header == session CSRFToken AND Origin == PUBLIC_URL origin; Bearer requests skip both.
- Ack (202) only after WAL durable. WAL full → 503 + Retry-After 5; oversize → 413; archiver OverCapacity() → ingest 503 (uncovered hot rows never evicted).
- Event limits (attributes depth/keys, clip, future skew/past age) live in ingest/event.go + config (INGEST_MAX_FUTURE_SKEW/PAST_AGE).
- Retention policy scopes instance>workspace>project>stream, NULL inherits (retention_policies table); no CH TTL, retention manager alone deletes.
- Evict order: mark segment as read source FIRST, then delete hot rows (else search briefly misses rows).
- Migrations: NNNN_name.up/down.sql, checksum recorded; editing an applied migration errors; no down file = irreversible. PG 0001-0010 (instance, tenancy_auth, archive, integrations, settings, account_sessions, email, stream_metadata, admin_tokens, purge_jobs), CH 0001-0003 (events, dedup, inserted_at). Add new ones with next number, never edit.
- Webhooks refuse private/loopback/CGNAT/link-local dests unless alerts.webhook_allow_private (notify.NewClient takes a func, read per dial).
- Runtime settings (ADR 0018): internal/settings Service (snapshot, reload 30s, OnLoad sets slog LevelVar); handlers read a.Settings.Bool/Int/Duration/ProjectInt, never cfg, for keys in settings.defs. Env-set key = locked (cfg.FromEnv). Routes in api/settings_handlers.go. api.New(store, hasher, cfgAuth, sets, providers, log).
- Streams/environments: `name` is the immutable id used by ingest/search; display_name/description/color (store.Meta, '' = unset) change via PATCH (metaReq: absent = keep, '' = clear).
- Ingest auto-creates unknown streams (logs.go:streamCreator) per project setting + max_streams; createStream 409 at the limit.
- Email links (api/email_handlers.go, store/email.go): token only in POST body + URL fragment `origin/page#token=` (never in paths: middleware logs them); single use via DELETE...RETURNING in tx. Forgot-password always 202 + async send. Invitation url returned only when NOT emailed; emailed link verifies the address on accept; accept needs user email == invited email. SMTP config from settings email.*, password env or file in ARCHIVE_SECRETS_DIR.
- Operators (ADR 0019, api/operator.go): admin unix socket (ADMIN_SOCKET, server/unix.go, all admin:* scopes, name "plugin") or `evl_adm_` admin tokens (session-only management, 5 admin:* scopes, CORS * no credentials). Not users: /admin handlers use adminAccess(scope), tenancy uses caller/operatorAccess = owner with auth.OperatorPermissions (never logs:read/export/delete); /auth/me and /admin/tokens 401. Workspace created by operator needs owner_id. Audit actor_name.
- Ingest limits (ADR 0020, api/limits.go): rate per token/project/workspace + daily events/bytes per project/workspace, settings default 0 = unlimited; 429 + Retry-After; refused batch spends nothing; counters in memory per process. /admin/health (health_handlers.go). Purge: POST /projects/{id}/purges (logs:delete, typed confirm) -> purge_jobs, run by archiver under its lock (archive/purge.go); refuses (422) cuts through archived segments; WAL rows not purged; tombstone keeps archiver off that stream-day.
- Archive credentials never in DB: storage_providers.secret_file names a file in ARCHIVE_SECRETS_DIR (storage_secrets.go writes them); only one active provider (unique partial index).
Gotchas:
- Integration archive tests fail on Docker Desktop macOS (bind mount shows 0-byte Parquet to CH); also on clean HEAD. Run them on Linux.
- Search: total/level_counts are counted BEFORE the rows query (logs_search.go), so a concurrent insert can make total < items; level_counts honour the level filter (web works around it).
- OTLP is at /v1/logs not /api/v1 (exporters append /v1/logs).
- CH day partition holds all tenants → deletes use ALTER DELETE per partition, never DROP PARTITION (ADR 0009).
- Spool dir (SPOOL_DIR) must persist across restarts; spool id stabilises dedup tokens (ingest/writer.go:loadSpoolID).
- Registration limited (OPEN_REGISTRATION, register IP limiter 10/h); password hashing bounded by auth.Hasher queue (Argon2 concurrency), Burn() equalises timing.
- webui/dist is embedded; backend builds without frontend only with the .gitkeep placeholder.
- ADRs in docs/adr/0001-0020 explain WAL (0003), cache budget (0004), archive catalog (0008), OTLP/alerts/export (0010), secrets (0015,0017), settings (0018), operators (0019), limits/purge (0020).
Log v2 (F4): event tags max20 x40 lowercase in WAL/event schema2 + CH0004; tags[] AND filters search/export, comma tags + JSON AttrFilter[] on tail. Archive schema2 stores tags; schema1 segments project typed empty arrays.
Catalog PG0011: store/event_types.go + api/event_types.go; event-types CRUD, idempotent fivem preset preserving customized entries, field-rules GET/PUT. Ingest auto-discovers names. Rules redact attribute paths before WAL (including OTLP), fail closed if rules DB unavailable; hidden/format are presentation only.
Related: map/web.md, map/sdk.md, map/integrations.md, map/deploy.md
