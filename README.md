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
.venv/bin/python -m server
```

Then open <http://127.0.0.1:8787/s/redesign>. The server serves `web/dist`, so that
is one origin with no proxy. For frontend work, `cd web && npm run dev` gives HMR on
:5173 and proxies `/api` to :8787.

`/s/redesign?fixture` renders `contracts/fixtures/` with no database behind it.

## Running it somewhere else

On loopback with no tokens Analog is open, and that stays the default. The moment you
bind anything else it refuses to start until a token exists, because an
unauthenticated Analog on a network is world-writable.

```bash
.venv/bin/analog token add kai --kind human          # on the server; shown once
.venv/bin/analog token add claude-code --kind agent
.venv/bin/python -m server --host 0.0.0.0
```

A token identifies **exactly one actor**, and the server takes `actor` from it. A
client claiming a name its token does not hold gets a `403`, so what the event log
says about who did what is true rather than asserted. `analog token list` and
`analog token revoke <actor>` manage them; reissuing revokes the previous one.

From a client:

```bash
.venv/bin/analog login https://analog.example.com --token analog_...
```

That writes `~/.analog.toml` (mode 600) and learns your actor from the server.
`analog whoami` says which server you are talking to and who it thinks you are — the
first thing to run when something returns 401 or 403. Agents can set `ANALOG_URL` /
`ANALOG_ACTOR` / `ANALOG_TOKEN` instead. Exit code **3** always means an auth
problem.

The web UI asks for a token on first load and keeps it in `localStorage`.
Deliberately **not** a cookie: a card's sandboxed iframe cannot set an
`Authorization` header, so agent-authored HTML has no ambient credential to ride —
which is the concern SPEC §8 raised about this app touching a network.

## Desktop app

`app/` is a Tauri 2 shell around the same web bundle. It has no API client of its own
— it loads the same code the server serves, so there is no second copy of the auth
rules to drift out of sync. What it adds is a connection screen, because unlike a
browser it has no origin to inherit.

```bash
cd app && npm install
npm run dev          # or: npm run build -> app/src-tauri/target/release/bundle/
```

The Rust toolchain is pinned in `app/src-tauri/rust-toolchain.toml` (Tauri 2 needs
≥1.88) rather than taking over your `rustup default`.

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
