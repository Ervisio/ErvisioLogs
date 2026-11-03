# ADR 0014: Plugin 0.1.1 on the plugin SDK, with Caddy publishing

Status: Proposed. Written in 0.1.1. Checked with `tsc --noEmit` and `vite build` only. The plugin has not been loaded
in a real Ervisio, and no Caddy was changed through it. Builds on ADR 0012 (plugin) and ADR 0013 (PostgreSQL TLS).

## Context
The owner tried plugin 0.1.0 and reported three problems:
- The page looked unlike the other Ervisio plugins and its settings were hard to use.
- The app container exited in production: "ERVISIO_LOGS_PUBLIC_URL must be https in production" and
  "ERVISIO_LOGS_POSTGRES_DSN: sslmode=disable is refused in production".
- It did not work with the Caddy already on the server. It registered no site, so the owner had to add one by hand.
  The owner asked for full compatibility with the existing plugins.

## Options
1. Patch the plain-DOM `index.js` of 0.1.0. Small change, still unlike the other plugins, no shared kit.
2. Rebuild on `@ervisio/plugin-sdk` 0.3.0 (contract version 3), the kit plugin-docker, plugin-caddy and ErvisioDrive
   use, and port plugin-caddy's Caddyfile code. More code, same look and same Caddy rules as the other plugins.

## Decision
Option 2.
- **Page**: React through the SDK shim, built with the SDK's Vite preset into `dist/ervisio-logs/index.js`. Header
  with state, an inner menu like plugin-docker's (Overview, Address, Settings, Console), Ervisio theme tokens, all
  classes prefixed `el-`. Settings uses rows with a save bar, never shows secrets, and blocks Save while you ask for
  production with values the backend refuses.
- **Production errors**: the TLS fix sits in the stack (ADR 0013). The plugin switches to production only when
  `productionProblems` finds nothing, and lists what blocks it otherwise.
- **Caddy**: detection of `caddy.service` and of Caddy containers through a `docker` HTTP capability on
  `/var/run/docker.sock` (admin unless the user is in the `docker` group, rules limited to the calls listed in the
  manifest). The site goes into `ervisio-logs.caddy` plus one `import` line, or inline between marker comments when a
  container mounts only its Caddyfile. The tokenizer and block format come from plugin-caddy (MIT, see
  `LICENSE-plugin-caddy`), so both plugins read each other's files. Each change: validate, reload, and on failure
  restore the files byte for byte and reload. A site written by hand for the same host counts as published and stays
  untouched. Cases the plugin cannot write show the block to paste.
- **Network**: `deploy/compose.yaml` adds the `ervisiologs_edge` network with the alias `ervisio-logs-app`, so a Caddy
  container reaches the app without the host port. The plugin connects the Caddy container to it.
- **Release**: `scripts/release.sh` writes `src/bundle.generated.ts`, runs `npm ci && npm run build`, adds the
  standalone checksums and `uninstall.sh`, and packs the same reproducible archive as before. The release workflow
  rewrites `ssh://git@github.com/` to https so the git dependency resolves without a key.

## Risks
- `sdk.api.http` over the Docker socket and the `adminUnlessGroup` field depend on Ervisio honouring the manifest as
  plugin-caddy's does. Not checked against a running Ervisio.
- Connecting a Caddy container to `ervisiologs_edge` changes that container's networks. Removing the site does not
  disconnect it.
- A Caddyfile edited at the same moment by another tool can be overwritten by the rollback.
- The SDK comes from a git tag, not npm.

## Tests needed
Load the signed 0.1.1 in Ervisio; install in both modes; publish through a system Caddy, a Caddy container with its
config folder mounted, and one with only the Caddyfile mounted; force a validate failure and check the rollback;
remove the site; switch to production; upgrade from 0.1.0. None has run.
