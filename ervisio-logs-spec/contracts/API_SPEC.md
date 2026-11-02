# HTTP API contract v1 (design contract; generate actual OpenAPI before coding)

Base `/api/v1`; JSON UTF-8; versioned; RFC7807-style errors; request_id; ISO8601 UTC; cursor pagination; stable IDs; explicit permissions. All UI actions have API equivalents. OpenAPI 3.1 is authoritative once generated.

## Routes
POST /auth/login; POST /auth/logout; GET /auth/me; OIDC start/callback; CRUD /workspaces; /workspaces/{id}/members; /projects; /projects/{id}; /projects/{id}/environments; /projects/{id}/streams; /tokens (create/list/revoke); /storage/providers (create/test/update/remove); /retention/policies; POST /ingest; POST /ingest/batch; POST /logs/search; GET /logs/{id}; GET /logs/tail (SSE); /queries; /dashboards; /alerts; /notification-channels; /usage; /audit; /archives; /admin/health.

## Ingest sample
POST /api/v1/ingest/batch Authorization: Bearer <ingest-token>
`{"stream":"inventory","events":[{"id":"evt_123","timestamp":"2026-11-02T14:30:00Z","level":"INFO","event":"inventory.item_added","message":"Item received","attributes":{"player_id":42,"item":"bread"}}]}`
Response 202 `{"request_id":"req_123","accepted":1,"rejected":0,"status":"durably_queued"}`. Project is derived from token; client cannot spoof tenant. For partial batches define per-item failures and retry semantics in OpenAPI.

## Search sample
POST /api/v1/logs/search `{"project_id":"prj_123","streams":["inventory"],"time_range":{"from":"2026-11-01T00:00:00Z","to":"2026-11-02T00:00:00Z"},"filters":{"severity":["WARN","ERROR"],"attributes.player_id":{"eq":42}},"limit":100,"cursor":null}`. Backend constructs authorized AST, never interpolates user SQL. Return results, next_cursor, execution_stats, searched_tiers, partial flag. Explicit error when archive unavailable; never silently omit archived data.

## SSE
GET /api/v1/logs/tail?project_id=...; heartbeat, event IDs, bounded replay, reconnect. Browser EventSource cannot set Authorization header: use secure HttpOnly same-site cookie for browser or short-lived one-time stream ticket; do not put long-lived bearer tokens in query strings.

## Status codes
400 malformed; 401 unauthenticated; 403 unauthorized; 404 not found (no resource existence leak); 409 conflict; 413 oversized; 422 validation; 429 quota; 503 backpressure/remote failure; 202 durable accepted. Rate-limit headers and Retry-After.

## OTLP
Provide standard OTLP HTTP logs endpoint as separate adapter, with supported signal versions and conformance tests; do not claim OTLP support until validated. SDKs: TypeScript and FiveM Lua first; Python/Go later.
