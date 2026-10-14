#!/bin/sh
# Installs the hh binary from GitHub releases. Never edits shell rc files.
set -eu

REPO="eduardosasso/hh"
BASE="${HH_BASE_URL:-}"
if [ -z "$BASE" ]; then
  if [ -n "${HH_VERSION:-}" ]; then
    BASE="https://github.com/$REPO/releases/download/$HH_VERSION"
  else
    BASE="https://github.com/$REPO/releases/latest/download"
  fi
fi
DIR="${HH_INSTALL_DIR:-$HOME/.local/bin}"

fail() { echo "install: $*" >&2; exit 1; }

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) fail "unsupported OS: $(uname -s) (darwin and linux only)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported architecture: $(uname -m) (amd64 and arm64 only)" ;;
esac

fetch() { # url, output file
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$1" -O "$2"
  else
    fail "curl or wget is required"
  fi
}

if command -v shasum >/dev/null 2>&1; then
  sha() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
elif command -v sha256sum >/dev/null 2>&1; then
  sha() { sha256sum "$1" | cut -d ' ' -f 1; }
else
  fail "shasum or sha256sum is required to verify the download"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

name="hh_${os}_${arch}.tar.gz"
fetch "$BASE/$name" "$tmp/$name"
fetch "$BASE/checksums.txt" "$tmp/checksums.txt"

want="$(awk -v f="$name" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[ -n "$want" ] || fail "$name not listed in checksums.txt"
[ "$(sha "$tmp/$name")" = "$want" ] || fail "checksum mismatch for $name"

tar -xzf "$tmp/$name" -C "$tmp" hh
mkdir -p "$DIR"
mv "$tmp/hh" "$DIR/hh"
chmod 755 "$DIR/hh"

case ":$PATH:" in
  *":$DIR:"*) ;;
  *)
    echo "$DIR is not on your PATH. Add this line to your shell config:"
    case "${SHELL:-}" in
      */fish) echo "  fish_add_path $DIR" ;;
      *) echo "  export PATH=\"$DIR:\$PATH\"" ;;
    esac
    ;;
esac

echo "installed hh $("$DIR/hh" version) to $DIR/hh"
echo "next: hh setup"
