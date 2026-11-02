# UI/UX specification

Design intent: professional, uncluttered, intuitive, dense only where useful. English primary, Italian translations; light/dark; keyboard accessibility and responsive mobile.

## Screens
Onboarding wizard (workspace -> project -> source -> token -> sample -> first event); overview (events, errors, ingestion rate, archive lag, storage health); projects/environments/streams; Log Explorer (time range, text, severity, stream, attribute builder, JSON detail, cursor pagination, saved queries); Live Tail (pause/resume, bounded buffer, reconnect); dashboards; alerts; API tokens; users/roles; integrations; storage providers; retention/cache; usage; audit; admin health.

## Storage settings UX
Provider cards: Local / Hetzner Storage Box SFTP / S3. Connection test with safe error feedback. Retention fields: local maximum days, global days, max cache GB, eviction high/low thresholds. Explain early eviction, remote-only slower searches, pending uploads, cache bytes vs total disk. Show current hot bytes, archive bytes, WAL bytes, oldest unarchived event, upload backlog, remote latency and last successful health check. Dangerous delete/purge requires typed confirmation.

## Interaction
Cmd/Ctrl+K command palette; virtualized log list; search result source badge Hot/Archive; explicit partial-result warning; copy event JSON; export with permission; skeleton/loading/error/empty states; accessibility AA target. Avoid rendering unbounded live stream DOM. Mobile log details as drawer.
