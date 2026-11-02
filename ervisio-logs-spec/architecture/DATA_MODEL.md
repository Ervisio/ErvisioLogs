# Data model and migrations

## PostgreSQL entities
users(id,email,password_hash,status,created_at); identities(user_id,provider,subject); sessions(id,user_id,expires_at,revoked_at); workspaces(id,slug,name); workspace_members(workspace_id,user_id,role_id); roles(id,workspace_id,name); role_permissions(role_id,permission); projects(id,workspace_id,slug,name); environments(id,project_id,name); streams(id,project_id,name,type); api_tokens(id,workspace_id,project_id,subject_id,hash,scopes,expires_at,last_used_at,revoked_at); storage_providers(id,workspace_id,type,config_ref,status); retention_policies(id,scope_type,scope_id,local_age,global_age,cache_budget,high_pct,low_pct); archive_segments(id,workspace_id,project_id,stream_id,provider_id,object_key,min_event_time,max_event_time,checksum,bytes,row_count,state,schema_version); saved_queries; dashboards; alert_rules; notification_channels; audit_events; jobs.

Use UUID/ULID consistently; FK and uniqueness scoped by workspace; encrypted credentials stored via secret manager/key encryption, never plaintext provider secrets. Composite indexes on tenant, project and time. Migrations reversible where feasible; zero-downtime expansion/contraction for production upgrades.

## ClickHouse canonical event columns
workspace_id, project_id, environment_id, stream_id, event_id, timestamp DateTime64(3,UTC), ingested_at, severity_number, severity_text, event_name, message, source_service, source_host, source_resource, trace_id, span_id, attributes JSON, schema_version. Use MergeTree family with carefully benchmarked ORDER BY; no one-table-per-stream. Avoid unbounded partitions per tenant. Optimize tenant/project/time/stream/severity. Test cardinality and TTL strategy under mixed retention policies.

## Event envelope
Require idempotency/event ID or generate one; define event-time skew bounds; enforce max payload, nesting depth, attribute count and key length. Store original timestamp and normalized UTC. Correlate FiveM player identifiers as attributes; avoid storing unnecessary personal identifiers by default.
