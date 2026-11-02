# ADR 0001: Ervisio plugin contract (D006)

Status: Proposed. Discovery done on 2026-11-02. No adapter code exists, and none may be written before P6.

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

## Not yet verified (blocks P6)
- Whether the frame may embed an external web UI (the frame CSP is `default-src 'none'` and `connect-src` only for declared
  HTTPS origins), or whether `openUrl` (new tab) is the only way to present the Logs UI.
- Whether Ervisio can issue SSO assertions or OIDC tokens to a plugin's backend, and how its roles map to ours.
- Marketplace signing and registry requirements (`docs/PLUGIN-SIGNING.md`, `plugins/registry.json`).
- Compatibility matrix against real Ervisio installations.

## Decision (provisional)
1. Core stays independent of Ervisio. P0 adds no Ervisio-specific code.
2. A future adapter implements a small Go interface in a separate package (`discovery`, `health`, `identity`, `lifecycle`).
   The interface is defined in P6 from the verified contract, not now.
3. Likely shapes, to test in P6: (a) a plugin whose `http` capability talks to the Logs service over a unix socket,
   (b) a plugin that links out with `openUrl`. Neither is claimed to work.

## Tests required before Accepted
Run the plugin against a real Ervisio 0.6.x install: load as dev plugin, call health through `capabilities.http`,
uninstall without data loss.
