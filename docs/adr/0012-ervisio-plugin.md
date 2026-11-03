# ADR 0012: Ervisio plugin that deploys Ervisio Logs (P6)

Status: Proposed. Implemented in P6 in `integrations/ervisio-plugin/`. The manifest was checked by hand against the
validator in `ervisio/server/internal/modules/plugins` (v0.6.2) and `index.js` passes `node --check`. The plugin has
**not** been loaded in a real Ervisio, and no install, upgrade or uninstall has been tried through it.
Builds on ADR 0001 (contract discovery) and ADR 0011 (release packages).

## Context
The owner asked for an Ervisio plugin that decides how to deploy Ervisio Logs on the server: standalone, or under
Docker through Ervisio's Docker plugin. ADR 0001 left four questions open. I answered them from the Ervisio v0.6.2
and plugin-docker v2.2.0 sources (code reading, not runtime):

- **Embedding the UI.** The frame is `sandbox="allow-scripts allow-forms"` with `default-src 'none'`: it cannot
  show another web app. `sdk.openExternal(url)` opens an http(s) URL in a new tab. The plugin uses that.
- **SSO.** Ervisio signs users in with PAM or SSH keys and gives plugins no identity token or OIDC flow. Ervisio Logs
  keeps its own accounts; the first administrator uses the setup code, as in the other deploy modes.
- **Talking to the app.** `capabilities.http` reaches unix sockets and named pipes only, never TCP. The plugin gets
  health through commands instead (`ervisio-logs healthcheck`, `docker compose ps`).
- **Plugin-to-plugin calls.** None exist. The Docker plugin lists any folder `/opt/stacks/<name>/` holding a
  `compose.yaml` as a stack, and drives it with `docker -H {env} compose --project-name <name> --project-directory
  /opt/stacks/<name> -f /opt/stacks/<name>/compose.yaml ...`.
- **Lifecycle.** Ervisio has no install or uninstall hook. Removing a plugin deletes only its folder in
  `/var/lib/ervisio/plugins/<id>` and its state. Plugins install from a `.tar.gz` (https URL or local path, at most
  32 MiB, top-level folder stripped); unsigned packages need `plugins.allow_unsigned` or developer mode.
- **Settings.** No storage API: plugins keep JSON in `~/.config/ervisio/plugins/<id>` (a `create: true` write folder,
  files 0600).
- **Modals.** The sandbox has no `allow-modals`, so `confirm()` returns false. The plugin confirms inline.

## Decisions

### One page, two modes
The plugin (`id: ervisio-logs`) contributes one page. On first open the administrator picks a mode; the choice is
saved in `~/.config/ervisio/plugins/ervisio-logs/settings.json`. The page shows Docker Compose's version when it
finds it, so the choice is informed. Changing mode later moves and deletes nothing.

### Docker mode: a stack the Docker plugin also sees
- The plugin writes `/opt/stacks/ervisio-logs/compose.yaml` and `.env`, and declares its own `compose-*` commands
  with **the same argv shape** as the Docker plugin (`remote: "docker"`, `{env}` as the whole `-H` item). The stack
  then shows up in Docker plugin › Stacks, where the owner can also manage it.
- Ervisio files writes create new files 0644, so the plugin first creates `/opt/stacks/ervisio-logs` as
  `root:docker 2770` (`stack-init`, after the Docker plugin's own `stacks-init` when `/opt/stacks` is missing). The
  `.env` with the database passwords is then unreadable outside the docker group.
- A new `.env` comes from the release's `.env.example` with random `POSTGRES_PASSWORD`, `CLICKHOUSE_PASSWORD` and
  `ERVISIO_LOGS_SECRET_KEY` made in the browser with `crypto.getRandomValues`. The page never displays them. An
  existing `.env` is kept; only missing secrets are added.
- Install and update are the same action: write `compose.yaml`, keep `.env`, `compose pull`, `compose up -d`.
- Stop is `compose down --remove-orphans`. No command can remove volumes: the manifest declares no `down -v`.
- Only the local Docker engine is used (no `env` passed). Remote environments would need the stack folder on the
  remote host, which the Docker plugin handles with paired servers; out of scope here.

### Standalone mode: the release tarball and install.sh
- `bin/standalone.sh install` (admin command) downloads `ervisio-logs-<v>-linux-<arch>.tar.gz` of **the version
  the plugin was built for** from the GitHub release, checks it against `bin/release.sha256` shipped inside the
  signed plugin, and runs its `install.sh` (ADR 0011). A mismatch installs nothing.
- `bin/standalone.sh uninstall` runs the release's `uninstall.sh`: service and binary go, data stays.
- Status, start, stop, restart and logs are plain `systemctl` and `journalctl` commands. Health is
  `ervisio-logs healthcheck` (127.0.0.1:8080).
- The settings form edits `/etc/ervisio-logs/ervisio-logs.env` in place (admin folder; the 0640 mode is kept).
  Secret fields (PostgreSQL DSN, ClickHouse password) are write-only: the page shows whether they are set, never the
  value, and an empty field keeps the current one.

### Packaging and versions
`scripts/release.sh` stamps the release version into `manifest.json`, writes `bin/release.version` and
`bin/release.sha256`, copies `uninstall.sh`, and replaces the `// @bundle` line of `index.js` with the Docker
bundle's `compose.yaml` and `.env.example`. A plugin folder that was not built this way (a dev checkout) shows a
notice and refuses to install. Upgrading the plugin, then pressing Update, upgrades the app.

### Uninstall never deletes data
Ervisio cannot run anything on plugin removal, so the page says so: removing the plugin leaves Ervisio Logs running.
The owner stops or removes it from the page first. Neither mode deletes volumes, databases, the spool or the env file.

## Consequences
- The plugin needs the root bridge for most actions (unlock dialog), except the Docker commands for members of the
  `docker` group and the journal for `systemd-journal`. Every call is in Ervisio's audit log.
- Standalone install needs outbound HTTPS from the server to github.com.
- The standalone commands run `/var/lib/ervisio/plugins/ervisio-logs/bin/standalone.sh`: a dev-mode plugin loaded
  from another folder can show the page but cannot install standalone (and has no release anyway).
- Marketplace listing goes through the `Ervisio/plugins` registry: its `register` workflow reads the latest stable
  release, checks the package with `plugin-sign` (0.6.2 validated the 0.1.0 manifest) and opens a PR; after a
  maintainer merges it, `publish` signs the package with the team key and adds it to the catalog. 0.1.0 is
  registered in Ervisio/plugins#35, not merged yet. Until a version is listed, the owner installs the `.tar.gz`
  with `plugins.allow_unsigned` or in developer mode.

## Tests required before Accepted
On a real Ervisio 0.6.x host: install the plugin package, run Docker mode end to end and see the stack in the Docker
plugin, run standalone mode end to end, update both modes to a newer release, remove the plugin and check that the
app and all its data are still there.
