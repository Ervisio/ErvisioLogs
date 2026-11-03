# Ervisio Logs: Ervisio plugin

The plugin adds an **Ervisio Logs** page to Ervisio (0.5.0 or later, Linux). From that page you choose how the app
runs on the server and you manage it:

- **Docker**: a Compose stack in `/opt/stacks/ervisio-logs`, the folder the Docker plugin reads, so the stack also
  appears under Docker › Stacks.
- **Standalone**: the release binary run by systemd (see [standalone.md](standalone.md)).

It also publishes the app on its own web address through the Caddy already on the server (see
[Publish through Caddy](#publish-through-caddy)).

Design and limits: ADR 0012 and ADR 0014. The plugin has **not** been loaded in a real Ervisio yet. Try it on a test server first.

## Install the plugin

Once the Ervisio team has listed it, install the signed plugin from Plugins › Browse (search "Ervisio Logs"). The
registry signs each version with the team key after a maintainer merges its registration PR on `Ervisio/plugins`.

Before that, or for a version not listed yet, download `ervisio-logs-<version>.tar.gz` from the release page and
install it under Plugins › Install, from its https address or from a path on the server. That copy is unsigned, so
Ervisio refuses it unless `plugins.allow_unsigned` is on in the Ervisio config, or developer mode is on.
`ervisio-logs-<version>-ervisio-plugin.tar.gz` holds the same bytes under the first release's name.

Ervisio unpacks it into `/var/lib/ervisio/plugins/ervisio-logs`. The standalone commands run
`/var/lib/ervisio/plugins/ervisio-logs/bin/standalone.sh`, so standalone mode only works from that folder. A checkout
of `integrations/ervisio-plugin` loaded in developer mode shows the page but refuses to install anything: it has no
release version, compose file or checksums. Build a package with `scripts/release.sh` instead.

## The page

A header shows the state of the app and its version. The inner menu, like plugin-docker's, has four sections:
**Overview** (the route internet › Caddy › app › databases with the state of each hop, the services and the
actions), **Address** (Caddy), **Settings** and **Console** (the full output of the last action, and the app logs on
request).

## Choose a mode

The first time you open the page, pick Docker or Standalone. The page shows whether Docker Compose was found. The
choice is saved in `~/.config/ervisio/plugins/ervisio-logs/settings.json`; changing it later moves and deletes nothing.

Most actions run as root, so Ervisio asks you to unlock. Members of the `docker` group run the Docker commands without
unlocking, and members of `systemd-journal` read the service journal. Ervisio records every call in its audit log.

### Docker

1. Press **Install**. The plugin creates `/opt/stacks` if missing and `/opt/stacks/ervisio-logs` as `root:docker`,
   mode 2770, writes `compose.yaml` from the release, creates `.env` with random database passwords and secret key,
   then pulls the images and starts the stack. The page never shows the generated secrets.
2. Under **Settings**, set the public URL, the environment, the port and proxy trust, then press **Save** and
   **Restart now** to restart with them. Or let **Address** set the public URL for you.
3. Press **Open** to open the app in a new tab.

**Update** writes the `compose.yaml` of the plugin's version, keeps `.env` (adding only missing secrets), pulls and
restarts. To upgrade the app, upgrade the plugin in Ervisio, then press Update. **Stop** runs `docker compose down`
without `-v`: the volumes and your data stay.

The Docker plugin can start, stop and edit the same stack. If you edit `compose.yaml` there, the next Update from
this page replaces it.

### Standalone

You provide PostgreSQL 15+ and ClickHouse 25.8 (the archive tier needs ClickHouse on the same machine; see
[standalone.md](standalone.md)). The server needs outbound HTTPS to github.com.

1. Press **Install**. The plugin downloads `ervisio-logs-<version>-linux-<arch>.tar.gz` for its own version, checks it
   against the SHA-256 shipped inside the plugin, and runs the release's `install.sh`. A checksum mismatch installs
   nothing. The service is enabled but does not start while the env file still holds placeholders.
2. Under **Settings**, fill in the public URL, the PostgreSQL DSN, the ClickHouse address, user and password, then
   **Save** and **Start**. Secret fields never show the current value: leave one empty to keep it.
3. **Logs** shows the last journal lines; **Open** opens the app.

**Update** reinstalls the binary and unit of the plugin's version and keeps `/etc/ervisio-logs/ervisio-logs.env`.
**Uninstall** runs the release's `uninstall.sh`: service and binary go, the env file, the data in PostgreSQL and
ClickHouse, the spool and the archive cache stay.

## Publish through Caddy

**Address** looks for Caddy on the server: the `caddy.service` unit, and, if you can use Docker, containers that
run a Caddy image. It suggests `logs.<your domain>` from the sites Caddy already serves.

- The plugin writes the site the way plugin-caddy does: in `ervisio-logs.caddy` next to the Caddyfile, loaded by one
  `import` line it appends to the Caddyfile. When a Caddy container has only its Caddyfile mounted, the block goes
  inside the Caddyfile between the `# Managed by the Ervisio Logs plugin` and `# End of the Ervisio Logs block` comments, so it survives the container being recreated.
- Each change runs `caddy validate` and a reload. If either fails, the plugin writes the files back exactly as they
  were and reloads again. Only after a good reload does it set `ERVISIO_LOGS_PUBLIC_URL` to the https address and
  `ERVISIO_LOGS_TRUST_PROXY=true`, switch the app to production when nothing else blocks it, and restart the app.
- Caddy in a container reaches the Docker stack on the `ervisiologs_edge` network, as `ervisio-logs-app:8080`. The
  plugin attaches the Caddy container to that network. The system Caddy reaches `127.0.0.1` and the app port.
- A site you wrote yourself for the same host shows as published. The plugin leaves it alone and offers to set
  only the public URL.
- When the plugin cannot write (a JSON config, a Caddyfile outside `/etc/caddy`, caddy-docker-proxy labels, a
  read-only mount, a stopped container, a Caddy container on a bridge network with the standalone service), it says
  why and shows the block to paste yourself.
- **Remove site** takes out only the plugin's block or file and the import line, with the same validate and rollback.

Writing `/etc/caddy`, `caddy validate` and the reload run as root, so Ervisio asks you to unlock.

## Production mode

The app refuses to start in production with an http public URL or a PostgreSQL DSN with `sslmode=disable`. Since
0.1.1 the Compose stack runs its PostgreSQL with TLS (ADR 0013), so the Docker mode can run in production once the
public URL is https. Settings lists what still blocks production and keeps **Save** disabled while you ask for
production with those values. In standalone mode, your DSN must use TLS.

## First sign-in

The app keeps its own accounts: Ervisio passes no identity to plugins. The first administrator signs in with the
setup code. Set `ERVISIO_LOGS_SETUP_CODE` in Settings, or read the generated code in the logs (look for
"setup code").

## Removing the plugin

Ervisio cannot run anything when it removes a plugin. Removing it deletes only the plugin folder and its settings:
Ervisio Logs keeps running with all its data. Stop or uninstall the app from the page first if you want it gone. To
delete the data too, remove the Docker volumes (`docker compose down -v` in `/opt/stacks/ervisio-logs`) or drop the
databases yourself.

## Not verified

Loading in Ervisio, the unlock flow for each command, the stack appearing in the Docker plugin, both install paths,
update to a newer release, plugin removal, and every Caddy path (system service, container with a mounted folder,
container with only the Caddyfile, rollback, remove). Nothing in 0.1.1 ran outside type checks and a build. ADR 0012 lists the tests needed.
