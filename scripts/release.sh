#!/usr/bin/env bash
# Builds the release artifacts into dist/<version>/ (ADR 0011):
#   ervisio-logs-<v>-linux-amd64.tar.gz   standalone: binary with the web UI embedded, systemd unit, installer
#   ervisio-logs-<v>-linux-arm64.tar.gz
#   ervisio-logs-<v>-docker.tar.gz        Compose bundle that pulls the published image
#   ervisio-logs-<v>.tar.gz (+ .sha256)   Ervisio plugin, unsigned, in the Ervisio/plugins registry format
#   ervisio-logs-<v>-ervisio-plugin.tar.gz  the same plugin bytes under the first name
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

# Keep the licenses of the installed web dependencies beside the embedded UI.
notices="$work/licenses"
node - "$root/web" "$notices" <<'JS'
const fs = require('node:fs');
const path = require('node:path');
const [web, out] = process.argv.slice(2);
fs.mkdirSync(out, { recursive: true });
const packages = JSON.parse(fs.readFileSync(path.join(web, 'package-lock.json'), 'utf8')).packages;
for (const [name, metadata] of Object.entries(packages)) {
  if (!name || metadata.dev) continue;
  const directory = path.join(web, name);
  if (metadata.optional && !fs.existsSync(directory)) continue;
  const files = fs.readdirSync(directory).filter((file) => /^(licen[cs]e|copying|notice)(\.|$)/i.test(file));
  if (!files.length) throw new Error(`release: no license notice for ${name}`);
  const target = path.join(out, name.replace(/^node_modules\//, '').replaceAll('/', '__'));
  fs.mkdirSync(target, { recursive: true });
  for (const file of files) fs.copyFileSync(path.join(directory, file), path.join(target, file));
}
JS

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
  cp -R "$notices" "$dir/licenses"
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
cp -R "$notices" "$dir/licenses"
tar -C "$work" --owner=0 --group=0 -czf "$out/$name.tar.gz" "$name"
echo "built $name.tar.gz"

# Ervisio plugin (ADR 0012, ADR 0014). It deploys this same release: the Compose bundle's compose.yaml and
# .env.example go into src/bundle.generated.ts before the build, and the standalone tarballs' checksums into
# bin/release.sha256 for bin/standalone.sh. The plugin builds with @ervisio/plugin-sdk (Vite preset and
# ervisio-plugin-pack). The archive's top-level folder is ervisio-logs, the plugin id.
if [[ -d integrations/ervisio-plugin ]]; then
  name="ervisio-logs-$version-ervisio-plugin"
  src="$work/repository/integrations/ervisio-plugin"
  dir="$work/plugin/ervisio-logs"
  mkdir -p "$work/plugin"
  mkdir -p "$work/repository/packages"
  (cd packages/admin-client && git ls-files -z --cached --others --exclude-standard | tar --null -T - -cf -) \
    | { mkdir -p "$work/repository/packages/admin-client" && tar -C "$work/repository/packages/admin-client" -xf -; }
  # A clean copy without node_modules or dist from a development checkout.
  (cd integrations/ervisio-plugin && git ls-files -z --cached --others --exclude-standard | tar --null -T - -cf -) \
    | { mkdir -p "$src" && tar -C "$src" -xf -; }
  node - "$src" "$version" "$work/ervisio-logs-$version-docker" <<'JS'
const fs = require('fs');
const [plugin, version, bundle] = process.argv.slice(2);
const want = version.split('+')[0];
for (const p of ['package.json', 'plugin/manifest.json', 'package-lock.json']) {
  const metadata = JSON.parse(fs.readFileSync(`${plugin}/${p}`, 'utf8'));
  const v = metadata.version;
  if (p === 'package-lock.json' && metadata.packages?.['']?.version !== want) {
    console.error(`release: the plugin lockfile root package must be version ${want}`); process.exit(1);
  }
  if (v !== want) { console.error(`release: integrations/ervisio-plugin/${p} is version ${v}, the release is ${want}`); process.exit(1); }
}
const read = (p) => fs.readFileSync(p, 'utf8');
const data = { version, compose: read(`${bundle}/compose.yaml`), envExample: read(`${bundle}/.env.example`) };
fs.writeFileSync(`${plugin}/src/bundle.generated.ts`,
  '// Written by scripts/release.sh for this release.\n' +
  'export const BUNDLE: { version: string; compose: string | null; envExample: string | null } = ' +
  JSON.stringify(data, null, 2) + ';\n');
JS
  (cd "$src" && npm ci --no-audit --no-fund && npm run build)
  cp -R "$src/dist/ervisio-logs" "$dir"
  cp "$src/LICENSE-plugin-caddy" "$dir/LICENSE-plugin-caddy"
  cp "$src/node_modules/@ervisio/plugin-sdk/LICENSE" "$dir/LICENSE-plugin-sdk"
  install -m 0755 deploy/standalone/uninstall.sh "$dir/bin/uninstall.sh"
  chmod 0755 "$dir/bin/standalone.sh"
  printf '%s\n' "$version" > "$dir/bin/release.version"
  (cd "$out" && sha256sum -- ervisio-logs-"$version"-linux-*.tar.gz) > "$dir/bin/release.sha256"
  [[ -f "$dir/index.js" && -f "$dir/manifest.json" ]] || { echo "release: the plugin build left no index.js or manifest.json" >&2; exit 1; }
  [[ ! -e "$dir/manifest.sig" ]] || { echo "release: manifest.sig must not ship, the registry signs the plugin" >&2; exit 1; }
  # Ervisio installs plugins from a .tar.gz (an https:// address or a local path) and strips the top-level folder.
  # The Ervisio/plugins registry reads ervisio-logs-<v>.tar.gz and its .sha256 ("<hex>  <name>") from the latest
  # stable release, signs it with the team key and lists it. Reproducible archive: sorted, owner 0, commit time.
  mtime="$(git log -1 --format=%ct 2>/dev/null || echo 0)"
  reg="ervisio-logs-$version"
  tar -C "$work/plugin" --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$mtime" --format=gnu -cf - ervisio-logs \
    | gzip -n -9 > "$out/$reg.tar.gz"
  (cd "$out" && sha256sum -- "$reg.tar.gz" | sed 's/ \*/  /' > "$reg.tar.gz.sha256")
  cp "$out/$reg.tar.gz" "$out/$name.tar.gz"
  echo "built $reg.tar.gz (+ .sha256) and $name.tar.gz"
fi

(cd "$out" && sha256sum -- *.tar.gz > SHA256SUMS)
echo "artifacts in dist/$version (SHA256SUMS included)"
