# ervisio-logs: Ervisio plugin

Deploys and manages Ervisio Logs from Ervisio, as a Docker Compose stack or as a systemd service. User guide:
[`docs/deploy/ervisio-plugin.md`](../../docs/deploy/ervisio-plugin.md). Design: ADR 0012.

Not loaded in a real Ervisio yet.

| File | Role |
|---|---|
| `manifest.json` | id, page, commands (`docker -H {env} compose ...`, `systemctl`, `journalctl`, `standalone.sh`), file folders |
| `index.js` | The page, plain DOM in an ES module. `activate(sdk)` registers strings (en, it) and the page |
| `bin/standalone.sh` | Downloads the release tarball, checks its SHA-256, runs `install.sh`; or runs `uninstall.sh` |

This folder is the source, not an installable plugin. `scripts/release.sh` builds the package: it stamps the version
into `manifest.json`, replaces the `// @bundle` line of `index.js` with the release's `compose.yaml` and
`.env.example`, adds `bin/release.version`, `bin/release.sha256` and `bin/uninstall.sh`, and writes
`dist/<version>/ervisio-logs-<version>.tar.gz` with its `.sha256` (the format the `Ervisio/plugins` registry reads)
and a copy named `ervisio-logs-<version>-ervisio-plugin.tar.gz`. Without those files, the page refuses to install.

Rules the code keeps: no `confirm()` (the frame has no `allow-modals`), no secret shown on the page, no command that
deletes volumes or data.
