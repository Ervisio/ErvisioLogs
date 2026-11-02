# Internal observability

Expose Prometheus metrics (protected): ingest_received_total, ingest_durable_total, ingest_rejected_total by reason, ingest_to_query_latency, wal_bytes, wal_oldest_age, archive_upload_backlog, archive_upload_failures, archive_checksum_failures, cache_bytes, eviction_total, remote_read_latency, retention_pending_segments, query_duration histograms, query_partial_total, SSE_connections, db_health. Avoid high-cardinality labels (no player IDs or event IDs).

Structured logs to stdout, request IDs, trace IDs, redacted secrets. Admin health page shows provider health, last verified upload, pending archive count, oldest unarchived event, current storage tiers. Alerts for WAL >80%, provider outage, archive lag, catalog mismatch, failed retention and query degradation. System logging must function even if Ervisio Logs storage is unavailable.
