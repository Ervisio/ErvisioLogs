# F6–F9 backend contracts

The complete machine contract is `api/openapi.yaml`. Migration PostgreSQL 0012 adds account security, dashboard panels, channel deliveries and system subscriptions. Apply migrations before deploying the new binary.

## Account security

TOTP and passkeys are optional. Neither creates an instance-wide enrollment requirement.

- `GET /api/v1/auth/security` returns `{totp_enabled,passkeys:[{id,name,created_at,last_used_at}]}`.
- `POST /auth/totp/setup {password}` returns `{secret,uri}` with `Cache-Control: no-store`. A pending enrollment expires after ten minutes. Secret values use AES-GCM at rest with account-bound associated data.
- `POST /auth/totp/confirm {code}` activates the pending secret, revokes other sessions, and returns `{totp_enabled:true,recovery_codes:string[10]}` once, in one transaction. Store the recovery codes safely; only hashes remain in the database.
- When enrolled, `POST /auth/login {login,password}` returns `202 {mfa_required:true,challenge}` without a session. `POST /auth/mfa/verify {challenge,code}` accepts a current TOTP or unused recovery code and returns the usual session. Challenges expire after five minutes and five attempts. TOTP steps and recovery codes cannot be replayed.
- OAuth for an enrolled account redirects to `/login#mfa=<challenge>`. This fragment never reaches request logs. The browser submits it to the same verification endpoint.
- `DELETE /auth/totp {password,code}` removes TOTP and recovery codes. The code must be fresh or an unused recovery code.
- `POST /auth/passkeys/register/start {password,code?,name}` returns `{challenge,options:{publicKey:...}}`. Complete browser `navigator.credentials.create` and submit `POST /auth/passkeys/register/finish {challenge,credential}`. `credential` uses standard PublicKeyCredential JSON, including base64url binary fields.
- `POST /auth/passkeys/login/start {login}` returns `{challenge,options:{publicKey:...}}`. Complete `navigator.credentials.get` and submit `/auth/passkeys/login/finish {challenge,credential}`. Verified passkeys require authenticator user verification and sign in without an additional TOTP step. Challenge cookie, origin, RP ID, signature and counter checks reject substitution and replay.
- `DELETE /auth/passkeys/{id} {password,code?}` removes a credential owned by the current account. The account may retain zero passkeys. At most twenty passkeys per account.

Local-password accounts prove their password for enrollment/removal. Accounts using only external identities may send an empty password within five minutes of a fresh OAuth sign-in; older sessions receive 409 and must sign in again. TOTP or recovery proof remains required for passkey changes when TOTP is active. Existing password reset never removes TOTP. Do not lose both your authenticator and all recovery codes.

Passkeys require HTTPS, or localhost for development. The RP ID and allowed origin come from `PUBLIC_URL`. Their protocol implementation uses [go-webauthn v0.18.2](https://pkg.go.dev/github.com/go-webauthn/webauthn/webauthn), avoiding handwritten attestation, COSE and assertion validation. This is the reason for the new dependency and its transitive cryptographic formats.

## API tokens

Expiry already applies on every bearer request. `POST /tokens/{id}/rotate` revokes an active unexpired token and returns `201 {token,api_token}` with a replacement in one database transaction. The replacement keeps name, project, scopes and expiry. Callers need `tokens:manage` and every original scope. The old token stops working immediately; the plaintext replacement appears once.

## Channels and system alerts

`POST /notification-channels` accepts the existing `workspace_id`, `name` and optional `format` plus:

- Webhook: `{type:"webhook",url}` with existing signature, SSRF filtering and Slack/Discord formatting.
- Email: `{type:"email",recipient}` using the configured instance SMTP sender.
- Telegram: `{type:"telegram",bot_token,chat_id}`. Chat ID is numeric or a public `@channel` name. The bot token is encrypted at rest; outbound requests always use `https://api.telegram.org`. API response bodies and network URLs never enter delivered errors or history.

`PATCH /notification-channels/{id}` accepts `name`, `enabled`, existing webhook `format`/`url`, or `recipient` for email, or the `bot_token`/`chat_id` pair for Telegram. Type cannot change. Responses expose `url_hint`, never credentials. Only signed Ervisio webhooks have a rotating signing secret.

`GET /notification-channels/{id}/deliveries?limit=50` requires `alerts:manage` and returns `{items:[{id,status:"ok"|"error",error,created_at}]}`, including tests and system alerts. History follows the alert-history retention setting.

`GET/PUT /workspaces/{id}/system-alerts` reads/replaces `{channel_ids:[uuid]}`. Configuration requires both an instance administrator and `alerts:manage` in that workspace; all chosen channels must belong to it. Persistent transitions send firing/recovery notifications for WAL use over 80%, an unhealthy/missing active archive destination, and oldest unarchived events beyond 48 hours (or twice the archive interval, whichever is longer). Instance administrators receive no tenant-log access from this feature.

## Dashboard and usage

`GET /dashboard-panels?project_id=...` lists persisted shared panels. `POST /dashboard-panels` and `PUT /dashboard-panels/{id}` accept `{project_id,saved_query_id,name,type,field?,interval_seconds?,limit?}`. Type is `count`, `timeseries` or `top`; field is `level`, `stream`, `event`, `service`, `environment` or a validated `attributes.<path>`. Interval is 60–86400 seconds, default 3600. Top limit is 1–100, default 10. The saved query must belong to the same project. Author or `projects:write` can edit/delete, and readers need `logs:read`.

`GET /dashboard-panels/{id}/data` returns `{items:[{label,value}],partial,searched_tiers}` and uses the complete saved-query range across hot/archive tiers with event deduplication. Missing archive sources are explicit.

`GET /usage?project_id=...&range=24h&interval_seconds=3600` returns `{items:[{timestamp,stream,events,bytes}],events,bytes,byte_measurement,quota,quota_used,partial,searched_tiers}`. Bytes measure persisted uncompressed text, JSON attributes and tags, not compressed disk use or network traffic. Maximum range is 31 days; overly fine grouping above 10,000 buckets requires a coarser interval. `quota_used.scope` is `current_process`, matching the existing process-local daily quota enforcement. Workspace and instance audit routes remain `/workspaces/{id}/audit` and `/admin/audit` with cursor pagination.

## Verification

`go test -race ./...`, `go vet ./...`, integration-test compilation and Redocly contract lint pass. Protocol tests generate a real P-256 authenticator attestation/assertion locally and verify valid signatures, wrong origin and wrong challenge. Database integration tests cover enrollment, recovery-code replay, expiry/attempt caps, token rotation, passkey ceremonies, dashboard isolation and channel history.

Run `python3 scripts/collector-test.py` to verify a real [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/configuration/) using an isolated temporary Compose project: JSON receiver → batch → gzip/protobuf exporter → WAL → project-scoped search. The pinned image is `otel/opentelemetry-collector-contrib:0.139.0`; override host ports with `COLLECTOR_TEST_APP_PORT` and `COLLECTOR_TEST_OTLP_PORT`. The fixture cleans up its own volumes and secret files. No production databases or accounts are used.

At implementation time Docker was unresponsive. The database integrations and the Collector run were compiled/prepared but not executed; do not count their presence as a passing real-store test.

The security dependency scan also found two reachable preexisting compression issues. Fixed versions are verified against the Go vulnerability database: [GO-2026-6888](https://vuln.go.dev/ID/GO-2026-6888.json), `github.com/andybalholm/brotli` 1.2.7; [GO-2026-6865](https://vuln.go.dev/ID/GO-2026-6865.json), `github.com/klauspost/compress` 1.20.2. F9 upgrades those existing transitive dependencies and reruns the backend checks and vulnerability scan.

After those upgrades, `go test -race ./...`, `go vet ./...` and `govulncheck ./...` pass. The scan reports zero reachable vulnerabilities and zero vulnerable imported packages; three findings remain in required modules whose affected code paths are not called. The browser regression suite covers the existing shell/explorer flows plus MFA, OAuth recovery, TOTP enrollment, passkey enrollment/sign-in/removal/cancellation, tenant-route isolation and the usage response contract.
