-- The ingest writer retries an insert with the same insert_deduplication_token after a crash or
-- timeout. A non-replicated MergeTree deduplicates only when this window is above zero; it keeps
-- the hashes of the last N inserted blocks. 10000 covers hours of writer output at the default
-- group size, far longer than a writer retry spans.
ALTER TABLE events MODIFY SETTING non_replicated_deduplication_window = 10000;
