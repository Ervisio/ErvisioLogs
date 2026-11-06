# ervisio-logs: Ervisio plugin

Deploys and manages Ervisio Logs from Ervisio, as a Docker Compose stack or as a systemd service, and publishes it
through the Caddy already on the server. User guide: [`docs/deploy/ervisio-plugin.md`](../../docs/deploy/ervisio-plugin.md).
Design: ADR 0012, ADR 0014 and ADR 0019; navigation follows the approved F0 plugin variant B.

Built with `@ervisio/plugin-sdk` 0.3.0 (contract version 3), like plugin-docker and plugin-caddy. Not loaded in a real
Ervisio yet.

| Path | Role |
|---|---|
| `plugin/manifest.json` | id, page, commands, files, Docker and allowlisted admin-socket HTTP capabilities |
| `plugin/bin/standalone.sh` | Downloads the release tarball, checks its SHA-256, runs `install.sh`; or runs `uninstall.sh` |
| `src/index.ts` | `activate(sdk)`: registers strings (en, it), styles and the page |
| `src/views/management/` | Grouped navigation, connection, runtime settings, users, tenancy, metadata, ingest tokens, health, audit and archive management |
| `src/views/` | Existing setup, installation overview, Caddy, server env, archive credential files and console (20 recent jobs, up to 2000 lines each) |
| `src/api/admin-transport.ts` | SDK socket transport and ephemeral HTTPS Bearer transport |
| `../../packages/admin-client/` | Shared typed management client; authentication and storage belong to the caller |
| `src/api/logs.ts` | Mode, state, install, update, env file, production checks |
| `src/api/caddy.ts`, `caddy-core.ts` | Finds Caddy (system service or container), writes the site, validates, reloads, rolls back |
| `src/api/docker.ts` | The Docker Engine API calls the Caddy step needs |
| `src/bundle.generated.ts` | Release data: version, `compose.yaml`, `.env.example`. Empty in a checkout |
| `src/styles/logs.css` | Styles, all classes `el-`, theme tokens from Ervisio |

Build: `npm ci && npm run build` writes `dist/ervisio-logs/` (bundle plus `plugin/*`). That output still refuses to
install, because `bundle.generated.ts` holds no release. `scripts/release.sh` writes it from the release's Compose
bundle, builds, adds `bin/release.version`, `bin/release.sha256` and `bin/uninstall.sh`, and packs
`dist/<version>/ervisio-logs-<version>.tar.gz` with its `.sha256` (the `Ervisio/plugins` registry format) and a copy
named `ervisio-logs-<version>-ervisio-plugin.tar.gz`. `plugin/manifest.json` and `package.json` must carry the
release version, or the script stops.

Local admin access uses `/run/ervisio-logs/admin.sock` for systemd or the default Docker-volume socket at
`/var/lib/docker/volumes/ervisiologs_admin/_data/admin.sock`. A custom Docker data root needs an administrator-provided
host mapping. The SDK cannot dynamically substitute the manifest socket path. Socket permissions and a real host
have not been exercised here.

HTTPS connects only to a user-approved HTTPS origin. The installed SDK has no protected credential-storage API:
the Bearer token stays in memory while this page is open and is cleared on disconnect, connection change or unmount.
No token is saved to files, browser storage, env or console history. Requests omit cookies and reject redirects.
The SDK may reload the frame when approving a new host; enter the token again after that reload.

Runtime settings apply through the API without a restart. Server env changes keep their separate save and explicit
restart flow. Operators cannot read/export logs or create session-only instance admin tokens; project tokens issued
here are ingest-only. Workspace/project deletion requires typed slug confirmation and deletes their data through
the backend. The plugin never deletes Docker volumes. Caddy changes retain validation and rollback.

Checks: `npm test`, `npm run typecheck`, `npm run build`; `npm run test:ui` builds in memory and renders the real plugin
with a mock SDK using the repository's installed `web` React/JSDOM dependencies. It verifies connection-gated reads
and explicit setting writes, not host-kit appearance or real service behavior. Shared client tests run with
`node --test packages/admin-client/test/*.test.ts` from the repository root.

`LICENSE-plugin-caddy` covers the Caddyfile and Docker code adapted from Ervisio/plugin-caddy (MIT).

## Instance management (0.2 interface)

The nine sections separate runtime instance settings from host configuration. Workspace and project management,
users and sessions, archive destinations and credentials, retention and audit use `packages/admin-client`.
The console retains up to 20 operations and 2,000 lines per operation for the current page session.

Local management uses SDK HTTP capabilities, with no user session or log-reading permission:

- Docker: `/var/lib/docker/volumes/ervisiologs_admin/_data/admin.sock`, the rootful Docker volume from the bundled Compose stack.
- Standalone: `/run/ervisio-logs/admin.sock`, as configured by the standalone installer.

The Ervisio administrator must approve these socket capabilities. A custom Docker data directory or custom admin
socket path can instead use the HTTPS connection in **Instance** with a scoped `evl_adm_` token created by an
instance administrator in the web app. The SDK requests approval for the exact remote host before any request.
The panel reloads on first approval; reconnect afterwards. Tokens stay in memory and are never written to plugin
preferences, job output or local storage. Remote calls omit cookies. Tokens need the scopes for the operations
being used (`admin:read`, `admin:settings`, `admin:users`, `admin:tenancy`, `admin:storage`).
Operators can issue ingest tokens; read/export token creation still requires workspace membership.

**Server** exposes all documented environment variables, masks secret values, preserves existing secrets when
fields are empty, and reports values the installed Compose file does not pass. Environment changes require an
explicit restart. Runtime values belong in **Instance**; environment overrides lock those runtime values.
