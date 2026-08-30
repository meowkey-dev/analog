#!/bin/sh
# Install the latest Analog release: detect the platform, check the download
# against the release's SHA256SUMS, and put the three binaries on PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/meowkey-dev/analog/main/scripts/install.sh | sh
#
# Re-run it to upgrade. ANALOG_INSTALL_DIR overrides the destination.
# POSIX sh throughout — it may be piped straight into sh, not bash.
set -eu

REPO=meowkey-dev/analog
LATEST=https://github.com/$REPO/releases/latest/download

say() { printf '%s\n' "$*"; }
die() { printf 'analog installer: %s\n' "$*" >&2; exit 1; }

# --- platform ----------------------------------------------------------------

case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux)  os=linux ;;
    MINGW*|MSYS*|CYGWIN*)
        die "this installer is for macOS and Linux; on Windows, download
analog-windows-amd64.zip from https://github.com/$REPO/releases" ;;
    *) die "unsupported OS: $(uname -s)" ;;
esac

case "$(uname -m)" in
    arm64|aarch64) arch=arm64 ;;
    x86_64|amd64)  arch=amd64 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
esac

asset="analog-$os-$arch.tar.gz"

# --- fetch -------------------------------------------------------------------

fetch() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --retry 3 -o "$2" "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$2" "$1"
    else
        die "need curl or wget to download the release"
    fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "fetching $asset"
fetch "$LATEST/$asset" "$tmp/$asset"
fetch "$LATEST/SHA256SUMS" "$tmp/SHA256SUMS"

# --- verify ------------------------------------------------------------------

# The release publishes "<sha256>  <name>"; check our one asset against its
# line, so a corrupt or spoofed download fails the install instead of the user.
grep "  $asset\$" "$tmp/SHA256SUMS" > "$tmp/want.txt" \
    || die "the release has no checksum for $asset"
if command -v sha256sum >/dev/null 2>&1; then
    (cd "$tmp" && sha256sum -c want.txt) >/dev/null
else
    (cd "$tmp" && shasum -a 256 -c want.txt) >/dev/null
fi

tar -xzf "$tmp/$asset" -C "$tmp"

for bin in analog analog-server analog-mcp; do
    [ -f "$tmp/$bin" ] || die "the archive did not contain $bin"
done

# --- install -----------------------------------------------------------------

# Never sudo: piping a script into sh should not surprise the user with a
# password prompt. Fall back to a home directory and tell them how to reach it.
if [ -n "${ANALOG_INSTALL_DIR:-}" ]; then
    INSTALL_DIR=$ANALOG_INSTALL_DIR
elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    INSTALL_DIR=/usr/local/bin
else
    INSTALL_DIR=$HOME/.local/bin
fi
mkdir -p "$INSTALL_DIR"

for bin in analog analog-server analog-mcp; do
    install -m 755 "$tmp/$bin" "$INSTALL_DIR/$bin"
done

case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        say
        say "note: $INSTALL_DIR is not on your PATH. Add this to your shell profile:"
        say "  export PATH=\"$INSTALL_DIR:\$PATH\""
        ;;
esac

say
say "installed analog, analog-server, analog-mcp -> $INSTALL_DIR"
say
say "next:"
say "  analog-server               # then open http://127.0.0.1:8787"
say "  analog-server seed --reset  # optional: load the demo space"
