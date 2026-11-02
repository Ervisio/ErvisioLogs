# Product requirements

## Goals
- Ingest, persist, search, stream, export and alert on heterogeneous structured/unstructured logs.
- Project-centric UX simpler than typical observability suites; FiveM presets without FiveM-specific core coupling.
- Multi-user workspaces, project environments, streams, RBAC, scoped service tokens.
- Full documented REST API, OpenAPI 3.1, SDK examples; OTLP compatibility via explicit adapters.
- Standalone Docker Compose plus Ervisio managed/external plugin adapter.
- Storage options: local-only, local+S3, local+Hetzner Storage Box (SFTP), with optional second remote replica later.
- Configurable local max age, global remote retention, cache max GB and high/low watermarks.
- English default; Italian localization; responsive light/dark UI.

## Explicit behavior
- A log can be in local hot storage and remote archive simultaneously; after local age/capacity eviction it remains searchable remotely until global retention expires.
- Local retention is a maximum age, NOT a guaranteed minimum when capacity is reached.
- Remote retention age is measured from event timestamp under a documented skew policy, not from upload date.
- Never evict the only durable copy. No silently dropped accepted logs.
- SFTP archival has slower remote search than S3 and requires staging/cache; communicate this in UI.
- Global retention and deletion are eventually enforced with measurable deletion SLA; handle legal holds separately in later phase.

## Non-goals for MVP
- Arbitrary cross-tenant SQL, multi-region HA, full SIEM, exactly-once delivery, guaranteed real-time historical search on SFTP, advanced AI.

## UX success
- New user can create project, token, send first log and see it within 2 minutes (measured after install).
- User can find logs by time, severity, stream, source, text and common structured attributes.
