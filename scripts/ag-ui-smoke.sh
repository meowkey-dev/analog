#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

CHROME=${ANALOG_CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}
if [ ! -x "$CHROME" ]; then
    echo "Chrome not found at $CHROME; set ANALOG_CHROME to a Chrome/Chromium binary" >&2
    exit 2
fi

TIMEOUT_BIN=""
for candidate in timeout gtimeout; do
    if command -v "$candidate" >/dev/null 2>&1 &&
       "$candidate" --help 2>&1 | rg -q -- '--kill-after'; then
        TIMEOUT_BIN=$(command -v "$candidate")
        break
    fi
done
if [ -z "$TIMEOUT_BIN" ]; then
    echo "GNU timeout is required to bound Chrome; install coreutils or set up timeout" >&2
    exit 2
fi

TMP=$(mktemp -d)
SIDECAR_PID=""
cleanup() {
    if [ -n "$SIDECAR_PID" ]; then
        kill "$SIDECAR_PID" 2>/dev/null || true
        wait "$SIDECAR_PID" 2>/dev/null || true
    fi
    rm -rf "$TMP"
}
trap cleanup EXIT INT TERM

CGO_ENABLED=0 go build -o "$TMP/ag-ui-sidecar" ./examples/ag-ui
"$TMP/ag-ui-sidecar" >"$TMP/sidecar.log" 2>&1 &
SIDECAR_PID=$!
READY=0
for _ in $(seq 1 120); do
    if curl -fsS http://127.0.0.1:9191/health >/dev/null 2>&1; then
        READY=1
        break
    fi
    if ! kill -0 "$SIDECAR_PID" 2>/dev/null; then
        break
    fi
    sleep 0.1
done
if [ "$READY" -ne 1 ]; then
    echo "AG-UI sidecar did not become ready" >&2
    sed -n '1,120p' "$TMP/sidecar.log" >&2
    exit 1
fi

CHROME_STATUS=0
"$TIMEOUT_BIN" \
    --signal=TERM \
    --kill-after=3s \
    15s \
    "$CHROME" \
    --headless \
    --disable-gpu \
    --disable-background-networking \
    --disable-breakpad \
    --disable-component-update \
    --disable-default-apps \
    --disable-extensions \
    --disable-sync \
    --no-first-run \
    --no-default-browser-check \
    --user-data-dir="$TMP/chrome" \
    --virtual-time-budget=5000 \
    --dump-dom http://127.0.0.1:9191/ >"$TMP/dom.html" 2>"$TMP/chrome.log" || CHROME_STATUS=$?

DOM_PASS=0
if [ -f "$TMP/dom.html" ] && rg -q "AG-UI smoke: PASS" "$TMP/dom.html"; then
    DOM_PASS=1
fi
if [ "$CHROME_STATUS" -ne 0 ] && [ "$DOM_PASS" -eq 0 ]; then
    if [ "$CHROME_STATUS" -eq 124 ] || [ "$CHROME_STATUS" -eq 137 ] || [ "$CHROME_STATUS" -eq 143 ]; then
        echo "AG-UI browser smoke environment-blocked: Chrome did not exit within 15s" >&2
        sed -n '1,80p' "$TMP/chrome.log" >&2
        exit 2
    fi
    echo "Chrome failed before producing a passing DOM (exit $CHROME_STATUS)" >&2
    sed -n '1,80p' "$TMP/chrome.log" >&2
    exit 1
fi

if [ "$DOM_PASS" -ne 1 ]; then
    echo "browser did not consume the AG-UI SSE stream" >&2
    sed -n '1,120p' "$TMP/dom.html" >&2
    sed -n '1,120p' "$TMP/sidecar.log" >&2
    exit 1
fi
if ! rg -q 'sandbox="allow-scripts"' "$TMP/dom.html"; then
    echo "browser harness did not use the scripts-only sandbox" >&2
    exit 1
fi
if rg -q 'allow-same-origin|allow-forms' "$TMP/dom.html"; then
    echo "browser harness granted an unrelated sandbox capability" >&2
    exit 1
fi
if ! rg -q 'OPTIONS /agent origin="null"' "$TMP/sidecar.log" ||
   ! rg -q 'POST /agent origin="null"' "$TMP/sidecar.log"; then
    echo "sidecar did not observe the opaque-origin preflight and POST" >&2
    sed -n '1,120p' "$TMP/sidecar.log" >&2
    exit 1
fi

echo "AG-UI iframe smoke passed: JSON POST + streamed SSE + sandbox=allow-scripts"
