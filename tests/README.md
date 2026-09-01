# tests/

Two suites with different jobs — and, while issue #58's port is in flight, two
*implementations* of the conformance harness running side by side.

## `tests/contract/` — the conformance harness (python)

The executable definition of Analog. Written against `contracts/` and `SPEC.md`
rather than against any implementation, and it reaches the server the only way a
real client can: over HTTP, to a separate process. Nothing in here imports the
server — `test_black_box.py` asserts that, so it cannot quietly stop being true.

Point it at a binary with `ANALOG_SERVER_BIN`:

```bash
ANALOG_SERVER_BIN=./bin/analog-server pytest tests/contract
```

## `tests/conformance-go/` — the same harness, in go

The port of the above (issue #58): a **separate go module**, stdlib plus the
pure-go sqlite driver for the frozen-schema tests, speaking HTTP to a spawned
server the same way. The module boundary makes the implementation unimportable,
and `black_box_test.go` asserts that holds — no require, no replace, no
`github.com/meowkey-dev/analog` package anywhere in the test binary's
dependency graph.

`CORRESPONDENCE.md` maps every python test to its go counterpart, and
`parity_test.go` enforces that both suites reference every openapi operation and
every fixture. CI runs both suites against the same binary until the python one
retires.

```bash
cd tests/conformance-go && go test ./...
```

### What a server binary must provide

```
<bin> [--host H] [--port P]                     serve; /api/health answers when ready
<bin> seed --db D --media-dir M --reset         load contracts/fixtures/ into a fresh database
<bin> token add ACTOR --kind human|agent        mint a token, print it on a line of its own
```

- Configuration arrives in the environment: `ANALOG_DATA_DIR`, `ANALOG_DB`,
  `ANALOG_AUTH_FILE`. The harness passes them explicitly, because a subprocess
  never sees `monkeypatch.setenv`.
- With no `--host`/`--port`, the server must bind the address `openapi.json`
  advertises — `127.0.0.1:8787`.
- `token add` prints the secret on a line of its own, matching
  `^analog_[A-Za-z0-9_-]+$`. That line is how the harness learns it; the rest of
  the output is for humans.
- The token store is re-read per request, so issuing a token secures a server
  that is already running.

## Everything else lives beside the code

Tests that hold implementation objects rather than a socket — the client, the CLI,
the MCP tools, the token store — are Go tests next to what they test, run with
`go test ./...`. They were Python once, under `tests/unit/`; the Go port rewrote
them, which is what that directory was always for.

Every behaviour they pin that is observable over HTTP is *also* pinned in the
conformance suites, so a third implementation would still be fully judged.
