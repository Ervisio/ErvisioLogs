#!/usr/bin/env bash
# Standalone helper of the Ervisio plugin (ADR 0012). Ervisio runs it as root through the plugin's admin commands:
#   standalone.sh install     download the release tarball this plugin was built for, check it, run its install.sh
#   standalone.sh uninstall   run the release's uninstall.sh: removes the service and binary, deletes no data
# The release version and the tarball checksums come from release.version and release.sha256 next to this script,
# written by scripts/release.sh. A plugin folder without them (a dev checkout) refuses to install.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
base_url="https://github.com/Ervisio/ErvisioLogs/releases/download"

die() { echo "standalone.sh: $*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "run as root"
[[ "$(uname -s)" == Linux ]] || die "Linux only"

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "unsupported CPU $(uname -m): releases exist for amd64 and arm64" ;;
esac

install_release() {
  [[ -f "$here/release.version" && -f "$here/release.sha256" ]] ||
    die "this plugin was not built by scripts/release.sh (no release.version): install a released plugin"
  local version name line
  # tmp stays global: the EXIT trap runs after this function has returned.
  version="$(tr -d '[:space:]' < "$here/release.version")"
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || die "bad version in release.version"
  name="ervisio-logs-$version-linux-$arch"
  line="$(grep -E "^[0-9a-f]{64}  $name\.tar\.gz$" "$here/release.sha256" || true)"
  [[ -n "$line" ]] || die "no checksum for $name.tar.gz in release.sha256"
  command -v systemctl >/dev/null || die "systemd is required"

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  echo "downloading $name.tar.gz"
  if command -v curl >/dev/null; then
    curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "$tmp/$name.tar.gz" "$base_url/v$version/$name.tar.gz"
  elif command -v wget >/dev/null; then
    wget -q --https-only -O "$tmp/$name.tar.gz" "$base_url/v$version/$name.tar.gz"
  else
    die "curl or wget is required"
  fi
  (cd "$tmp" && printf '%s\n' "$line" | sha256sum -c --quiet -) || die "checksum mismatch: the download was not installed"
  echo "checksum OK"
  tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
  bash "$tmp/$name/install.sh"
}

uninstall_release() {
  [[ -f "$here/uninstall.sh" ]] || die "uninstall.sh is missing from the plugin folder"
  bash "$here/uninstall.sh"
}

case "${1:-}" in
  install) install_release ;;
  uninstall) uninstall_release ;;
  *) die "usage: standalone.sh install|uninstall" ;;
esac
