# ADR 0006: Event time, late and future timestamps (D007)

Status: Proposed, implemented and tested (2026-11-03); awaiting owner approval.

## Context
Clients send their own `timestamp`. A game server with a wrong clock, or a buffered client that flushes a week of logs,
can send times far from now. ClickHouse partitions `events` by day of `timestamp`, so a stray year-2000 event creates a
partition that retention has to find and drop, and a far-future event stays at the top of every search.

## Options
1. Store any timestamp. Simple; pollutes partitions and the default "newest first" view.
2. Clamp bad timestamps to arrival time. Keeps the event, but silently rewrites what the client said.
3. Reject events outside a window, per event, and keep arrival time separately.

## Decision
Option 3.
- `timestamp` is optional; when absent the server uses arrival time. It is stored with millisecond precision.
- `ingested_at` is always the arrival time, so late arrivals stay visible as such.
- An event more than `ERVISIO_LOGS_INGEST_MAX_FUTURE_SKEW` (default 5m) ahead, or older than
  `ERVISIO_LOGS_INGEST_MAX_PAST_AGE` (default 168h), is rejected. In a batch, the other events are still accepted and
  the response lists the rejected index and reason; a batch where every event is invalid answers 422.
- The past limit must stay at or below the shortest hot retention once retention exists (P4), so a late event never
  lands in a partition that was already dropped.

## Risks
- A client with a clock 6 minutes fast loses events. The rejection reason names the limit, and the operator can raise it.
- Clamping (option 2) might suit some users better; it can be added later as a per-project option.

## Tests
- `TestNormalizeRejects` (future skew, past age) and `TestValidatorZeroLimits` (defaults when unset).
- `TestIngestSearchAndTail` (real ClickHouse): mixed batch with 2 accepted and 2 rejected at indices 2 and 3.
