# Security and privacy specification

## Identity and authorization
Local password auth Argon2id, OIDC integration, optional MFA (required for instance admins in production policy). RBAC: instance admin, workspace owner/admin, developer, analyst, viewer, service account. Custom scopes `logs:ingest`, `logs:read`, `logs:export`, `logs:delete`, `projects:write`, `tokens:manage`, `members:manage`, `storage:manage`, `alerts:manage`. Enforce scope and tenant filters in service layer AND query planner. Test cross-tenant IDOR.

## Credentials
Random high-entropy tokens shown once, store keyed hashes, support expiry/revocation/rotation; SFTP SSH private key protected, pinned host keys; S3 secret encryption and never echo in API. Least privilege provider credentials. No arbitrary URLs for provider health checks without SSRF controls.

## App hardening
TLS at reverse proxy, secure cookies, CSRF protection for cookie-auth mutations, CSP, output escaping, structured input validation, max JSON depth, rate limits by tenant/token/IP, audit security actions, bounded query resources. Never expose raw ClickHouse SQL endpoint to tenants. Encrypt backups and support restoration drills.

## Privacy
Redaction at earliest possible point; avoid logging secrets, passwords, auth headers, player identifiers unless necessary; configurable retention and subject deletion workflows; export/access auditing; GDPR-oriented controls but no blanket compliance claim. Remote archive and backup deletion semantics must be documented.

## Threat-model tests
Credential leak, brute force, token replay, SFTP MITM, archive tampering, query injection, tenant bypass, zip bomb / decompression bomb, oversized batch, WAL disk exhaustion, malicious Parquet, SSRF, unsafe plugin embedding and clickjacking.
