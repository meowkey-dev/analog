# Analog

A shared canvas for one human and their agents. See [SPEC.md](SPEC.md).

`contracts/` and `internal/store/schema.sql` are frozen (see `contracts/README.md`).
Runtime choices the contract leaves open are recorded in [DECISIONS.md](DECISIONS.md);
gaps found in the contract are in [AMENDMENTS.md](AMENDMENTS.md).

## Install

Three binaries — `analog-server`, `analog`, `analog-mcp` — with no runtime to install
beside them. The web UI is inside `analog-server`.

```bash
brew install meowkey-dev/tap/analog
```

Or the installer script:

```bash
curl -fsSL https://raw.githubusercontent.com/meowkey-dev/analog/main/scripts/install.sh | sh
```

That detects your platform, checks the download against the release's `SHA256SUMS`,
and installs the three binaries to `/usr/local/bin` — or `~/.local/bin` when that
would need sudo (`ANALOG_INSTALL_DIR` overrides). Re-run it to upgrade. Or unpack a
release by hand:

```bash
curl -L https://github.com/meowkey-dev/analog/releases/latest/download/analog-darwin-arm64.tar.gz | tar xz
./analog-server
```

Then open <http://127.0.0.1:8787>.

## Run

Start the server:

```bash
analog-server
```

Then open <http://127.0.0.1:8787>. The server serves its embedded bundle, so the UI and
API use one origin with no proxy. The database, media and tokens live in `./data`, or
wherever `ANALOG_DATA_DIR` points.

## Onboard an agent

Once the server is running, give an agent an identity, the wiring it needs, and the
skill that teaches the workflow the API cannot. For Claude Code working in a project
at `~/src/my-project`, one command sets all three up:

```bash
analog onboard claude-code \
  --issue \
  --url http://127.0.0.1:8787 \
  --claude-env ~/src/my-project
```

That mints a token, installs the skill into `~/.claude/skills`, and merges
`ANALOG_URL`, `ANALOG_ACTOR`, `ANALOG_ACTOR_KIND`, `ANALOG_TOKEN` when supplied, and
`ANALOG_CONFIG=/nonexistent` into the project's `.claude/settings.local.json`. The
skill install is the `--config-via skill` default: rerunning skips an existing
user-level skill, and `--config-dir DIR` installs somewhere else and overwrites —
the update path. Add `--verbose` to also print the wiring instructions, the shell
exports and the `claude mcp add` command (the full-form output of earlier releases)
without changing what gets installed. `--config-via mcp` prints just the MCP command
and installs no skill; `--config-via skip` wires nothing at all.

In the help output, `--claude-env string` uses `string` as a placeholder for the
project directory; it is not the literal word `string`. Use `--claude-env .` for the
current project. It merges `ANALOG_URL`, `ANALOG_ACTOR`, `ANALOG_ACTOR_KIND`,
`ANALOG_TOKEN` when supplied, and `ANALOG_CONFIG=/nonexistent` into the project's
`.claude/settings.local.json`; Claude Code reads that file when a session starts, so
restart the agent afterward. The local settings file is gitignored, which keeps the
token out of git.

`--issue` mints the token and therefore runs on the server host. If the agent is on a
different machine, mint it there first, then run the onboarding command on the agent's
machine with `--token` instead. Drop `--issue` if you already have a token. The skill
is embedded in the binary, so a release or brew install can onboard without a checkout.

### MCP

Ten tools over stdio (SPEC §4.1). To wire MCP instead of the skill, `--config-via
mcp` prints the filled-in command (add `--verbose` to see the exports too):

```bash
claude mcp add analog -e ANALOG_URL=http://127.0.0.1:8787 -e ANALOG_ACTOR=claude-code -e ANALOG_ACTOR_KIND=agent -e ANALOG_TOKEN=analog_... -- /path/to/analog-mcp
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
analog export redesign --format html > redesign.html
```

`analog --help` lists the rest. Every read command takes `--json`. Failure is always
non-zero, and exit **3** specifically means auth. `analog --version` names the
release you are on. `/api/health` reports that as `release`; `version` there is the
API contract, a different number.

### An agent that is already running

MCP config and skills are both read when a session starts, so neither reaches an
agent mid-session. A command on disk does:

```bash
analog onboard claude-code --issue --url https://analog.example.com --wrapper ~/.local/bin
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
credentials:

```bash
analog onboard claude-code --url https://analog.example.com --token analog_... --claude-env ~/code/that-project
```

`--claude-env-shared` targets the committed `.claude/settings.json` instead of the
gitignored `settings.local.json`; use it only for non-secret wiring. Keep the token
out of the shared file. If you are running from a source checkout, use `bin/analog`
instead of `analog` when the binary is not on `PATH`.

Mint the token on whichever machine runs the server — `analog-server token add
claude-code --kind agent` — since that is where the token store lives.

This is the half that matters. MCP and the CLI give an agent the operations; the
skill teaches the conventions that decide whether the tool stays usable — read
feedback *first*, one idea per card, always label links, don't resolve what you
haven't acted on, don't rearrange the human's canvas. An agent with the tools and no
skill will use Analog as a dumping ground.

## Running it somewhere else

On loopback with no tokens Analog is open, and that stays the default. The moment you
bind anything else it refuses to start until a token exists, because an
unauthenticated Analog on a network is world-writable.

```bash
analog-server token add kai --kind human          # on the server; shown once
analog-server token add claude-code --kind agent
analog-server --host 0.0.0.0
```

A token identifies **exactly one actor**, and the server takes `actor` from it. A
client claiming a name its token does not hold gets a `403`, so what the event log
says about who did what is true rather than asserted. `token list` and
`token revoke <actor>` manage them; reissuing revokes the previous one. The same
command group is on the client as `analog token ...`, for when that is what is
installed — either way it reads the server's auth file, so it runs on the server host.

From a client:

```bash
analog login https://analog.example.com --token analog_...
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

See [deploy/](deploy/README.md) for systemd and TLS.

## Desktop app

There is one, and it is a separate commercial product: a local sidecar that runs its
own server, so you never see a port. It talks to this code over the HTTP API in
`contracts/` like any other client — it has no private fork of the server.

You do not need it. `analog-server` plus a browser is the whole thing, which is what
the rest of this README documents.

## Development

The remaining sections are for contributors: building from a checkout, running the
fixture space, testing, and understanding the repository layout.

### Build from source

```bash
(cd web && npm install && npm run build)     # the bundle the server embeds
scripts/build.sh                             # -> bin/
```

If `npm --version` reports something implausible (a Bun or Volta shim, say), point at
a real Node install — Vite 8 will not run under a shim. Without a built bundle the
server still runs; it just serves the API and no UI.

`scripts/build.sh darwin/arm64 linux/amd64 windows/amd64` cross-compiles. `CGO_ENABLED=0`
throughout, so no target needs a C toolchain.

To run the seeded fixture from a checkout:

```bash
bin/analog-server seed --reset      # load contracts/fixtures/ into a fresh database
bin/analog-server
```

Then open <http://127.0.0.1:8787/s/redesign>. For frontend work, `cd web && npm run dev`
gives HMR on :5173 and proxies `/api` to :8787. `/s/redesign?fixture` renders
`contracts/fixtures/` with no database behind it.

### Test

```bash
scripts/build.sh                        # the binaries the suite judges

go test ./...                           # the Go tests beside the code
(cd tests && go test ./...)             # the conformance suite, over HTTP

(cd web && npm run build)               # tsc --noEmit + vite build
```

`tests/` is the executable definition of Analog: written against `contracts/` and
SPEC.md rather than against the implementation, and reaching the server over a
socket, so it judges any binary that answers. Point it at one with
`ANALOG_SERVER_BIN` — the release workflow does exactly that. It is a separate Go
module, so the implementation is structurally unimportable from it, and
`tests/README.md` has the contract a server binary must honour. It began in
Python (which kept the port honest) and was ported to Go under a coverage-parity
regime once the port was done ([DECISIONS.md](DECISIONS.md) has the story).

Everything that needs the implementation's own objects is a Go test beside the code.

### The §7 acceptance demo

With the server running:

```bash
go run ./scripts/demo reset         # wipe the demo space, start over
go run ./scripts/demo agent-a       # 1-3: MCP over stdio, as claude-code
#   4: in the browser — drag cards, delete Option D, pin "y-axis starts at 40, fix",
#      link Option A -> Option C "depends on"
go run ./scripts/demo agent-b       # 5-6: the CLI, as codex
go run ./scripts/demo agent-a-again # 7:  independent cursors
```

Step 7 asserts the three things the design exists for: every delta Agent A receives
came from the human, Agent B's writes are not replayed to Agent A as feedback, and
the annotation Agent B resolved is gone.

Beyond the narrative, `go run ./scripts/demo extras` is a smoke pass over
everything else — every remaining MCP tool, media upload, If-Match conflicts,
branch mode, staleness, export/import, SSE — on scratch spaces it deletes again.
No human interaction needed, so it doubles as a quick regression check after a
rebuild.

### Layout

    cmd/
      analog/          the `analog` CLI
      analog-server/   the server, plus `seed` and `token`
      analog-mcp/      MCP over stdio
    internal/
      store/           SQLite. Every rule lives here.
      api/             HTTP handlers; thin
      auth/            per-actor bearer tokens
      sse/             the event broker
      ids/             ULID with the s_/c_/l_/a_/m_ prefixes
      mcp/             the ten tools
      skill/           where the agent skill is embedded from
      web/             where the built bundle is embedded from
    client/            exported HTTP client — third parties import this
    skill/             the agent skill (also embedded in the `analog` binary)
    web/               React + Vite
    deploy/            systemd unit and Caddyfile for running it on a host
    scripts/           build.sh; the §7 demo and the deprecated onboard shim (go run ./scripts/…)
    tests/             the conformance harness

### CI

`.github/workflows/test.yml` runs `go test`, the conformance suite against a built
binary on Linux and macOS, and the web typecheck. `release.yml` cross-compiles the
matrix on a tag and checks that the server runs standalone and stays under 20 MB.

## License

[Apache-2.0](LICENSE). Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md);
there is no CLA, just a `Signed-off-by` line.

`contracts/` and `internal/store/schema.sql` are the wire format and are changed only
through the amendment process, which is the one thing worth reading before opening a
pull request.
