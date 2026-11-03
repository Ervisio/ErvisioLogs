# ADR 0017: Type archive credentials in the Storage page

Status: Proposed. Written in 0.1.4. Checked with `go build`, `go vet`, `tsc` and `vite build` only.

## Context
ADR 0008 kept archive credentials out of the API: the Storage form asked for the name of a file that someone had
already written in the secrets folder, by hand or through the Ervisio plugin (ADR 0015). Owners asked to type the S3
access key and secret key, or the SFTP password or key, straight into the form, or to load them from a file on their
own computer.

## Options
1. Keep file names only and point people at the plugin or a shell.
2. Store the credentials in the database, encrypted with the instance secret key.
3. Let the instance administrator send the value once over the API; the server writes it to a new file in the secrets
   folder, readable by the service only, and never returns it.

## Decision
Option 3. It keeps the storage model of ADR 0008 (one file per destination, nothing in the database, nothing in
backups of PostgreSQL) and changes only how the file gets there.

- `PUT /api/v1/admin/storage/secrets/{name}` with `{"value": ...}`: instance administrators only, 32 KiB at most, no
  NUL bytes. The server writes a temporary file with mode `0600`, syncs it and renames it over `name`. The audit log
  records the name and whether a file was replaced, never the value.
- `GET /api/v1/admin/storage/secrets` lists file names, so the form can offer the ones already there.
- The form defaults to "Enter them here": access key and secret key for S3 (sent as `{"access_key","secret_key"}`),
  the password or the private key for SFTP. A file picker reads a file on the user's computer in the browser and fills
  the same fields; for S3 it expects the JSON layout above. On submit the page writes the file as
  `<destination-slug>-<time in base 36>`, then creates the destination with that `secret_file`, and clears the fields.
- "Use a file already on the server" keeps the old behaviour.
- The secrets folder becomes writable by the service: Compose drops `:ro` from the mount, `install.sh` creates
  `/etc/ervisio-logs/secrets` as `root:ervisio-logs 2770`, the systemd unit adds it to `ReadWritePaths`, and the
  plugin's `standalone-secrets-init` command uses `2770`.

## Risks
- A compromised instance administrator session can now overwrite a credentials file. It could already repoint a
  destination to any file name, so the new power is writing content, not reading it.
- The value crosses the network once. It needs HTTPS in front of the app, as sign-in does.
- If creating the destination fails after the file was written, the file stays in the folder. Pick it under "Use a
  file already on the server" or delete it by hand.
- A process inside the app container or the service account can now change the secrets folder. Before, a bug in the
  app could read those files but not replace them.
- A secrets folder outside `/etc/ervisio-logs/secrets` on a standalone install stays read-only until its path joins
  `ReadWritePaths`; the page then reports a server error on save.

## Tests needed
Unit test in `web/src/App.test.tsx` for the S3 flow (written, not run). On a Docker install and a standalone install:
add an S3 and an SFTP destination by typing the credentials, check the file mode and owner, and check that the health
check passes. Not run.
