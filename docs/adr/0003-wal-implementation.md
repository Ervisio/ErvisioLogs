# ADR 0003: Durable ingestion WAL (D002)

Status: Proposed, implemented and tested (2026-11-03); awaiting owner approval. Option 1 is implemented in `backend/internal/wal`.

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

## Decision
Option 1 behind the `wal.Log` interface, so option 2 or 3 can replace it without touching the ingest handlers.

- One record per accepted HTTP batch. Frame: `length u32 | crc32c(seq, payload) u32 | seq u64 | payload`.
- A single writer goroutine takes every append waiting at that moment, writes them and calls `fsync` once (group commit).
  `Append` returns only after that `fsync`, and the handler answers 202 only after `Append` returns.
- Segments rotate at `SegmentBytes`. The checkpoint file (`seq u64 | crc32c`) is written to a temporary file, synced and
  renamed. Segments wholly below the checkpoint are deleted.
- The disk budget (`MaxBytes`) covers every segment. A record that does not fit gets `ErrFull` and the handler answers 503
  with `Retry-After`. A failed write (for example `ENOSPC`) is cut back to the last record boundary; if that cut fails, the
  log refuses all further appends.
- At open, a damaged tail of the last segment is truncated, because only an unacknowledged write can leave one. Damage in
  a sealed segment, a gap in sequence numbers or a damaged checkpoint stops start-up with `ErrCorrupt`: the log never
  drops acknowledged records silently.
- Sequence numbers never repeat, even after every segment has been consumed and deleted.

## Prototype results (2026-11-03)
Hardware: AMD Ryzen 7 5700X3D, Windows 11, NTFS on the D: drive, Go 1.27.3. Not the target Linux disk class.

| Check | Result |
|---|---|
| Append, read, commit, reopen; rotation; consumed segments deleted | pass |
| Torn tail: partial header, partial payload, bad checksum, zeroed block | truncated, acknowledged records intact, numbering continues |
| Damaged sealed segment or checkpoint | open fails with `ErrCorrupt` |
| Disk budget | `ErrFull` at the budget, appends resume after commit |
| `kill` of a child process during 8 concurrent appenders, 3 rounds on the same directory | every acknowledged record present, in order |
| Ack latency, 1 KiB records, 1 writer | P50 2.1 ms, P95 2.5 ms, P99 2.9 ms, one fsync per append |
| Ack latency, 1 KiB records, 32 writers | P50 4.1 ms, P95 4.4 ms, P99 6.3 ms, 16 appends per fsync, about 8,100 appends/s |

One append carries a whole HTTP batch, so events per second scale with batch size. The 1k, 10k and 50k events/s runs
from the question list were not run as such: they need the ingest path and the target hardware (P7).

Not tested: real `ENOSPC` (simulated by the budget only), power loss as opposed to process kill (the OS page cache
survives a process kill), and the race detector on Windows (no cgo toolchain here; CI runs `go test -race` on Linux).

## Duplicate window
A crash after the ClickHouse insert and before the checkpoint replays those records. The writer groups consecutive WAL
records into one insert. Before inserting, it writes an in-flight file (`first`, `last` sequence, CRC) so a restart
replays exactly the same group, and it sends `insert_deduplication_token = <spool id>-<first>-<last>`. Migration
`0002_events_dedup` gives the table a non-replicated deduplication window of 10,000 blocks, so ClickHouse drops the
repeated block. Event IDs are fixed before the WAL write, and search deduplicates by `event_id` as a second guard.

The spool id is 16 random bytes written once to the spool directory. Without it, a new spool directory (or a second
instance) would start again at sequence 1, produce tokens already in the window, and ClickHouse would discard new
events as copies. The first integration run hit exactly this and stored nothing; `TestSpoolIDIsStableAndUnique` guards it.

## Ingest path results (2026-11-03)
| Check | Result |
|---|---|
| `TestWriterResendAfterCrashIsDeduplicated` (real ClickHouse 25.8): insert, no commit, restart | 12 rows, not 24 |
| Smoke, real Compose stack: ClickHouse stopped, batch answered 202, app killed with SIGKILL, both restarted | event stored exactly once |
| Smoke: clean restart of the app after ingest | event stored exactly once |

No throughput benchmark of the full ingest path has been run. Capacity numbers belong to P7.
