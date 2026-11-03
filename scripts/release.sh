#!/usr/bin/env bash
# Builds the release artifacts into dist/<version>/ (ADR 0011):
#   ervisio-logs-<v>-linux-amd64.tar.gz   standalone: binary with the web UI embedded, systemd unit, installer
#   ervisio-logs-<v>-linux-arm64.tar.gz
#   ervisio-logs-<v>-docker.tar.gz        Compose bundle that pulls the published image
#   ervisio-logs-<v>-ervisio-plugin.tar.gz  Ervisio plugin (when integrations/ervisio-plugin exists)
#   SHA256SUMS
# It does not build or push the container image: see .github/workflows/release.yml.
#
#   VERSION=1.2.3 scripts/release.sh
#   IMAGE=ghcr.io/ervisio/ervisio-logs VERSION=1.2.3 SKIP_WEB=1 scripts/release.sh
set -euo pipefail
cd "$(dirname "$0")/.."
root="$(pwd)"

version="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)}"
version="${version#v}"
commit="$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
image="${IMAGE:-ghcr.io/ervisio/ervisio-logs}"
[[ "$version" =~ ^[0-9A-Za-z.+-]+$ ]] || { echo "release: bad VERSION $version" >&2; exit 2; }

out="$root/dist/$version"
rm -rf "$out"
mkdir -p "$out"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Web UI, embedded by go:embed. The placeholder .gitkeep stays.
if [[ "${SKIP_WEB:-0}" != 1 ]]; then
  (cd web && npm ci --no-audit --no-fund && npm run build)
fi
[[ -f web/dist/index.html ]] || { echo "release: web/dist is empty; build the UI or unset SKIP_WEB" >&2; exit 1; }
find backend/internal/webui/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
cp -R web/dist/. backend/internal/webui/dist/

ldflags="-s -w -X github.com/Ervisio/ErvisioLogs/backend/internal/version.Version=$version -X github.com/Ervisio/ErvisioLogs/backend/internal/version.Commit=$commit"
for arch in amd64 arm64; do
  name="ervisio-logs-$version-linux-$arch"
  dir="$work/$name"
  mkdir -p "$dir"
  (cd backend && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "$ldflags" -o "$dir/ervisio-logs" ./cmd/ervisio-logs)
  cp deploy/standalone/ervisio-logs.service deploy/standalone/ervisio-logs.env.example "$dir/"
  install -m 0755 deploy/standalone/install.sh "$dir/install.sh"
  install -m 0755 deploy/standalone/uninstall.sh "$dir/uninstall.sh"
  cp docs/deploy/standalone.md "$dir/README.md"
  tar -C "$work" --owner=0 --group=0 -czf "$out/$name.tar.gz" "$name"
  echo "built $name.tar.gz"
done

# Docker bundle: the same compose.yaml, with the local build replaced by the published image.
name="ervisio-logs-$version-docker"
dir="$work/$name"
mkdir -p "$dir/scripts"
sed -e '/^    build: /d' \
    -e "s|\${ERVISIO_LOGS_IMAGE:-ervisio-logs:dev}|\${ERVISIO_LOGS_IMAGE:-$image:$version}|" \
    -e 's|^# Usage: .*|# Usage: scripts/gen-env.sh \&\& docker compose --env-file .env -f compose.yaml up -d --wait|' \
    deploy/compose.yaml > "$dir/compose.yaml"
grep -q '^    build: ' "$dir/compose.yaml" && { echo "release: build: left in the docker bundle" >&2; exit 1; }
cp .env.example "$dir/.env.example"
install -m 0755 scripts/gen-env.sh "$dir/scripts/gen-env.sh"
cp docs/deploy/docker.md "$dir/README.md"
tar -C "$work" --owner=0 --group=0 -czf "$out/$name.tar.gz" "$name"
echo "built $name.tar.gz"

# Ervisio plugin (ADR 0012). It deploys this same release: the Compose bundle's compose.yaml and .env.example go
# into index.js, and the standalone tarballs' checksums into bin/release.sha256 for bin/standalone.sh.
# The archive's top-level folder is ervisio-logs, the plugin id.
if [[ -d integrations/ervisio-plugin ]]; then
  name="ervisio-logs-$version-ervisio-plugin"
  dir="$work/plugin/ervisio-logs"
  mkdir -p "$work/plugin"
  cp -R integrations/ervisio-plugin "$dir"
  install -m 0755 deploy/standalone/uninstall.sh "$dir/bin/uninstall.sh"
  chmod 0755 "$dir/bin/standalone.sh"
  printf '%s\n' "$version" > "$dir/bin/release.version"
  (cd "$out" && sha256sum -- ervisio-logs-"$version"-linux-*.tar.gz) > "$dir/bin/release.sha256"
  python3 - "$dir" "$version" "$work/ervisio-logs-$version-docker" <<'PY'
import json, re, sys
plugin, version, bundle = sys.argv[1:4]
mpath = f"{plugin}/manifest.json"
with open(mpath, encoding="utf-8") as f:
    m = json.load(f)
m["version"] = version.split("+")[0]
with open(mpath, "w", encoding="utf-8", newline="\n") as f:
    json.dump(m, f, indent=2)
    f.write("\n")
read = lambda p: open(p, encoding="utf-8").read()
data = {"version": version, "compose": read(f"{bundle}/compose.yaml"), "envExample": read(f"{bundle}/.env.example")}
jpath = f"{plugin}/index.js"
src = read(jpath)
line = "const BUNDLE = " + json.dumps(data, ensure_ascii=False) + "; // @bundle"
src, n = re.subn(r"^const BUNDLE = .*; // @bundle$", lambda _: line, src, count=1, flags=re.M)
if n != 1:
    sys.exit("release: the @bundle line is missing from integrations/ervisio-plugin/index.js")
with open(jpath, "w", encoding="utf-8", newline="\n") as f:
    f.write(src)
PY
  # Ervisio installs plugins from a .tar.gz (an https:// address or a local path) and strips the top-level folder.
  tar -C "$work/plugin" --owner=0 --group=0 -czf "$out/$name.tar.gz" ervisio-logs
  echo "built $name.tar.gz"
fi

(cd "$out" && sha256sum -- *.tar.gz > SHA256SUMS)
echo "artifacts in dist/$version (SHA256SUMS included)"
