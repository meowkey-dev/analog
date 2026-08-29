#!/usr/bin/env bash
# Build the three binaries, with the web bundle inside analog-server.
#
#   scripts/build.sh                     host platform, into bin/
#   scripts/build.sh darwin/arm64 ...    cross-compile the named platforms
#
# The bundle is copied into internal/web/dist and embedded, which is most of the
# point of the port: one file to ship, and `analog-server` alone serves the UI.
# CGO is off everywhere -- modernc.org/sqlite is pure Go, so a cross-build needs no
# C toolchain.
set -euo pipefail

cd "$(dirname "$0")/.."

OUT=${OUT:-bin}
BUNDLE=web/dist
EMBED=internal/web/dist

if [ -f "$BUNDLE/index.html" ]; then
    # Replace rather than merge: a stale asset from a previous build would be
    # embedded forever otherwise.
    find "$EMBED" -mindepth 1 ! -name .gitkeep -delete
    cp -R "$BUNDLE"/. "$EMBED"/
    echo "embedding $BUNDLE"
else
    echo "no $BUNDLE/index.html — building an API-only server" >&2
    echo "  (cd web && npm install && npm run build)" >&2
fi

build() {
    local goos=$1 goarch=$2 dir=$3 suffix=""
    [ "$goos" = windows ] && suffix=.exe
    mkdir -p "$dir"
    for cmd in analog analog-server analog-mcp; do
        # -trimpath so the binary does not carry this checkout's paths;
        # -s -w drops the symbol table, which is a third of the size.
        CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
            go build -trimpath -ldflags="-s -w" \
            -o "$dir/$cmd$suffix" "./cmd/$cmd"
    done
    echo "built $dir  ($(du -sh "$dir" | cut -f1))"
}

if [ $# -eq 0 ]; then
    build "$(go env GOOS)" "$(go env GOARCH)" "$OUT"
    exit 0
fi

for platform in "$@"; do
    goos=${platform%%/*}
    goarch=${platform##*/}
    build "$goos" "$goarch" "$OUT/${goos}-${goarch}"
done
