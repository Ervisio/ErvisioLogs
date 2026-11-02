# Storage, cache, lifecycle and remote providers

## Example policy
local_max_age_days: 3; remote_global_retention_days: 14; cache_max_gb: 20; high_watermark_pct: 90; low_watermark_pct: 75. Days are from canonical event time with a bounded allowed skew policy. Local eviction at age 3d OR earlier if capacity exceeds high watermark; never before remote durability confirmation. Remote deletion at 14d plus documented cleanup lag. Cache GB does not cap WAL, PostgreSQL or ClickHouse overhead unless separately configured.

## Tier model
Hot: ClickHouse on local fast disk. Archive: immutable Parquet on one selected provider (S3-compatible OR Hetzner Storage Box over SFTP). Catalog: PostgreSQL. Remote reads: plan segments by tenant/project/stream/time, fetch/stream relevant files, query using tested engine; cache downloaded files with LRU/age policy. Prefer hot for overlap; dedupe event IDs. No mounting Storage Box as ClickHouse live data directory.

## Provider contract
Put(tempKey, stream, checksum); Promote(tempKey, finalKey) where supported; Get/GetRange (capability dependent); Stat; List; Delete; Health; Capabilities. S3: multipart upload, object checksums, conditional operations when available. SFTP: host key pinning, key auth, upload temporary file then atomic rename within same filesystem, post-upload verification; do not assume random range reads or server-side metadata guarantees. Idempotent uploads and deletions. Ensure no path traversal and provider-specific key encoding.

## Archive segment lifecycle
STAGING -> UPLOADING -> VERIFIED -> CATALOGED -> HOT_EVICTABLE -> REMOTE_EXPIRED -> DELETING -> DELETED. Atomic catalog transaction; reconciliation for abandoned stages and orphan remote files; retry/backoff; object immutable once cataloged. Batch/compaction targets configurable (benchmark 32–256 MB compressed); avoid tiny files. Tombstones prevent expired segments from reappearing after retries.

## Retention precedence
Instance caps -> workspace defaults -> project overrides -> stream overrides, validated by permissions and maximum allowed global retention. Global >= local. Explicit behavior for no remote provider: local-only retention, no promise of remote durability. Support manual purge only with elevated scope, confirmation and audit. Remote delete asynchronous and report pending status. Optional S3 lifecycle rules as secondary safeguard, not sole source of truth.

## Capacity pressure
Evict only VERIFIED/CATALOGED remote-backed hot segments oldest-first, until low watermark. If still above cap, reject ingestion with 503 or 429 according to capacity/quota reason; never delete sole copy. Separate disk quotas for spool and query temp. Alerts for cache pressure, archive lag, remote error, spool full, retention backlog.

## Validation tests
3d/14d rollover; 20GB 90/75 eviction; outage during upload; truncated SFTP transfer; orphan cleanup; remote read after hot eviction; late events; checksum mismatch; duplicate segments; archive expiration; legal deletion requests; clock skew; backup/restore of catalog.
