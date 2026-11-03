# Ervisio Logs: Ervisio plugin

The plugin adds an **Ervisio Logs** page to Ervisio (0.5.0 or later, Linux). From that page you choose how the app
runs on the server and you manage it:

- **Docker**: a Compose stack in `/opt/stacks/ervisio-logs`, the folder the Docker plugin reads, so the stack also
  appears under Docker › Stacks.
- **Standalone**: the release binary run by systemd (see [standalone.md](standalone.md)).

Design and limits: ADR 0012. The plugin has **not** been loaded in a real Ervisio yet. Try it on a test server first.

## Install the plugin

Download `ervisio-logs-<version>-ervisio-plugin.tar.gz` from the release page and install it in Ervisio under
Plugins › Install, from its https address or from a path on the server. The package is not signed by the Ervisio
team yet, so Ervisio refuses it unless `plugins.allow_unsigned` is on in the Ervisio config, or developer mode is on.

Ervisio unpacks it into `/var/lib/ervisio/plugins/ervisio-logs`. The standalone commands run
`/var/lib/ervisio/plugins/ervisio-logs/bin/standalone.sh`, so standalone mode only works from that folder. A checkout
of `integrations/ervisio-plugin` loaded in developer mode shows the page but refuses to install anything: it has no
release version, compose file or checksums. Build a package with `scripts/release.sh` instead.

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
   **Update** to apply them.
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
update to a newer release, and plugin removal. ADR 0012 lists the tests needed.
