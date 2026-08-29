#!/usr/bin/env python3
"""The SPEC §7 acceptance demo, start to finish.

    python scripts/demo.py agent-a        # 1-3: create the space, post cards, link them
    <human does step 4 in the browser>
    python scripts/demo.py agent-b        # 5-6: read feedback over the CLI, post a fix
    python scripts/demo.py agent-a-again  # 7:  Agent A's independent cursor

Agent A speaks MCP over stdio to `analog/mcp_server/server.py` — a real subprocess and a
real protocol round trip, not a function call. Agent B shells out to the installed
`analog` binary. They are different actors with independent cursors, which is the
whole point of step 7.
"""

from __future__ import annotations

import asyncio
import json
import os
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(REPO_ROOT))

SLUG = "demo"
URL = os.environ.get("ANALOG_URL", "http://127.0.0.1:8787")
AGENT_A = "claude-code"
AGENT_B = "codex"


def heading(text: str) -> None:
    print(f"\n\033[1m{text}\033[0m")


def show(label: str, value) -> None:
    print(f"  {label}: {json.dumps(value, ensure_ascii=False) if not isinstance(value, str) else value}")


# --- Agent A: MCP over stdio -------------------------------------------------

def mcp_client():
    from fastmcp import Client
    from fastmcp.client.transports import StdioTransport

    return Client(StdioTransport(
        command=sys.executable,
        args=[str(REPO_ROOT / "analog" / "mcp_server" / "server.py")],
        env={**os.environ, "ANALOG_URL": URL, "ANALOG_ACTOR": AGENT_A,
             "ANALOG_ACTOR_KIND": "agent", "PYTHONPATH": str(REPO_ROOT)},
    ))


def unwrap(result):
    data = result.structured_content
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


async def agent_a() -> None:
    async with mcp_client() as client:
        tools = sorted(t.name for t in await client.list_tools())
        heading("Agent A — MCP over stdio")
        show("tools", tools)
        assert len(tools) == 10, tools

        heading("1. create_space('demo')")
        show("space", unwrap(await client.call_tool(
            "create_space", {"slug": SLUG, "title": "List perf — demo"}))["slug"])

        heading("2. add_cards — 4 options + 1 html chart")
        cards = unwrap(await client.call_tool("add_cards", {
            "slug": SLUG,
            "cards": [{"title": t, "content": c, "kind": "md"} for t, c in OPTIONS]
                     + [{"title": "Render time by option", "content": CHART_HTML,
                         "kind": "html", "width": 460, "height": 320}],
        }))
        by_title = {c["sp_title"]: c["id"] for c in cards}
        for card in cards:
            show(card["sp_title"], f"{card['id']}  ({card['sp_kind']}) at "
                                   f"({card['x']}, {card['y']})")

        heading("3. link_cards — Option B contradicts Option D")
        edge = unwrap(await client.call_tool("link_cards", {
            "slug": SLUG, "from_card": by_title["Option B"],
            "to_card": by_title["Option D"], "label": "contradicts"}))
        show("edge", f"{edge['id']}  {edge['label']}")

        (REPO_ROOT / "demo" / "ids.json").write_text(json.dumps(by_title, indent=2))

    print(f"\nNow do step 4 in the browser: {URL}/s/{SLUG}")
    print("  drag cards · delete Option D · pin a comment on the chart reading")
    print("  \"y-axis starts at 40, fix\" · link Option A -> Option C \"depends on\"")


# --- Agent B: the CLI --------------------------------------------------------

def analog(*args: str, stdin: str | None = None, actor: str = AGENT_B) -> str:
    env = {**os.environ, "ANALOG_URL": URL, "ANALOG_ACTOR": actor,
           "ANALOG_ACTOR_KIND": "agent"}
    proc = subprocess.run([sys.executable, "-m", "analog.cli.main", *args], input=stdin,
                          capture_output=True, text=True, env=env, cwd=REPO_ROOT)
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

async def agent_a_again() -> None:
    async with mcp_client() as client:
        heading("7. Agent A calls get_feedback again — its own cursor, not Agent B's")
        feedback = unwrap(await client.call_tool("get_feedback", {"slug": SLUG}))
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


def main() -> int:
    step = sys.argv[1] if len(sys.argv) > 1 else "all"
    if step == "agent-a":
        asyncio.run(agent_a())
    elif step == "agent-b":
        agent_b()
    elif step == "agent-a-again":
        asyncio.run(agent_a_again())
    else:
        print(__doc__)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
