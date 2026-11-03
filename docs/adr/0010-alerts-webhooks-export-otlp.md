# ADR 0010: Saved queries, alerts, webhooks, export and OTLP receiver (P5)

Status: Proposed. Implemented in P5. The unit tests (`internal/api/integrations_test.go`, `internal/notify/webhook_test.go`)
and the integration tests (`internal/api/integrations_integration_test.go`) are written and pass `go vet`; none of
them has been run, on the owner's instruction not to run tests or CI. The OTLP receiver has not been tried with a real
OpenTelemetry exporter or Collector.

## Context
P5 adds the features that move logs out of the search page: saved views, threshold alerts with notifications, bulk
export and an OpenTelemetry entry point. Each one opens a new path for data to leave the instance (webhooks, export)
or enter it (OTLP), so the decisions below are mostly about limits and about what the server refuses to do.

## Decisions

### Saved queries
- A saved query stores the filter fields of a search request (`SavedFilter`) plus an optional relative `range`
  (`15m` to `31d`). Absolute times, `limit`, `cursor` and `include_total` are refused, so a view always means
  "the same filter, now".
- The filter is validated against the project when saved: unknown streams and environments give 422.
- Any project member reads; the creator or a member with `projects:write` changes or deletes.

### Alert rules
- One rule type: count of events matching the filter over the last `window_seconds` (60 to 86400), compared with
  `threshold` (fires when `count > threshold`), evaluated every `interval_seconds` (30 to 86400).
- States are `ok`, `firing` and `error`. Only transitions notify: ok to firing (`alert.firing`), firing to ok
  (`alert.resolved`), any state to error (`alert.error`). Error to ok is silent. Each transition writes one
  `alert_events` row with the outcome of every delivery.
- **Only the hot tier is counted.** Windows are at most 24 h and the hot tier covers far more, so archived data is
  not needed; reading Parquet on every evaluation would cost more than it adds.
- The evaluator (`RunAlerts`) runs every `ERVISIO_LOGS_ALERTS_INTERVAL` (default 30s, 0 disables it). It takes a Postgres
  advisory lock, so with several replicas one does the work and the others skip that tick. It picks at most 100 due
  rules per tick, runs 4 at a time, and each ClickHouse count runs read-only with `max_execution_time` 30 s.
- History older than `ERVISIO_LOGS_ALERTS_HISTORY_RETENTION` (default 720h) is pruned once an hour.

### Notification channels (webhooks)
- One channel type, `webhook`, with three body formats: `ervisio` (signed JSON payload), `slack` and `discord` (a text
  message their incoming webhooks accept).
- Signing (ervisio format): `X-Ervisio-Signature: v1=<hex HMAC-SHA256(key, timestamp + "." + body)>` with
  `X-Ervisio-Timestamp` and a stable `X-Ervisio-Event-Id` for de-duplication. The key is derived from the server
  secret, the channel id and its secret version, so nothing secret is stored per channel; rotation bumps the version
  and the old key stops working at once. The secret (`whsec_...`) is shown only in the create and rotate answers.
- SSRF protection: the URL must be https and must not resolve to loopback, private, link-local, CGNAT, multicast or
  unspecified addresses. The check runs on the address the client actually dials, so a DNS answer that changes after
  validation cannot reach a private host. Redirects are not followed. `ERVISIO_LOGS_WEBHOOK_ALLOW_PRIVATE=true` lifts the
  filter and allows http, for LAN receivers; it is off by default.
- Delivery retries network errors, 429 and 5xx up to 3 attempts with backoff; a send has 45 s in total. The last
  outcome is kept on the channel (`last_status`, `last_error`). Error text never carries the URL.
- The API never returns the URL, only `url_hint` (scheme and host), because Slack and Discord URLs carry their secret
  in the path. Members read channels; `alerts:manage` changes them.

### Export
- `POST /api/v1/logs/export` streams every match, newest first, as NDJSON or CSV, up to `ERVISIO_LOGS_EXPORT_MAX_ROWS`
  (default 1 000 000). It reuses the search path (hot and archive, ADR 0002/0008).
- HTTP status is sent before the data, so the end state goes in trailers: `X-Export-Truncated: true` when the cap
  cut the export, `X-Export-Error` when the stream failed midway.
- CSV cells that start with `=`, `+`, `-`, `@`, tab or CR get a leading quote (formula injection).
- Needs `logs:export` (owner, admin, developer, analyst, or a token with that scope). Every export is audited as
  `logs.export` with the filter, not the data.

### OTLP/HTTP receiver
- `POST /v1/logs` (outside `/api/v1`, because exporters append `/v1/logs` to the endpoint) accepts
  `application/x-protobuf` and `application/json`, optionally gzip, at most 1000 log records, with a `logs:ingest`
  token. Records go through the normal ingest path (validation, WAL, ADR 0003/0006).
- Mapping: `service.name` to service, `host.name` to host, `severityNumber` (or `severityText`) to level, body to
  message, `traceId`/`spanId` as hex (all-zero means none), `event.name` to event. Stream and environment come from
  `ervisio.stream`/`ervisio.environment` attributes or the `X-Ervisio-Stream` header. Other resource, scope and record
  attributes are nested under `attributes` (`resource.*`, `scope.*`), with keys the index cannot hold rewritten.
- Partial acceptance follows the spec: 200 with `partialSuccess.rejectedLogRecords`.
- Known gaps: error bodies are problem JSON, not `google.rpc.Status`; gRPC is not offered.
- **Not validated.** The documentation and the UI must not claim OTLP support until a real exporter and the
  Collector `otlphttp` exporter have been tried against it.

## Risks
- Webhook URLs are stored in clear in Postgres. Whoever reads the database reads the Slack/Discord secrets. Encrypting
  them with a key derived from the server secret is a follow-up.
- An alert that counts only the hot tier misses events written late into archived days. Windows of at most 24 h make
  this rare.
- One evaluator at a time limits throughput to roughly 100 rules per tick; enough for the expected scale, not
  measured.
- Export of a large range holds a search open for its whole duration; the row cap is the only limit.

## Tests (written, not run)
- Unit: state transitions, CSV escaping, relative ranges, OTLP severity mapping, attribute nesting, event mapping,
  protobuf and JSON decoding, OTLP responses, URL validation, signing and verification.
- Integration: saved query CRUD and RBAC; channel create, redaction, rotation, signed test delivery verified with the
  shown secret, failure recorded; rule validation, firing once, no repeat while firing, resolution; export NDJSON and
  CSV with cap, trailer, formula escaping and audit; OTLP JSON ingest with partial rejection and mapping.
