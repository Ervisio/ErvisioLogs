# Deployment and operations

Compact Docker Compose: Go app serving frontend, PostgreSQL, ClickHouse; dedicated persistent volumes for PG, CH, spool, cache. Optional SFTP/S3 archive provider configured via admin UI. Reverse proxy TLS. No direct public DB exposure. Health endpoints, resource limits, graceful shutdown, schema migration preflight.

Standard/Scale: split API and worker; optional JetStream; configurable remote object provider. Target Linux amd64/arm64; macOS/Windows Docker for development. Versions pinned in image digests/lockfiles after compatibility verification.

Secrets: `.env.example` placeholders only; admin bootstrap via one-time setup; no default passwords. SFTP host key pinning and SSH key; S3 least privilege. Config changes to provider require test connection, compatibility check, migration plan and safe rollback. Do not delete prior archive until verified migration complete.

Backup: PostgreSQL/catalog, ClickHouse hot data as needed, configuration and encryption keys; archive objects protected separately; recovery drills. Document restore ordering (catalog and objects), consistency checks and missing-object reporting. Disk budgets for cache, WAL, DB and temp independent. Monitoring and alerting mandatory for spool pressure.
