# ADR 0001: Ervisio plugin contract (D006)

Status: Superseded in part by ADR 0012 (P6). Discovery done on 2026-11-02. The open questions below were answered
from the Ervisio v0.6.2 sources on 2026-11-03; ADR 0012 lists the answers and the plugin built on them.

## Context
The spec forbids inventing the plugin protocol. I read the real sources (shallow clones of `Ervisio/ervisio` v0.6.2,
`Ervisio/plugin-sdk` v0.3.0 with contract version 3, `Ervisio/plugins`, `Ervisio/ErvisioDrive`).

## Verified facts
- An Ervisio plugin is a folder with a strict `manifest.json` (unknown fields are rejected) and one self-contained ES module.
  Source: `plugin-sdk/README.md`, `ervisio/docs/api/plugins.md`.
- Plugin code runs in a frame sandboxed with `allow-scripts allow-forms`, served by the daemon (`/plugin-frame/<id>`).
  It talks to the app only through `postMessage`; the app broker maps each request to one declared daemon call.
- The frame reaches the machine through three manifest-declared capabilities: `commands` (fixed argv),
  `http` (APIs on **unix sockets** or Windows named pipes, each with method + path-regex rules and a header allowlist)
  and `files` folders. Network access is limited to `capabilities.network` HTTPS/WSS origins.
- Admin operations go through a root bridge with an unlock dialog.
- `ErvisioDrive` is a standalone app (Next.js, OIDC relying party) that ships a companion plugin
  (`ErvisioDrive/plugin`) which installs and manages it through Docker commands. This is the closest precedent for a
  "managed by Ervisio" mode of a standalone product.
- The SDK is MIT licensed, copyright Fonlogen. The Ervisio core is MIT as well.

## Open in discovery (answered from source in ADR 0012, not yet tried at runtime)
- Whether the frame may embed an external web UI (the frame CSP is `default-src 'none'` and `connect-src` only for declared
  HTTPS origins), or whether `openUrl` (new tab) is the only way to present the Logs UI.
- Whether Ervisio can issue SSO assertions or OIDC tokens to a plugin's backend, and how its roles map to ours.
- Marketplace signing and registry requirements (`docs/PLUGIN-SIGNING.md`, `plugins/registry.json`).
- Compatibility matrix against real Ervisio installations.

## Decision (provisional, revised by ADR 0012)
1. Core stays independent of Ervisio. The backend has no Ervisio-specific code.
2. The Go adapter interface planned here (`discovery`, `health`, `identity`, `lifecycle`) was not built: Ervisio gives
   plugins no identity and no lifecycle hooks, so there is nothing for the backend to adapt to. The plugin is a
   deployer and launcher (ADR 0012).
3. Shape (b) was chosen: the plugin opens the Logs UI with `sdk.openExternal`. Shape (a) is out: `http` reaches unix
   sockets only, and the Logs service listens on TCP.

## Tests required before Accepted
Run the plugin against a real Ervisio 0.6.x install: load as dev plugin, call health through `capabilities.http`,
uninstall without data loss.
