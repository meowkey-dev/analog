# Analog

A shared canvas for one human and their agents. See [SPEC.md](SPEC.md).

`contracts/` and `analog/server/schema.sql` are frozen (see `contracts/README.md`). Runtime
choices the contract leaves open are recorded in [DECISIONS.md](DECISIONS.md); gaps
found in the contract are in [AMENDMENTS.md](AMENDMENTS.md).

## Setup

```bash
uv venv --python 3.14 && uv pip install -e ".[dev,mcp]"
(cd web && npm install && npm run build)
```

If `npm --version` reports something implausible (a Bun or Volta shim, say), point at
a real Node install — Vite 8 will not run under a shim.

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

There is one, and it is a separate commercial product: a local sidecar that runs its
own server, so you never see a venv or a port. It talks to this code over the HTTP
API in `contracts/` like any other client — it has no private fork of the server.

You do not need it. `python -m server` plus a browser is the whole thing, which is
what the rest of this README documents.

## Giving an agent access

Three things have to line up: a **token** (the server decides who the agent is), a
**surface** — MCP or the CLI — carrying that token, and the **skill**, which teaches
the workflow the API cannot. One command sets up all three:

```bash
.venv/bin/python scripts/onboard_agent.py claude-code --issue --url http://127.0.0.1:8787 --skill-into ~/.claude/skills --print-mcp
```

`--issue` mints the token, so it has to run on the server host; everything else runs
wherever the agent does. Drop `--issue` and pass `--token` if you already have one.

### MCP

Ten tools over stdio (SPEC §4.1). The command above prints this filled in:

```bash
claude mcp add analog -e ANALOG_URL=http://127.0.0.1:8787 -e ANALOG_ACTOR=claude-code -e ANALOG_ACTOR_KIND=agent -e ANALOG_TOKEN=analog_... -- /path/to/.venv/bin/analog-mcp
```

Add `--scope user` to make it available in every project instead of just the current
one. Check it with `claude mcp get analog`; a healthy server reports `✔ Connected`.

The tools are `list_spaces`, `create_space`, `read_space`, `add_cards`,
`update_card`, `delete_card`, `link_cards`, `get_feedback`, `resolve_annotation`,
and `await_feedback` — the last one blocks until you do something, for a resident
agent.

### CLI

For an agent that has a shell but no MCP config — CI steps, `bash` tool calls, you:

```bash
export ANALOG_URL=http://127.0.0.1:8787
export ANALOG_ACTOR=claude-code        # no default: an unnamed agent fails loudly
export ANALOG_TOKEN=analog_...         # only if the server has tokens
analog whoami                          # who the server thinks you are
analog feedback redesign               # the important one
analog add redesign --title "Option E" --kind md --file draft.md
cat chart.svg | analog add redesign --title "Revenue" --kind svg -
analog resolve a_01... --reply "rebased axis at 0"
```

`analog --help` lists the rest. Every read command takes `--json`. Failure is always
non-zero, and exit **3** specifically means auth.

### An agent that is already running

MCP config and skills are both read when a session starts, so neither reaches an
agent mid-session. A command on disk does:

```bash
.venv/bin/python scripts/onboard_agent.py claude-code --issue --url https://analog.example.com --wrapper
```

That writes `~/.local/bin/analog-claude-code`, a one-line shell wrapper with the URL,
actor and token baked in. Tell the running agent to use it and it works immediately —
no restart, no exports:

```
analog-claude-code whoami
analog-claude-code feedback <slug>
```

Then paste `skill/analog/SKILL.md` into the conversation, or tell it to read the
file. That is the workflow half, and it matters more than the wiring.

It sidesteps a trap worth knowing about: `analog login` writes `~/.analog.toml` for
the **user**, so an agent running as you would inherit your identity and post under
your name. The wrapper pins its own actor and ignores that file. It contains a token,
so it is mode 700 and lives outside the repo.

### The skill, without MCP

The skill teaches the CLI, so it needs the CLI configured — it is documentation, not
credentials. Three pieces:

```bash
ln -sf "$PWD/.venv/bin/analog" ~/.local/bin/analog     # `analog` on PATH, as the skill writes it
.venv/bin/python scripts/onboard_agent.py claude-code --url https://analog.example.com --token analog_... --skill-into ~/.claude/skills --claude-env ~/code/that-project
```

`--claude-env` merges `ANALOG_URL` / `ANALOG_ACTOR` / `ANALOG_ACTOR_KIND` /
`ANALOG_TOKEN` into that project's `.claude/settings.local.json`, which Claude Code
applies to its Bash tool calls. It merges rather than overwrites, and
`settings.local.json` is the gitignored one — the token stays out of git. It also
sets `ANALOG_CONFIG=/nonexistent` so a `~/.analog.toml` belonging to *you* cannot
make the agent post under your name.

Then **restart the agent**: both the skill listing and `settings.local.json` are read
at session start.

Mint the token on whichever machine runs the server — `analog token add claude-code
--kind agent` — since that is where the token store lives.

This is the half that matters. MCP and the CLI give an agent the operations; the
skill teaches the conventions that decide whether the tool stays usable — read
feedback *first*, one idea per card, always label links, don't resolve what you
haven't acted on, don't rearrange the human's canvas. An agent with the tools and no
skill will use Analog as a dumping ground.

## Test

```bash
.venv/bin/python -m pytest              # 445 tests
(cd web && npm run build)               # tsc --noEmit + vite build
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

    analog/
      server/    FastAPI + SQLite. Every rule lives in store.py.
      client/    typed HTTP client over the API
      cli/       `analog`
      mcp_server/  FastMCP stdio server
    skill/       the agent skill
    web/         React + Vite
    deploy/      systemd unit and Caddyfile for running it on a host
    scripts/     seed.py, demo.py, onboard_agent.py, build_dist.py
    tests/       contract/ and unit/

## CI

`.github/workflows/test.yml` runs the contract and unit suites on Python 3.11 and
3.13, plus the web typecheck.

## License

[Apache-2.0](LICENSE). Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md);
there is no CLA, just a `Signed-off-by` line.

`contracts/` and `analog/server/schema.sql` are the wire format and are changed only through
the amendment process, which is the one thing worth reading before opening a pull
request.
