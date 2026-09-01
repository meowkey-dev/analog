# tests/

Two kinds of test, two different jobs.

## `tests/` — the conformance suite

The executable definition of Analog. Written against `contracts/` and `SPEC.md`
rather than against any implementation, and it reaches the server the only way a
real client can: over HTTP, to a separate process. A red build here means the
server stopped describing the contract — the question is never "which refactor
moved a symbol" but "which side is wrong", and sometimes the answer is the
fixture, which makes it an amendment (see `contracts/README.md`).

It is a **separate go module**, on purpose: `internal/` is structurally
unimportable from here, and `black_box_test.go` asserts that holds — no require,
no replace, no `github.com/meowkey-dev/analog` package anywhere in the test
binary's dependency graph. Its only dependency is the pure-go sqlite driver, for
the frozen-schema tests, whose contract ("this DDL behaves as sqlite says") no
amount of HTTP can reach.

It began in python and ran there through the go port; issue #58 ported it beside
the original under a coverage-parity regime, and the python original retired once
parity was proven. The git history has the correspondence table that gated that
retirement.

```bash
scripts/build.sh                     # the binaries the suite judges
cd tests && go test ./...
```

Point it at a different binary with `ANALOG_SERVER_BIN` — this is how the
release workflow judges the exact artifacts being shipped:

```bash
ANALOG_SERVER_BIN=/path/to/analog-server go test ./...
```

### What a server binary must provide

```
<bin> [--host H] [--port P]                     serve; /api/health answers when ready
<bin> seed --db D --media-dir M --reset         load contracts/fixtures/ into a fresh database
<bin> token add ACTOR --kind human|agent        mint a token, print it on a line of its own
```

- Configuration arrives in the environment: `ANALOG_DATA_DIR`, `ANALOG_DB`,
  `ANALOG_AUTH_FILE`.
- With no `--host`/`--port`, the server must bind the address `openapi.json`
  advertises — `127.0.0.1:8787`.
- `token add` prints the secret on a line of its own, matching
  `^analog_[A-Za-z0-9_-]+$`. That line is how the suite learns it; the rest of
  the output is for humans.
- The token store is re-read per request, so issuing a token secures a server
  that is already running.

## Everything else lives beside the code

Tests that hold implementation objects rather than a socket — the client, the CLI,
the MCP tools, the token store — are Go tests next to what they test, run with
`go test ./...` from the repo root. They were Python once, under `tests/unit/`;
the Go port rewrote them.

Every behaviour they pin that is observable over HTTP is *also* pinned in the
conformance suite (`coverage_test.go` fails if an openapi operation or a fixture
is never exercised), so a third implementation would still be fully judged.
