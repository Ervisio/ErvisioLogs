# ADR 0007: Hot search and live tail (P2)

Status: Proposed, implemented and tested (2026-11-03); awaiting owner approval.

## Context
P2 needs search over ClickHouse with pagination, attribute filters and a live tail over SSE. The API must never build
SQL from user text, must keep tenants apart, and must page stably while new events arrive.

## Decisions
- **Search request** is JSON, not a query language: fixed fields (levels, severity, streams, environments, events,
  services, trace id, message substring) plus up to a fixed number of attribute filters with `eq`, `neq`, `contains`,
  `exists`, `gt`, `gte`, `lt`, `lte`. A text query language can compile to the same structure later.
- **Attribute paths** use dots (`player.license`, `attributes.` prefix optional). Each segment must match the alphabet
  ingest allows for keys, then is quoted with backticks, which that alphabet cannot contain. Values always go as bound
  parameters. `exists` checks `JSONAllPaths`, because a path to an object reads as NULL in the ClickHouse JSON type.
- **Tenancy**: every query starts with `workspace_id = ? AND project_id = ?` from the authorised project, never from
  the request alone. Stream and environment names resolve to IDs through a per-project catalog.
- **Range**: default the last 24 hours, at most 31 days per query.
- **Pagination** is keyset on `(timestamp DESC, event_id DESC)`. The cursor is opaque base64 JSON. `LIMIT 1 BY event_id`
  hides duplicates that slipped past insert deduplication.
- **Totals** (`include_total`) count `uniqExact(event_id)` per level; they cost a second query, so they are opt-in.
- **Live tail** is SSE from an in-process hub. The writer publishes after a successful insert, so the tail shows only
  stored events. The hub keeps the last 10,000 events; `Last-Event-ID` resumes from it, and a cursor older than the ring
  (or from another process) gets `event: reset` so the client reloads with a search. A paused browser tail opens a new
  `EventSource`, which cannot set the header, so the same cursor is also accepted as the `last_event_id` query
  parameter; the header wins when both are present.

## Limits
- **Single instance only.** With several API instances, a tail client sees only what its own instance wrote. A shared
  bus (NATS, optional in P7) is needed before running more than one instance.
- A slow tail subscriber is dropped when its 512-event buffer fills, to protect the writer.
- No benchmark of search latency on a large table has been run. ORDER BY and partitioning are still provisional (D004).

## Tests
- Unit: `TestAttrExpr`, `TestBuildSearch`, `TestSearchCursorRoundTrip`, hub tests (project isolation, backlog, reset, slow drop).
- Integration, real ClickHouse 25.8: `TestIngestSearchAndTail` (auth, filters, pagination, bad cursor, injection
  attempt in a field name, range limit, tail with level filter) and `TestTailResumeAndAuth` (resume and reset).
- Smoke against the Compose stack: ingest then attribute search through the container.
