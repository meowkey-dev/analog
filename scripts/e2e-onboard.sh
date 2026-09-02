#!/usr/bin/env bash
# Manual end-to-end check for `analog onboard` (issue #31): not in CI. The unit
# suite in cmd/analog/onboard_test.go covers the pieces; this drives the real
# surfaces the way a user does — a mock repo, a fake home, tmux windows for the
# server, the onboarding, and the onboarded agent, plus a human annotating over
# HTTP. Verifies the artifacts work, not just that they exist:
#
#   scripts/e2e-onboard.sh
#
# Mirrors the README path: build → run server → onboard in a mock project →
# act as the onboarded agent → human leaves feedback → agent reads and resolves it.
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
TMP=$(mktemp -d /tmp/analog-e2e.XXXXXX)
PORT=18787
URL=http://127.0.0.1:$PORT
SESSION=analog-e2e
LOG="$TMP/e2e.log"
fail() { echo "FAIL: $*" | tee -a "$LOG"; tmux kill-session -t $SESSION 2>/dev/null; exit 1; }
pass() { echo "ok: $*" | tee -a "$LOG"; }

cd "$REPO"

# --- the mock repo -----------------------------------------------------------------
git init -q "$TMP/mockrepo"
cd "$TMP/mockrepo"
git config user.email agent@example.com
git config user.name "E2E Agent"
cat > AGENTS.md <<EOF
# Mock repo

Reviewed on Analog. Space slug: mockdemo. The onboarded agent reads feedback here.
EOF
echo "notes" > notes.md
git add . && git commit -qm "initial mock repo"
cd "$REPO"

mkdir -p "$TMP/home" "$TMP/data"

# --- the server, in tmux -------------------------------------------------------------
tmux kill-session -t $SESSION 2>/dev/null || true
tmux new-session -d -s $SESSION -n server -c "$REPO"
tmux send-keys -t $SESSION "env ANALOG_DATA_DIR=$TMP/data ANALOG_DB=$TMP/data/analog.db \
  ANALOG_AUTH_FILE=$TMP/data/auth.json ./bin/analog-server --port $PORT; tmux wait-for -S server-done" Enter
for _ in $(seq 1 100); do
  curl -fsS $URL/api/health >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fsS $URL/api/health >/dev/null || fail "server never came up"
pass "server answers /api/health (window: server)"

# --- onboard the agent, as a user would, from the mock repo ---------------------------
# export HOME first so the shell's ~ expansion lands in the fake home, and the
# ANALOG_* env like deploy/README.md tells operators to (token add reads the
# server's auth file, so it needs to be pointed at the same one).
tmux new-window -t $SESSION -n onboard -c "$TMP/mockrepo"
tmux send-keys -t $SESSION "export HOME=$TMP/home ANALOG_DATA_DIR=$TMP/data \
  ANALOG_DB=$TMP/data/analog.db ANALOG_AUTH_FILE=$TMP/data/auth.json; $REPO/bin/analog onboard e2e-agent \
  --issue --url $URL --wrapper ~/.local/bin \
  --claude-env $TMP/mockrepo > $TMP/onboard.out 2>&1; tmux wait-for -S onboard-done" Enter
tmux wait-for onboard-done
grep -q "analog_[A-Za-z0-9_-]*" "$TMP/onboard.out" || fail "no token printed by onboard"
grep -q "skill installed" "$TMP/onboard.out" || fail "skill was not installed"
grep -q "did not authenticate" "$TMP/onboard.out" && fail "onboard flagged its own happy path"
pass "onboard minted a token, installed skill + wrapper (window: onboard)"

# --- the artifacts it left behind ------------------------------------------------------
[ -f "$TMP/home/.claude/skills/analog/SKILL.md" ] || fail "skill missing from ~/.claude/skills"
pass "skill installed into the fake home"
[ "$(stat -f %Lp "$TMP/home/.local/bin/analog-e2e-agent")" = "700" ] || fail "wrapper is not mode 700"
pass "wrapper is mode 700"
python3 - <<EOF || fail "claude-env merge wrong"
import json, pathlib
s = json.loads(pathlib.Path("$TMP/mockrepo/.claude/settings.local.json").read_text())
env = s["env"]
assert env["ANALOG_URL"] == "$URL", env
assert env["ANALOG_ACTOR"] == "e2e-agent", env
assert env["ANALOG_CONFIG"] == "/nonexistent", env
assert env["ANALOG_TOKEN"].startswith("analog_"), env
EOF
pass "mock repo's .claude/settings.local.json carries the ANALOG_* env"

# --- act as the onboarded agent, through the wrapper -------------------------------------
tmux new-window -t $SESSION -n agent -c "$TMP/mockrepo"
tmux send-keys -t $SESSION "export HOME=$TMP/home; $TMP/home/.local/bin/analog-e2e-agent whoami > $TMP/whoami.out 2>&1; tmux wait-for -S w1" Enter
tmux wait-for w1
grep -q "token   valid" "$TMP/whoami.out" || fail "wrapper whoami did not authenticate"
grep -q "e2e-agent (agent)" "$TMP/whoami.out" || fail "wrapper whoami is not e2e-agent"
pass "wrapper whoami: valid token, correct actor"

tmux send-keys -t $SESSION "$TMP/home/.local/bin/analog-e2e-agent new mockdemo \
  --title 'Mock demo' > $TMP/new.out 2>&1 && $TMP/home/.local/bin/analog-e2e-agent \
  add mockdemo --title 'Option A' --text 'lazy load the chart' --json > $TMP/add.out 2>&1; tmux wait-for -S w2" Enter
tmux wait-for w2
CARD=$(python3 -c "import json;print(json.load(open('$TMP/add.out'))['id'])")
[ -n "$CARD" ] || fail "agent could not add a card through the wrapper"
pass "onboarded agent created a space and a card ($CARD)"

# --- the human reviews, through the server binary -----------------------------------------
HUMAN_TOKEN=$(env ANALOG_DATA_DIR=$TMP/data ANALOG_DB=$TMP/data/analog.db \
  ANALOG_AUTH_FILE=$TMP/data/auth.json $REPO/bin/analog-server token add kai --kind human 2>/dev/null \
  | grep -o 'analog_[A-Za-z0-9_-]*' | head -1)
[ -n "$HUMAN_TOKEN" ] || fail "could not mint a human token"
code=$(curl -s -o "$TMP/annotation.json" -w '%{http_code}' -X POST $URL/api/spaces/mockdemo/annotations \
  -H "Authorization: Bearer $HUMAN_TOKEN" -H "X-Analog-Actor: kai" -H "X-Analog-Actor-Kind: human" \
  -H "Content-Type: application/json" \
  -d "{\"card_id\": \"$CARD\", \"body\": \"say why it is lazy\", \"motivation\": \"editing\"}")
[ "$code" = "201" ] || { cat "$TMP/annotation.json"; fail "human annotation POST returned $code"; }
pass "human left an 'editing' annotation on the card"

tmux send-keys -t $SESSION "$TMP/home/.local/bin/analog-e2e-agent feedback mockdemo \
  > $TMP/feedback.out 2>&1; tmux wait-for -S w3" Enter
tmux wait-for w3
grep -q "1 open comment" "$TMP/feedback.out" || fail "feedback did not show the comment"
grep -q "say why it is lazy" "$TMP/feedback.out" || fail "feedback did not show the body"
grep -q "editing" "$TMP/feedback.out" || fail "feedback did not show the motivation"
pass "onboarded agent reads the human's feedback through the wrapper"

# --- resolve, to prove the loop closes ------------------------------------------------------
AID=$(python3 -c "import json;print(json.load(open('$TMP/annotation.json'))['id'])")
tmux send-keys -t $SESSION "$TMP/home/.local/bin/analog-e2e-agent resolve $AID \
  --reply 'reworded' > $TMP/resolve.out 2>&1; tmux wait-for -S w4" Enter
tmux wait-for w4
grep -qi "resolved" "$TMP/resolve.out" || fail "resolve through the wrapper failed"
pass "agent resolved the annotation with a reply"

# --- the deprecated shim still forwards -------------------------------------------------------
# The shim is a Go program now, built here once: the tmux windows run with HOME
# pointed at the fake home, and a `go run` in there would populate Go caches in it.
go build -o "$TMP/onboard_agent" ./scripts/onboard_agent
tmux new-window -t $SESSION -n shim -c "$REPO"
tmux send-keys -t $SESSION "env HOME=$TMP/home $TMP/onboard_agent --bin-dir $REPO/bin \
  shim-agent --url $URL --claude-env $TMP/mockrepo --verbose > $TMP/shim.out 2> $TMP/shim.err; tmux wait-for -S w5" Enter
tmux wait-for w5
grep -q "deprecated" "$TMP/shim.err" || fail "shim did not warn"
grep -q "export ANALOG_ACTOR=shim-agent" "$TMP/shim.out" || fail "shim did not forward"
python3 - <<EOF || fail "shim did not update claude-env"
import json, pathlib
env = json.loads(pathlib.Path("$TMP/mockrepo/.claude/settings.local.json").read_text())["env"]
assert env["ANALOG_ACTOR"] == "shim-agent", env
EOF
pass "deprecated shim warns and forwards (settings now name shim-agent)"

# --- a clean screenshot for the record, then teardown ------------------------------------------
tmux capture-pane -t $SESSION:server -p > "$TMP/pane-server.txt" || true
tmux kill-session -t $SESSION 2>/dev/null || true
pkill -f "analog-server --port $PORT" 2>/dev/null || true

echo
echo "E2E PASSED — artifacts and transcript in $TMP"
