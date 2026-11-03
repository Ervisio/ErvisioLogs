# ADR 0015: SFTP password sign-in and a secrets folder in every install

Status: Proposed. Written in 0.1.2. Checked with `go build`, `go vet`, `tsc`, the plugin and web builds and
`docker compose config` only. No SFTP server was reached with a password, and the plugin commands never ran.
Builds on ADR 0008 (secrets live in files, never in the database) and ADR 0014 (plugin).

## Context
The owner wanted a Hetzner Storage Box as the off-site archive and has only a user name and password. The SFTP
provider accepted only a private key. Also, no install set `ERVISIO_LOGS_ARCHIVE_SECRETS_DIR`, so S3 and SFTP
destinations failed with "no secrets directory is configured" in Docker, standalone and the plugin alike. The web form
hint described a JSON file with `password` or `private_key` that the backend never read.

## Options
1. Store the password in the provider config. Simple, but it puts a credential in PostgreSQL, against ADR 0008.
2. Keep one secret file per destination and add `auth` to the SFTP config, saying whether the file holds a key or a
   password. Give every install a secrets folder and let the plugin write files into it.

## Decision
Option 2.
- **Backend**: `SFTPConfig.Auth` is `""`/`"key"` (default, existing providers unchanged) or `"password"`. With
  password, the file content minus one trailing newline is the password, offered as `password` and as
  `keyboard-interactive` (every prompt gets the password). The host key pin stays mandatory.
- **Web**: the SFTP form has "Sign in with" (SSH key, Password); the hint now says what the file holds.
- **Compose**: the app gets `ERVISIO_LOGS_ARCHIVE_SECRETS_DIR=/run/ervisio-logs/secrets` and the bind mount
  `./secrets:/run/ervisio-logs/secrets:ro`.
- **Standalone**: the unit sets `Environment=ERVISIO_LOGS_ARCHIVE_SECRETS_DIR=/etc/ervisio-logs/secrets` (the env file
  overrides it), and `install.sh` creates the folder `root:ervisio-logs 2750`.
- **Plugin**: Settings › Archive credentials writes the value to a staging file inside the already closed stack or
  config folder, runs a root `install` that places it with the owner and mode the app reads (Docker: uid 65532,
  `0400`; standalone: `root:ervisio-logs`, `0640`), then removes the staging file. The page lists file names through
  `ls`, never content. Install in Docker mode creates the folder before `compose up`, so Docker does not create it as
  root.

## Risks
- The staging file holds the secret for the length of one command, in a folder open to the docker group (Docker) or
  to root only (standalone). If removal fails, the plugin says so and the file stays.
- Upgrading a 0.1.1 Compose stack: the plugin's Update creates `secrets/`. Someone who upgrades by hand and skips
  that gets a root-owned folder from Docker, which the app can list but whose files root owns; the guide covers it.
- Password sign-in is weaker than a key; the guide offers both.

## Tests needed
Run `TestSFTPContract` with a password account; connect to a real Storage Box on port 23 with a password and with a
key; write credentials from the plugin in both modes and check owner and mode; upgrade a 0.1.1 stack and a 0.1.1
standalone install. None has run.
