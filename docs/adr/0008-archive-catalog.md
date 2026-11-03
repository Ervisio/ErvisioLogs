# ADR 0008: Archive catalog, publishing and tombstones (D003)

Status: Proposed. Implemented on `feat/p4-archive` (2026-11-03). The integration tests were written; the last ones
(orphan cleanup, admin storage UI) ran once before the owner asked to stop running tests, the rest of the suite was
not re-run after the final changes. Not approved.

## Context
STORAGE_AND_RETENTION.md requires immutable Parquet segments on one remote provider (S3-compatible or SFTP Storage
Box), a PostgreSQL catalog, the lifecycle STAGING -> UPLOADING -> VERIFIED -> CATALOGED -> HOT_EVICTABLE ->
REMOTE_EXPIRED -> DELETING -> DELETED, atomic catalog transactions, reconciliation of abandoned stages and orphan
files, and tombstones so an expired segment cannot come back after a retry.

The hard part is the hand-off between hot storage and the archive. Events reach ClickHouse late (WAL replay after an
outage, late timestamps accepted by ADR 0006), so "everything in day D" is never final, and the archiver cannot know
which hot rows a segment covers unless the rows carry a marker set at insert time. `ingested_at` is set by the API
when the request arrives, before the WAL, so a row replayed after an outage can carry an old `ingested_at` and land
after the archiver passed. It cannot be the marker.

## Decision
**Insert marker.** ClickHouse migration 0003 adds `inserted_at DateTime64(3) DEFAULT now64(3)`. The writer never sends
it, so the ClickHouse server stamps each block when it inserts it. The migration materializes the column for existing
rows once.

**Segment key and generations.** A segment holds the rows of one (workspace, project, stream, UTC day) whose
`inserted_at` falls in `[cutoff_from, cutoff_to)`. Generation 1 has `cutoff_from = 0`; generation N+1 starts at the
`cutoff_to` of generation N. The archiver reads `cutoff_to` from the ClickHouse clock (`now64(3)` minus a 2 minute
margin for inserts still in flight), not from the app clock. Late rows for an archived day produce a small extra
generation; they are never lost and never rewrite an immutable object. Compaction of generations is deferred (see
Risks).

A day becomes archivable once it ended more than `archive_grace` ago (default 1 hour).

**Coverage rule.** The hot rows of a key with `inserted_at < max(cutoff_to)` over its CATALOGED generations have a
verified remote copy. Those rows, and only those, may be deleted from hot storage.

**Object keys.** `v1/<workspace>/<project>/<stream>/<yyyy>/<mm>/<dd>/g<generation>-<segment id>.parquet`. Keys are
built from UUIDs and integers only, so neither path traversal nor provider-specific escaping can arise. The
segment id in the name makes a retried upload of the same generation write a new object instead of overwriting
one that might be cataloged.

**Publishing.**
1. STAGING: insert the catalog row (unique on key and generation), then export with `SELECT ... FORMAT Parquet` to
   a local staging file. Compute SHA-256 and row count while writing.
2. UPLOADING: `Put` to a temporary key (`tmp/<segment id>.parquet`).
3. VERIFIED: S3 checks size and the SHA-256 the server stored with the object. SFTP checks the size and re-reads the
   file to recompute SHA-256. Then `Promote` moves the object to its final key: rename on SFTP; on S3, server-side
   copy, a second check, and delete of the temporary object.
4. CATALOGED: one transaction sets the state, checksum, bytes, rows and `cutoff_to`. Only from here on does the
   coverage rule count the segment.

A crash at any step leaves a row in an earlier state. Reconciliation retries it with backoff, or, after
`max_attempts`, marks it failed and deletes the temporary object. A failed generation leaves its rows in hot storage
(they are not covered), and the next archiver pass covers them with a new segment.

**Tombstones.** Expired segments move to REMOTE_EXPIRED, DELETING, DELETED and the row stays. The archiver never
creates a segment for a key whose day is past global retention, so a retry cannot resurrect it. Orphan reconciliation
lists the provider prefix and deletes objects that have no catalog row in a live state and are older than one day.

**Reading.** The query planner reads the catalog, never a directory listing. It reads a segment remotely only if its
hot rows have been evicted (`hot_evicted_at` set). Otherwise hot storage still holds every row of the segment and is
preferred. Results from both tiers are merged and deduplicated by `event_id`.

As built (ADR 0002 picked ClickHouse): search downloads each evicted segment that overlaps the time range into the
local cache, checks size and SHA-256 against the catalog, and reads it with `file(path, 'Parquet', <schema>)`. The
schema is given explicitly (SchemaVersion 1) so ClickHouse never infers types per file; single quotes in it are
doubled. The path is built from the configured ClickHouse mount and the segment id only. Hot and cached segments are
joined with `UNION ALL`; `LIMIT 1 BY event_id` and `uniqExact(event_id)` count an event once when it exists in both.
A segment that cannot be downloaded or fails verification is left out and the response carries `partial: true`, with
`searched_tiers` including `archive`.

**Orphan cleanup.** Once per process per 24 hours the archiver lists every provider and, for objects older than
24 hours only: deletes `v1/...parquet` keys with no catalog row outside DELETED, deletes `tmp/<segment id>.parquet`
whose segment is no longer STAGING, UPLOADING or VERIFIED, and deletes interrupted SFTP `.part-` files. Anything
outside `v1/` and `tmp/`, or not matching the key format, is never touched, so a shared bucket keeps its other data.
A provider that fails is logged and skipped; the others still run.

**Administration.** `/api/v1/admin/storage` (usage, providers, active provider), provider create, update, delete,
health test and activation, and `/api/v1/admin/retention` (instance, workspace, project and stream policies). All of
them require the instance administrator. A provider holding non-deleted segments cannot be deleted (409). Activation
requires a passing health check. Stopping archiving (no active provider) keeps data local until global retention.
The web Storage page drives these routes.

**Secrets.** Provider credentials are not stored in PostgreSQL. The catalog row holds non-secret settings and the
name of a secret file (Docker secret or mounted file) that the app reads at use time. Encryption of archive files
(D008) stays open.

## Alternatives
- Cut segments by `ingested_at`: rejected, because WAL replay can insert old `ingested_at` values after the cut.
- Rewrite the day's segment when late rows arrive: rejected, objects must stay immutable once cataloged.
- Check coverage by comparing event ids between hot and Parquet: correct but needs the segment downloaded for every
  eviction.
- ClickHouse `INSERT INTO FUNCTION s3(...)`: no SFTP equivalent, and the checksum and verify steps would be split
  between two systems.

## Risks
- Small generations: a stream with steady late arrivals gets one tiny file per archiver pass. Mitigation for now:
  late rows of an archived day are archived at most once per `late_archive_interval` (default 6 hours). Compaction
  into a new generation that supersedes others needs its own tombstone rules and is not built yet.
- `MATERIALIZE COLUMN` on a large existing table is one mutation that rewrites the column in every part.
- Segments are one file per stream-day. A very busy stream-day can exceed the 256 MB target; splitting by size is not
  built yet.
- Cache race: the budget can delete a cached segment between the download and the ClickHouse query. That search
  fails with 503 and succeeds on retry, which downloads the file again. A read lease would close the gap.
- Orphan cleanup lists the whole provider once a day. On a large S3 bucket that is many LIST requests; a prefix per
  day and a resumable cursor are not built.
- The ClickHouse clock defines coverage. A clock jump backwards on the ClickHouse host could stamp new rows below a
  cutoff that is already cataloged, and those rows would be evicted without a remote copy. Mitigation: the archiver
  refuses to run if `now64()` is below the largest cutoff it has cataloged.

## Tests
Provider contract suite run against the local, S3 (SeaweedFS) and SFTP (OpenSSH) providers: put, verify, promote,
idempotent delete, truncated upload, checksum mismatch. Catalog: crash at every state, then reconciliation; duplicate
generation; tombstone blocks re-archive; orphan cleanup. End to end with a simulated clock: 3 day local / 14 day
global rollover, late event after archive, remote outage during upload with no data loss, search after hot eviction.
