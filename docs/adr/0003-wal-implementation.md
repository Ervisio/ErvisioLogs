# ADR 0003: Durable ingestion WAL (D002)

Status: Proposed. No prototype has been run.

## Context
HTTP 202 is allowed only after the batch is durably recorded. At-least-once delivery, bounded disk, fail closed with 503
when full, replay after crash with stable ordering per stream.

## Options
1. Segmented append-only log written by us: length + CRC32C framed records, `fsync` (or group commit) before ack, segment
   rotation, checkpoint file for the consumer offset. Smallest dependency surface, most code to get right.
2. Embedded mature queue or store (candidates to evaluate: `tidwall/wal`, `nats-io/nats-server` embedded JetStream, BoltDB or Badger as a queue).
3. External broker (NATS JetStream) as the Scale profile only; the Compact profile keeps option 1 or 2.

## Questions the prototype must answer
- Group-commit `fsync` cost: ack latency P50/P99 at 1k, 10k, 50k events/s on the target disk class.
- Behaviour on disk full, torn last record after power loss (kill -9 and fault-injected truncation), replay ordering, duplicate window after a crash between ClickHouse insert and checkpoint.
- Disk budget enforcement independent of cache and database volumes (see ADR 0004).

## Test gate (from ACCEPTANCE_TESTS.md)
Crash at every transition, no accepted event lost, no unbounded growth.

## Working assumption
Option 1 behind a `wal.Log` interface so option 2 or 3 can replace it. Interface is not written yet.
