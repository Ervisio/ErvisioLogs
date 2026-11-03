-- ADR 0008: the archiver needs a marker that the ClickHouse server sets when it inserts a row.
-- ingested_at is set by the API before the WAL, so a row replayed after an outage can carry an old
-- value and land after the archiver passed. The writer never sends inserted_at.
ALTER TABLE events ADD COLUMN IF NOT EXISTS inserted_at DateTime64(3, 'UTC') DEFAULT now64(3) AFTER ingested_at;
-- Without this, rows written before the migration would compute the default at read time and
-- always look newly inserted, so the archiver would never cover them.
ALTER TABLE events MATERIALIZE COLUMN inserted_at SETTINGS mutations_sync = 2;
