#!/usr/bin/env bash
# Installs or updates pve-console on a Proxmox VE node.
#
#   curl -fsSL https://raw.githubusercontent.com/CultureLinux/pve-console/main/install.sh | sudo bash
#   curl -fsSL .../install.sh | sudo bash -s -- --edge          newest build of main
#   curl -fsSL .../install.sh | sudo bash -s -- --version v0.2.0
#
# It downloads the binary of a GitHub release, checks its SHA-256 sum against the
# one published with it, and installs it (atomically) as /usr/local/sbin/pve-console.
# Run it again to update. Nothing else on the node is touched.
set -euo pipefail

REPO=${PVE_CONSOLE_REPO:-CultureLinux/pve-console}
BASE_URL=${PVE_CONSOLE_BASE_URL:-} # to test, or to use a mirror: any directory with the 2 files
ASSET=pve-console_linux_amd64
DEST_DIR=/usr/local/sbin
CHANNEL=stable
VERSION=""
DIR_GIVEN=0
DRY_RUN=0

usage() {
  cat <<'USAGE'
Usage: install.sh [--edge | --version vX.Y.Z] [--dir DIR] [--dry-run]

  (no option)       install the latest release (the stable channel)
  --edge            install the newest build of main (a pre-release, may be unfinished)
  --version vX.Y.Z  install that release
  --dir DIR         install in DIR instead of /usr/local/sbin (no root needed)
  --dry-run         show what would be downloaded and where, change nothing
  -h, --help        this help
USAGE
}

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

while (($#)); do
  case $1 in
    --edge) CHANNEL=edge; shift ;;
    --version)
      [[ $# -ge 2 ]] || die "--version needs a value"
      CHANNEL=version
      VERSION=$2
      shift 2
      ;;
    --dir)
      [[ $# -ge 2 ]] || die "--dir needs a value"
      DEST_DIR=$2
      DIR_GIVEN=1
      shift 2
      ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; die "unknown option: $1" ;;
  esac
done

if [[ $CHANNEL == version && ! $VERSION =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$ ]]; then
  die "invalid version: $VERSION (expected vX.Y.Z)"
fi

if [[ -n $BASE_URL ]]; then
  base=${BASE_URL%/}
else
  case $CHANNEL in
    stable) base="https://github.com/$REPO/releases/latest/download" ;;
    edge) base="https://github.com/$REPO/releases/download/latest" ;;
    version) base="https://github.com/$REPO/releases/download/$VERSION" ;;
  esac
fi
dest="$DEST_DIR/pve-console"

say "channel     : $CHANNEL${VERSION:+ ($VERSION)}"
say "download    : $base/$ASSET"
say "install in  : $dest"
if ((DRY_RUN)); then
  say "(dry run: nothing downloaded, nothing changed)"
  exit 0
fi

[[ $(uname -s) == Linux ]] || die "pve-console is built for Linux only"
case $(uname -m) in
  x86_64 | amd64) ;;
  *) die "pve-console is built for linux/amd64 only (this machine is $(uname -m))" ;;
esac
for tool in curl sha256sum install mktemp; do
  command -v "$tool" >/dev/null || die "missing tool: $tool"
done
if ((!DIR_GIVEN)) && [[ $EUID -ne 0 ]]; then
  die "run it as root (for example: curl ... | sudo bash), or choose a directory with --dir"
fi
if ! command -v pveversion >/dev/null; then
  say "warning: pveversion not found, this does not look like a Proxmox VE node (installing anyway)"
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/pve-console-install.XXXXXX")
cleanup() {
  rm -f -- "$tmp/$ASSET" "$tmp/SHA256SUMS"
  rmdir -- "$tmp" 2>/dev/null || true
}
trap cleanup EXIT

curl -fsSL --retry 3 -o "$tmp/$ASSET" "$base/$ASSET" || die "download failed: $base/$ASSET"
curl -fsSL --retry 3 -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" ||
  die "download failed: $base/SHA256SUMS (a binary is never installed without its sum)"

# Check the binary against the published sum. The file name in SHA256SUMS must match.
expected=$(awk -v f="$ASSET" '{ name = $2; sub(/^\*/, "", name); if (name == f) { print $1; exit } }' "$tmp/SHA256SUMS")
[[ -n $expected ]] || die "$ASSET is not listed in SHA256SUMS"
actual=$(sha256sum "$tmp/$ASSET" | awk '{ print $1 }')
if [[ $expected != "$actual" ]]; then
  die "checksum mismatch for $ASSET (expected $expected, got $actual): nothing installed"
fi
say "checksum    : OK ($actual)"

chmod 0755 "$tmp/$ASSET"
new_version=$("$tmp/$ASSET" version 2>/dev/null) || die "the downloaded binary does not run on this machine"
old_version=""
if [[ -x $dest ]]; then
  old_version=$("$dest" version 2>/dev/null || true)
fi

# Atomic replacement: write next to the destination, then rename over it.
install -d -m 0755 "$DEST_DIR"
owner=()
if [[ $EUID -eq 0 ]]; then owner=(-o root -g root); fi
install -m 0755 ${owner[@]+"${owner[@]}"} "$tmp/$ASSET" "$DEST_DIR/.pve-console.new"
mv -f -- "$DEST_DIR/.pve-console.new" "$dest"

say ""
if [[ -n $old_version && $old_version != "$new_version" ]]; then
  say "updated     : $old_version"
  say "          -> $new_version"
elif [[ -n $old_version ]]; then
  say "already up to date: $new_version"
else
  say "installed   : $new_version"
fi
say "run         : pve-console   (the terminal UI, as root)   or   pve-console tui --demo"
