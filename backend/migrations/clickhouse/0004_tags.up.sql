ALTER TABLE events ADD COLUMN tags Array(LowCardinality(String)) DEFAULT [];
ALTER TABLE events ADD INDEX idx_tags tags TYPE bloom_filter(0.01) GRANULARITY 4;
