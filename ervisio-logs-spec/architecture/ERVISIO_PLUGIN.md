# Ervisio integration contract — discovery required

Three deployment modes: standalone; managed by Ervisio; external linked instance. Core service must not depend on Ervisio availability. Adapter handles discovery, lifecycle, navigation, health, SSO and role mapping. Uninstall adapter must never silently delete log data.

## Mandatory discovery before coding adapter
Inspect actual Ervisio core/plugin SDK repositories and manifests: registration API, service lifecycle, Docker orchestration, authentication and role model, frontend embedding, routing, permissions, version compatibility. Write ADR and compatibility matrix. If repo unavailable, scaffold adapter interfaces and mark integration blocked; DO NOT invent proprietary plugin protocol or claim integration works.

## Auth
Prefer OIDC or documented token exchange; verify issuer/audience/signature and map identities/roles; never trust unsigned headers. Cross-origin embedding needs CSP/frame-ancestors/cookie strategy. Standalone local auth remains functional.
