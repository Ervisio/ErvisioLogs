# Off-site archive over SFTP (Hetzner Storage Box)

Ervisio Logs can copy sealed days to an SFTP server. You sign in with a password or with an SSH key. Neither one ever
goes through the browser or into the database: each sits in its own file in the secrets folder (ADR 0008, ADR 0015).

| Install | Secrets folder | Owner and mode |
|---|---|---|
| Docker Compose | `secrets/` next to `compose.yaml`, mounted at `/run/ervisio-logs/secrets` | uid 65532, folder `0700`, files `0400` |
| Standalone | `/etc/ervisio-logs/secrets` | `root:ervisio-logs`, folder `2750`, files `0640` |

`ERVISIO_LOGS_ARCHIVE_SECRETS_DIR` points to the folder. Compose and the systemd unit set it; set it yourself for any
other layout.

## 1. Collect the Storage Box details

In the Hetzner console, turn on SSH support for the box. You need:

- host: `uXXXXX.your-storagebox.de`
- user: `uXXXXX` (or a sub-account `uXXXXX-subN`)
- port: `23`, the SSH port Hetzner documents for Storage Boxes.
- the host key. Run it from a machine you trust and keep only the `ssh-ed25519` line, without the host name in front:

  ```sh
  ssh-keyscan -p 23 -t ed25519 uXXXXX.your-storagebox.de
  ```

## 2. Write the credentials file

**From the Ervisio plugin**: Settings › Archive credentials. Type a file name such as `storagebox`, pick Password or
SSH key, paste it and save. The plugin installs the file with the owner and mode above and lists names only.

**By hand, Docker** (in `/opt/stacks/ervisio-logs` or wherever `compose.yaml` lives):

```sh
sudo install -d -m 0700 -o 65532 -g 65532 secrets
read -rs PW; printf '%s\n' "$PW" | sudo sh -c 'umask 077 && cat > secrets/storagebox && chown 65532:65532 secrets/storagebox && chmod 0400 secrets/storagebox'; unset PW
```

**By hand, standalone**:

```sh
f=/etc/ervisio-logs/secrets/storagebox
read -rs PW; printf '%s\n' "$PW" | sudo sh -c "umask 077 && cat > $f && chown root:ervisio-logs $f && chmod 0640 $f"; unset PW
```

With a key instead of a password, the file holds the unencrypted private key in OpenSSH format.

## 3. Add the destination

In Ervisio Logs, Storage › Add destination › SFTP server:

| Field | Value |
|---|---|
| Host | `uXXXXX.your-storagebox.de` |
| Port | `23` |
| User | `uXXXXX` |
| Sign in with | Password (or SSH key) |
| Folder on the server | `ervisio-logs` (relative to the login folder; create it first if the box refuses) |
| Host public key | the `ssh-ed25519 AAAA...` line from step 1 |
| Credentials file | `storagebox` |

Ervisio Logs connects and checks the folder before it saves. A wrong host key, password or file name shows up there.

The stored config reads `{"host": ..., "port": 23, "user": ..., "base_dir": ..., "host_key": ..., "auth": "password"}`.
Leave `auth` out, or set `"key"`, for key sign-in.

Not tried against a real Storage Box yet.
