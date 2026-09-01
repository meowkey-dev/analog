#!/usr/bin/env python3
"""The SPEC §7 acceptance demo, start to finish.

    python3 scripts/demo.py reset         # wipe the demo space, start over
    python3 scripts/demo.py agent-a       # 1-3: create the space, post cards, link them
    <human does step 4 in the browser>
    python3 scripts/demo.py agent-b       # 5-6: read feedback over the CLI, post a fix
    python3 scripts/demo.py agent-a-again # 7:  Agent A's independent cursor

And when you want more than the narrative — a smoke pass over everything else the
surface can do, no human needed. Creates and deletes its own scratch spaces:

    python3 scripts/demo.py extras

Agent A speaks MCP over stdio to the `analog-mcp` binary — a real subprocess and a
real protocol round trip, not a function call. Agent B shells out to `analog`. They
are different actors with independent cursors, which is the whole point of step 7.

Needs nothing but a Python interpreter: the MCP transport is newline-delimited
JSON-RPC, which is about thirty lines below rather than a dependency.

Binaries come from ./bin, or $ANALOG_BIN_DIR.
"""

from __future__ import annotations

import base64
import json
import os
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
BIN = Path(os.environ.get("ANALOG_BIN_DIR", REPO_ROOT / "bin"))

SLUG = "demo"
URL = os.environ.get("ANALOG_URL", "http://127.0.0.1:8787")
AGENT_A = "claude-code"
AGENT_B = "codex"


def heading(text: str) -> None:
    print(f"\n\033[1m{text}\033[0m")


def show(label: str, value) -> None:
    rendered = value if isinstance(value, str) else json.dumps(value, ensure_ascii=False)
    print(f"  {label}: {rendered}")


def binary(name: str) -> Path:
    path = BIN / name
    if not path.is_file():
        raise SystemExit(f"no {path} — build the binaries first:  scripts/build.sh")
    return path


def agent_env(actor: str) -> dict[str, str]:
    return {**os.environ, "ANALOG_URL": URL, "ANALOG_ACTOR": actor,
            "ANALOG_ACTOR_KIND": "agent",
            # Never let a ~/.analog.toml belonging to the operator decide who the
            # demo's agents are.
            "ANALOG_CONFIG": "/nonexistent"}


# --- Agent A: MCP over stdio -------------------------------------------------

class MCP:
    """The smallest MCP stdio client that can drive ten tools."""

    def __init__(self, actor: str):
        self.proc = subprocess.Popen(
            [str(binary("analog-mcp"))], env=agent_env(actor),
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True, bufsize=1)
        self.next_id = 0

    def __enter__(self) -> "MCP":
        self.call("initialize", {
            "protocolVersion": "2024-11-05", "capabilities": {},
            "clientInfo": {"name": "analog-demo", "version": "0"}})
        self.notify("notifications/initialized")
        return self

    def __exit__(self, *exc) -> None:
        self.proc.stdin.close()
        self.proc.wait(timeout=5)

    def _send(self, message: dict) -> None:
        self.proc.stdin.write(json.dumps(message) + "\n")
        self.proc.stdin.flush()

    def notify(self, method: str, params: dict | None = None) -> None:
        self._send({"jsonrpc": "2.0", "method": method, "params": params or {}})

    def call(self, method: str, params: dict | None = None):
        self.next_id += 1
        self._send({"jsonrpc": "2.0", "id": self.next_id, "method": method,
                    "params": params or {}})
        line = self.proc.stdout.readline()
        if not line:
            raise SystemExit("analog-mcp closed the connection")
        message = json.loads(line)
        if "error" in message:
            raise SystemExit(f"{method} failed: {message['error']['message']}")
        return message["result"]

    def tools(self) -> list[str]:
        return sorted(t["name"] for t in self.call("tools/list")["tools"])

    def tool(self, name: str, **arguments):
        """One tool call, unwrapped from the MCP envelope."""
        result = self.call("tools/call", {"name": name, "arguments": arguments})
        if result.get("isError"):
            raise SystemExit(f"{name}: {result['content'][0]['text']}")
        data = result["structuredContent"]
        return data.get("result", data) if isinstance(data, dict) else data


CHART_HTML = """<!doctype html><meta charset="utf-8">
<style>body{font:13px system-ui;margin:12px}
.bar{height:20px;background:#4a7;margin:6px 0;border-radius:3px}
.axis{border-top:1px solid #999;margin-top:10px;padding-top:4px;color:#666;font-size:11px}</style>
<h3>Render time by option (ms)</h3>
<div>A <div class="bar" style="width:62%"></div></div>
<div>B <div class="bar" style="width:31%"></div></div>
<div>C <div class="bar" style="width:78%"></div></div>
<div class="axis">y-axis starts at 40</div>
<script>document.title = "chart";</script>"""

OPTIONS = [
    ("Option A", "## Option A\n\nShip the existing renderer, add lazy loading.\n\n"
                 "- lowest risk\n- no new deps"),
    ("Option B", "## Option B\n\nSwap to a virtualised list.\n\n"
                 "- best at scale\n- rewrite of the scroll logic"),
    ("Option C", "## Option C\n\nPaginate server-side.\n\n"
                 "- simplest client\n- extra round trips"),
    ("Option D", "## Option D\n\nCache everything in IndexedDB.\n\n"
                 "- fast on repeat visits\n- cache invalidation is a project"),
]


def agent_a() -> None:
    with MCP(AGENT_A) as mcp:
        tools = mcp.tools()
        heading("Agent A — MCP over stdio")
        show("tools", tools)
        assert len(tools) == 10, tools

        heading("1. create_space('demo')")
        try:
            show("space", mcp.tool("create_space", slug=SLUG, title="List perf — demo")["slug"])
        except SystemExit:
            raise SystemExit(f"space '{SLUG}' already exists —"
                             "  python3 scripts/demo.py reset")

        heading("2. add_cards — 4 options + 1 html chart")
        cards = mcp.tool("add_cards", slug=SLUG, cards=[
            {"title": t, "content": c, "kind": "md"} for t, c in OPTIONS
        ] + [{"title": "Render time by option", "content": CHART_HTML,
              "kind": "html", "width": 460, "height": 320}])
        by_title = {c["sp_title"]: c["id"] for c in cards}
        for card in cards:
            show(card["sp_title"], f"{card['id']}  ({card['sp_kind']}) at "
                                   f"({card['x']}, {card['y']})")

        heading("3. link_cards — Option B contradicts Option D")
        edge = mcp.tool("link_cards", slug=SLUG, from_card=by_title["Option B"],
                        to_card=by_title["Option D"], label="contradicts")
        show("edge", f"{edge['id']}  {edge['label']}")

        (REPO_ROOT / "demo" / "ids.json").write_text(json.dumps(by_title, indent=2))

    print(f"\nNow do step 4 in the browser: {URL}/s/{SLUG}")
    print("  drag cards · delete Option D · pin a comment on the chart reading")
    print("  \"y-axis starts at 40, fix\" · link Option A -> Option C \"depends on\"")


# --- Agent B: the CLI --------------------------------------------------------

def analog(*args: str, stdin: str | None = None, actor: str = AGENT_B) -> str:
    proc = subprocess.run([str(binary("analog")), *args], input=stdin,
                          capture_output=True, text=True, env=agent_env(actor))
    if proc.returncode != 0:
        raise SystemExit(f"analog {' '.join(args)} failed ({proc.returncode}):\n{proc.stderr}")
    return proc.stdout


def agent_b() -> None:
    heading("5. Agent B (a different agent, over the CLI): analog feedback demo")
    report = analog("feedback", SLUG)
    print("\n".join("  " + line for line in report.rstrip().splitlines()) or "  (nothing)")
    if not report.strip():
        raise SystemExit("Agent B saw nothing — has step 4 been done in the browser?")

    heading("6. Agent B: post the fixed chart, then resolve the comment")
    fixed = (REPO_ROOT / "demo" / "fixed.svg").read_text()
    added = analog("add", SLUG, "-", "--title", "Render time by option (fixed)",
                   "--kind", "svg", stdin=fixed)
    show("cat fixed.svg | analog add demo --kind svg -", added.strip())

    open_annotations = json.loads(analog("comments", SLUG, "--json"))
    if not open_annotations:
        raise SystemExit("no open comment to resolve — was one pinned in step 4?")
    target = open_annotations[0]["id"]
    show("resolving", f"{target}  {open_annotations[0]['body']!r}")
    show("analog resolve", analog("resolve", target, "--reply",
                                  "rebased axis at 0").strip())


# --- Agent A again -----------------------------------------------------------

def agent_a_again() -> None:
    with MCP(AGENT_A) as mcp:
        heading("7. Agent A calls get_feedback again — its own cursor, not Agent B's")
        feedback = mcp.tool("get_feedback", slug=SLUG)
        show("summary", feedback["summary"])
        for bucket in ("annotations", "cards_edited", "cards_deleted", "cards_moved",
                       "links_added", "links_removed"):
            if feedback[bucket]:
                show(bucket, feedback[bucket])

        actors = {row.get("actor") for bucket in
                  ("cards_edited", "cards_deleted", "cards_moved", "links_added",
                   "links_removed") for row in feedback[bucket]}
        heading("checks")
        print(f"  every delta came from the human, not from an agent : {actors == {'human'}}  {actors or '{}'}")
        print(f"  Agent B's card was not replayed as an edit          : "
              f"{not any('fixed' in (c.get('title') or '') for c in feedback['cards_edited'])}")
        print(f"  the comment Agent B resolved is gone               : "
              f"{feedback['annotations'] == []}")


# --- Extras: the rest of the surface, no human needed -------------------------
#
# The §7 narrative is a story; this is an inventory. Everything MCP or the CLI can
# do that the story does not touch gets one honest round trip here, on scratch
# spaces that are deleted again at the end. The one thing it cannot do is be the
# human in a browser, so the human's part here — pinning an annotation — goes
# over raw HTTP as the human actor, clearly labelled.

LAB = "demo-lab"
ROUNDTRIP = "demo-lab-roundtrip"

# A 1×1 PNG, so `analog upload` has a real image without a fixture on disk.
PIXEL_PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")


def try_rm_space(slug: str, actor: str) -> None:
    # A 404 is fine — it just means there was nothing to clean up.
    subprocess.run([str(binary("analog")), "rm-space", slug, "--yes"],
                   capture_output=True, env=agent_env(actor))


def human_post(path: str, body: dict) -> dict:
    """One write as the human, over raw HTTP: what the browser does in step 4."""
    query = "actor=human&actor_kind=human"
    req = urllib.request.Request(f"{URL}/api{path}?{query}",
                                 data=json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read())


def expect_conflict(run, label: str) -> None:
    """A 409 is a feature: surfaced to the caller, never auto-resolved (SPEC §3)."""
    try:
        run()
    except SystemExit as e:
        show(label, str(e).replace("\n", " ").strip())
        return
    raise SystemExit(f"{label}: expected a conflict, got success")


def extras() -> None:
    try_rm_space(LAB, AGENT_A)
    try_rm_space(ROUNDTRIP, AGENT_A)

    heading("E0. analog whoami — the first diagnostic when something 401s or 403s")
    print("\n".join("  " + line for line in analog("whoami", actor=AGENT_B).rstrip().splitlines()))

    with MCP(AGENT_A) as a, MCP(AGENT_B) as b:
        heading("E1. list_spaces, create_space, read_space (MCP)")
        show("spaces", [s["slug"] for s in a.tool("list_spaces")])
        a.tool("create_space", slug=LAB, title="Extras lab")
        read = a.tool("read_space", slug=LAB)
        show("lab", f"{len(read['nodes'])} nodes, {len(read['edges'])} edges, "
                    f"{len(read['annotations'])} open annotations")

        heading("E2. add_cards: explicit x/y, auto-layout, and the plain kind")
        cards = a.tool("add_cards", slug=LAB, cards=[
            {"title": "Pinned at (40, 40)", "kind": "plain",
             "content": "coordinates chosen by the agent", "x": 40, "y": 40},
            {"title": "Placed by the server",
             "content": "no x/y — auto-layout decides"},
        ])
        pinned, placed = cards[0], cards[1]
        for c in cards:
            show(c["sp_title"], f"({c['x']}, {c['y']})  rev {c['sp_rev']}")

        heading("E3. update_card: if_match honoured, a stale one must 409")
        upd = a.tool("update_card", slug=LAB, card_id=pinned["id"],
                     patch={"text": "rewritten"}, if_match=pinned["sp_rev"])
        show("fresh if_match", f"rev {pinned['sp_rev']} -> {upd['sp_rev']}")
        expect_conflict(lambda: a.tool("update_card", slug=LAB, card_id=pinned["id"],
                                       patch={"text": "lost write"}, if_match=pinned["sp_rev"]),
                        "stale if_match")

        heading("E4. delete_card — the agent's own card, so this is allowed")
        a.tool("delete_card", slug=LAB, card_id=pinned["id"])
        show("deleted", pinned["id"])

        heading("E5. await_feedback — a resident agent blocks, and wakes on a comment")
        show("B consumes backlog", b.tool("get_feedback", slug=LAB)["summary"] or "(nothing)")
        woke = {}
        def resident():
            woke["fb"] = b.tool("await_feedback", slug=LAB, timeout_s=30, poll_s=1)
        thread = threading.Thread(target=resident)
        thread.start()
        time.sleep(1.0)
        # The one browser gesture this script cannot make: a rect pin with an
        # instruction in it, exactly as step 4's human would leave.
        note = human_post(f"/spaces/{LAB}/annotations", {
            "card_id": placed["id"],
            "selector": {"type": "rect", "x": 0.1, "y": 0.2, "w": 0.3, "h": 0.25},
            "body": "> tighten this\n\nthe prose rambles", "motivation": "editing"})
        show("human pinned", f"{note['id']} on {placed['sp_title']}  (rect selector)")
        thread.join(timeout=35)
        if thread.is_alive() or "rambles" not in json.dumps(woke.get("fb", {})):
            raise SystemExit("await_feedback never woke on the annotation")
        stale0 = woke["fb"]["annotations"][0]
        show("B woke to", f"{stale0['body']!r}  stale={stale0['stale']}")

        heading("E6. the rewrite that makes the pin stale, seen by the resident")
        a.tool("update_card", slug=LAB, card_id=placed["id"],
               patch={"text": "tightened prose"})
        fb = b.tool("get_feedback", slug=LAB)
        show("cards_edited", fb["cards_edited"])
        stale1 = fb["annotations"][0]
        if not stale1["stale"]:
            raise SystemExit("annotation should be stale after a content rewrite")
        show("stale flip", f"card_rev {stale1['card_rev']} < sp_rev now — stale={stale1['stale']}")

        heading("E7. resolve_annotation (MCP), with the reply the human reads")
        show("resolved", b.tool("resolve_annotation", annotation_id=stale1["id"],
                                slug=LAB, reply="tightened — see the new rev")["id"])

        heading("E8. CLI media: upload makes a JSON Canvas file node")
        with tempfile.NamedTemporaryFile(suffix=".png", delete=False) as f:
            f.write(PIXEL_PNG)
            png = f.name
        file_card = json.loads(analog("upload", LAB, png, "--json"))
        show("file node", f"{file_card['id']}  type={file_card['type']}  {file_card['file']}")

        heading("E9. link, unlink — and one pair that cancels inside one window")
        show("linked", json.loads(analog("link", LAB, file_card["id"], placed["id"],
                                         "--label", "illustrates", "--json"))["id"])
        fb = a.tool("get_feedback", slug=LAB)
        if not fb["links_added"] or fb["annotations"]:
            raise SystemExit(f"expected A to see B's link and no open comments: {fb}")
        show("A saw links_added", [l["label"] for l in fb["links_added"]])
        edge_id = fb["links_added"][0]["id"]
        analog("unlink", LAB, edge_id)
        fb = a.tool("get_feedback", slug=LAB)
        if not fb["links_removed"]:
            raise SystemExit("A should have seen the link removal")
        show("A saw links_removed", edge_id)
        # Created and removed before A looked again: neither bucket (DECISIONS.md).
        added = json.loads(analog("link", LAB, placed["id"], file_card["id"],
                                  "--label", "noise", "--json"))["id"]
        analog("unlink", LAB, added)
        fb = a.tool("get_feedback", slug=LAB)
        if fb["links_added"] or fb["links_removed"]:
            raise SystemExit("a link created and removed in one window shows in neither")
        print("  created+removed between A's reads         : in neither bucket  (correct)")

        heading("E10. export → import into a branch-mode space (Obsidian round trip)")
        with tempfile.NamedTemporaryFile(suffix=".canvas", delete=False, mode="w") as f:
            f.write(analog("export", LAB))
            canvas_path = f.name
        a.tool("create_space", slug=ROUNDTRIP, title="Extras roundtrip",
               revision_mode="branch")
        id_map = json.loads(analog("import", ROUNDTRIP, "--file", canvas_path,
                                   "--json"))["id_map"]
        show("id_map", f"{len(id_map)} cards remapped (clients never choose ids)")
        old = next(iter(id_map.values()))
        branched = a.tool("update_card", slug=ROUNDTRIP, card_id=old,
                          patch={"text": "revised in branch mode"})
        if branched["id"] == old:
            raise SystemExit("branch mode should return a NEW card")
        edges = a.tool("read_space", slug=ROUNDTRIP)["edges"]
        show("branch", f"{old} superseded by {branched['id']}  "
                      f"auto-link {edges[0]['label']!r}")

        heading("E11. feedback --since 0 --peek — replay without advancing the cursor")
        replay = analog("feedback", LAB, "--since", "0", "--peek")
        again = analog("feedback", LAB, "--since", "0", "--peek")
        if replay != again:
            raise SystemExit("--peek must not advance the cursor")
        print("  identical twice, cursor untouched — silence still means nothing changed")

        heading("E12. events --watch rides the SSE stream")
        # Start the watcher at the current cursor, so the backlog is empty and any
        # event it prints can only have arrived live.
        cursor = json.loads(analog("events", LAB, "--json"))["cursor"]
        watcher = subprocess.Popen(
            [str(binary("analog")), "events", LAB, "--since", str(cursor), "--watch"],
            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True,
            env=agent_env(AGENT_B))
        time.sleep(1.0)
        blip = json.loads(analog("link", LAB, file_card["id"], placed["id"],
                                 "--label", "sse blip", "--json"))["id"]
        analog("unlink", LAB, blip)
        try:
            out, _ = watcher.communicate(timeout=8)
        except subprocess.TimeoutExpired:
            watcher.kill()
            out, _ = watcher.communicate()
        seen = [line for line in out.splitlines() if "link.created" in line]
        if not seen:
            raise SystemExit("events --watch never saw the link event — SSE is broken?")
        show("arrived live", seen[-1].strip()[:80])

    heading("cleanup")
    try_rm_space(LAB, AGENT_A)
    try_rm_space(ROUNDTRIP, AGENT_A)
    print(f"  {LAB} and {ROUNDTRIP} deleted — the narrative's '{SLUG}' space is untouched")


def main() -> int:
    step = sys.argv[1] if len(sys.argv) > 1 else "all"
    if step == "agent-a":
        agent_a()
    elif step == "agent-b":
        agent_b()
    elif step == "agent-a-again":
        agent_a_again()
    elif step == "extras":
        extras()
    elif step == "reset":
        try_rm_space(SLUG, AGENT_A)
        print(f"'{SLUG}' deleted (a 404 above means it was already gone)")
    else:
        print(__doc__)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
