-- PROVISIONAL canonical hot-event table (DATA_MODEL.md). Partitioning and ORDER BY are
-- placeholders pending the D004 benchmark; one table for all tenants/streams, partitioned by
-- day (never per tenant) to avoid unbounded partition counts. No TTL: the retention manager
-- (P4) owns deletion because retention is tenant-specific and must not outrun archive durability.
CREATE TABLE IF NOT EXISTS events
(
    workspace_id    UUID,
    project_id      UUID,
    environment_id  UUID,
    stream_id       UUID,
    event_id        String,
    timestamp       DateTime64(3, 'UTC'),
    ingested_at     DateTime64(3, 'UTC') DEFAULT now64(3),
    severity_number UInt8,
    severity_text   LowCardinality(String),
    event_name      LowCardinality(String),
    message         String CODEC(ZSTD(3)),
    source_service  LowCardinality(String),
    source_host     LowCardinality(String),
    source_resource String,
    trace_id        String,
    span_id         String,
    attributes      JSON,
    schema_version  UInt16 DEFAULT 1
)
ENGINE = MergeTree
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (workspace_id, project_id, stream_id, timestamp, event_id);
