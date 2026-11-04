# ADR 0018: Runtime settings at instance, workspace and project level

Status: Proposed. Written for 0.2. Checked with `go test`, `go vet` and the API integration tests.

## Context
Every tunable lived in `ERVISIO_LOGS_*` variables. Changing one meant editing `.env` and restarting, and Compose
passed only some of them to the container, so edits were silently ignored. Owners wanted to change these values from
the web app and the Ervisio plugin, and to set some of them per workspace or per project. Ingest also rejected events
for streams nobody had created, which broke the FiveM resource's default streams.

## Decision
A `settings` table in PostgreSQL (migration 0005) holds overrides as JSON, keyed by setting and by an optional
workspace and project. The `settings` package reads it into an in-memory snapshot and reloads it every 30 seconds, so
every replica picks up a change within that time; the replica that made the change reloads at once.

- Precedence: environment variable > project > workspace > instance > built-in default. A key set in the environment
  is locked: the API reports it with `locked: true` and the variable's name, and refuses changes with 409.
- Only two keys exist below the instance: `ingest.auto_create_streams` (default on) and `ingest.max_streams`
  (default 20, 0 means no limit). Everything else is instance-wide: registration, session lifetime, log level,
  ingest time limits, archive and alert timing, private webhooks, export row cap.
- Durations travel and are stored as whole seconds. Each key has bounds; values outside them answer 422.
- Routes: `GET`, `PUT /{key}` and `DELETE /{key}` under `/admin/settings` (instance administrators),
  `/workspaces/{id}/settings` (`workspace:manage` to write) and `/projects/{id}/settings` (`projects:write` to write).
  Every change is audited as `settings.update` or `settings.reset`.
- Ingest creates an unknown stream when the project allows it and is under its limit, and audits it as
  `stream.auto_create`. The limit also applies to `POST /projects/{id}/streams` (409).
- Infrastructure values (addresses, credentials, paths, body size, shutdown timeout) stay environment-only and need a
  restart. Compose now passes every variable, with the overridable ones empty unless `.env` sets them.
- `ERVISIO_LOGS_ARCHIVE_INTERVAL=0` and `ERVISIO_LOGS_ALERTS_INTERVAL=0` still keep the archiver and the evaluator off
  in that process; the setting changes only the interval of a loop that runs.

## Risks
- Two concurrent ingests can each create a stream when the project has one slot left. The limit is a guard against
  runaway names, not a quota.
- A replica serves old values for up to 30 seconds after another replica's change.
- Turning `alerts.webhook_allow_private` off does not close an idle pooled connection to a private address; it can
  carry one more delivery within the transport's 60-second idle timeout.
- A row whose key was removed, or whose value no longer passes the bounds, is skipped at load and shows the default.
- An install that kept `ERVISIO_LOGS_LOG_LEVEL=info` uncommented in `.env` has that setting locked until the line is
  removed.
