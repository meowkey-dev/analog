# Analog

A shared canvas for one human and their agents. See [SPEC.md](SPEC.md).

`contracts/` and `server/schema.sql` are frozen (see `contracts/README.md`). Runtime
choices the contract leaves open are recorded in [DECISIONS.md](DECISIONS.md); gaps
found in the contract are in [AMENDMENTS.md](AMENDMENTS.md).

## Setup

```bash
uv venv --python 3.14 && uv pip install -e ".[dev,mcp]"
(cd web && /opt/homebrew/bin/npm install && /opt/homebrew/bin/npm run build)
```

`npm` on this machine's PATH is Bun's shim and will not run Vite — use the Homebrew
path. See DECISIONS.md.

## Run

```bash
.venv/bin/python scripts/seed.py --reset
.venv/bin/uvicorn server.main:app --host 127.0.0.1 --port 8787
```

Then open <http://127.0.0.1:8787/s/redesign>. The server serves `web/dist`, so that
is one origin with no proxy. For frontend work, `cd web && npm run dev` gives HMR on
:5173 and proxies `/api` to :8787.

`/s/redesign?fixture` renders `contracts/fixtures/` with no database behind it.

## Use it from an agent

```bash
export ANALOG_ACTOR=claude-code        # no default: an unnamed agent fails loudly
analog feedback redesign               # the important one
analog add redesign --title "Option E" --kind md --file draft.md
cat chart.svg | analog add redesign --title "Revenue" --kind svg -
analog resolve a_01... --reply "rebased axis at 0"
```

`analog --help` lists the rest. For MCP, point your client at
`mcp_server/server.py` (stdio) with `ANALOG_ACTOR` set. `skill/analog/SKILL.md`
teaches either surface the workflow — copy it into `.claude/skills/`.

## Test

```bash
.venv/bin/python -m pytest              # 371 tests
(cd web && /opt/homebrew/bin/npm run build)   # tsc --noEmit + vite build
```

`tests/contract/` is written against `contracts/` and SPEC.md rather than against the
implementation; `tests/unit/` covers the client, CLI and MCP surfaces.

## The §7 acceptance demo

With the server running:

```bash
.venv/bin/python scripts/demo.py agent-a        # 1-3: MCP over stdio, as claude-code
#   4: in the browser — drag cards, delete Option D, pin "y-axis starts at 40, fix",
#      link Option A -> Option C "depends on"
.venv/bin/python scripts/demo.py agent-b        # 5-6: the CLI, as codex
.venv/bin/python scripts/demo.py agent-a-again  # 7:  independent cursors
```

Step 7 asserts the three things the design exists for: every delta Agent A receives
came from the human, Agent B's writes are not replayed to Agent A as feedback, and
the annotation Agent B resolved is gone.

## Layout

    server/      FastAPI + SQLite. Every rule lives in store.py.
    client/      typed HTTP client over the API
    cli/         `analog`
    mcp_server/  FastMCP stdio server (not `mcp/` — it would shadow the `mcp` package)
    skill/       the agent skill
    web/         React + Vite
    scripts/     seed.py, demo.py
    tests/       contract/ and unit/
