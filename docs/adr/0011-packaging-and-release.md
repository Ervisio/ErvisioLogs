# ADR 0011: Packaging and release (P7)

Status: Proposed. Implemented in P7: `scripts/release.sh`, `.github/workflows/release.yml`, `deploy/standalone/`,
`docs/deploy/`. Nothing here has been run yet: no release was built, no image was pushed, no standalone install was
tried on a real host. The scripts pass `bash -n` and the linux/arm64 cross-compile passes `go build`.

## Context
The app must be deployable three ways: as a standalone service, as a Docker Compose stack, and through an Ervisio
plugin that picks one of the two (ADR 0012). All three must ship the same version from one tag, and none may need a
build toolchain on the target host.

## Decisions

### One binary, three packages
- The Go binary embeds the migrations and the built web UI, so a release is one static file per architecture
  (`CGO_ENABLED=0`, linux amd64 and arm64). The version and commit come from `-ldflags` (`internal/version`).
- `scripts/release.sh` writes `dist/<version>/`:
  - `ervisio-logs-<v>-linux-<arch>.tar.gz`: binary, systemd unit, env example, `install.sh`, `uninstall.sh`, README.
  - `ervisio-logs-<v>-docker.tar.gz`: `deploy/compose.yaml` with `build:` removed and the image pinned to
    `ghcr.io/ervisio/ervisio-logs:<v>`, plus `.env.example`, `scripts/gen-env.sh` and a README.
  - `ervisio-logs-<v>.tar.gz` and `.sha256`: the Ervisio plugin, stamped with the same version (ADR 0012), as a
    reproducible archive in the `Ervisio/plugins` registry format; `ervisio-logs-<v>-ervisio-plugin.tar.gz` is a copy.
  - `SHA256SUMS` for all of them.
- The script fails if a `build:` line is left in the Docker bundle, so the bundle can never build from source.

### Container image
- Multi-arch (amd64, arm64) with `FROM --platform=$BUILDPLATFORM` for the web and Go stages and `GOOS`/`GOARCH` from
  `TARGETOS`/`TARGETARCH`: the Go cross-compiler builds arm64 without QEMU. The final stage stays distroless, uid
  65532, with a `HEALTHCHECK` on `ervisio-logs healthcheck`.
- `ERVISIO_LOGS_IMAGE` overrides the image in Compose, so an owner can pin a digest or a mirror.

### Standalone (systemd)
- `install.sh` creates the system user, installs the binary atomically (`.new` then `mv`), installs the unit,
  creates `/etc/ervisio-logs/ervisio-logs.env` (0640 root:ervisio-logs) once with a random secret key that it never
  prints, and points the archive cache into ClickHouse's `user_files` when it finds a local ClickHouse (ADR 0002:
  ClickHouse reads archived segments with `file()`).
- It enables the service but starts it only when the env file has no placeholder left (`CHANGE_ME`, an empty
  ClickHouse password, `logs.example.com`). Re-running it upgrades: binary and unit are replaced, the env file stays.
- The unit runs `config-check` and `migrate up` as `ExecStartPre`, so a bad setting or a failed migration stops the
  start with the reason in the journal. Hardening: `ProtectSystem=strict`, `NoNewPrivileges`, an empty capability
  bounding set, `@system-service` syscall filter, `MemoryDenyWriteExecute`, address families INET/INET6/UNIX.
- `uninstall.sh` removes the unit and the binary and deletes no data. It lists what it kept.

### Release workflow
- On a `v*` tag: check the tag is semver, build and push the image with `docker buildx`, run `scripts/release.sh`,
  then create a **draft** GitHub release with `gh release create --verify-tag --draft`, notes from
  `docs/releases/<version>.md`. A person publishes the draft.
- It uses only the actions already pinned in `ci.yml` and the CLIs on the runner. No new third-party action.

## Consequences
- An owner without Go or Node can deploy from the release page.
- The Docker bundle pulls from GHCR: the package must be public, or the owner logs in first.
- The plugin and the app share a version: upgrading the plugin is how a plugin user upgrades the app.

## Not yet verified
- The registry asset names from `scripts/release.sh` (added after 0.1.0; for 0.1.0 they were uploaded by hand). The
  tag-triggered workflow ran for v0.1.0 and published the image and the other packages.
- `install.sh`, the unit's sandboxing (above all `SystemCallFilter` and `MemoryDenyWriteExecute` with the Go runtime)
  and `uninstall.sh` on a real Debian/Ubuntu/RHEL host.
- The Docker bundle started from the published image.
