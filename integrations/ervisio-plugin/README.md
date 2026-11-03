# ervisio-logs: Ervisio plugin

Deploys and manages Ervisio Logs from Ervisio, as a Docker Compose stack or as a systemd service, and publishes it
through the Caddy already on the server. User guide: [`docs/deploy/ervisio-plugin.md`](../../docs/deploy/ervisio-plugin.md).
Design: ADR 0012 and ADR 0014.

Built with `@ervisio/plugin-sdk` 0.3.0 (contract version 3), like plugin-docker and plugin-caddy. Not loaded in a real
Ervisio yet.

| Path | Role |
|---|---|
| `plugin/manifest.json` | id, page, commands, files, the `docker` HTTP capability over `/var/run/docker.sock` |
| `plugin/bin/standalone.sh` | Downloads the release tarball, checks its SHA-256, runs `install.sh`; or runs `uninstall.sh` |
| `src/index.ts` | `activate(sdk)`: registers strings (en, it), styles and the page |
| `src/views/` | Header and inner menu (`LogsPage`), `Setup`, `Overview`, `Address` (Caddy), `Settings`, `Console` |
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

Rules the code keeps: no `confirm()` (the frame has no `allow-modals`), no secret shown on the page, no command that
deletes volumes or data, every Caddy change validated and rolled back on failure.

`LICENSE-plugin-caddy` covers the Caddyfile and Docker code adapted from Ervisio/plugin-caddy (MIT).
