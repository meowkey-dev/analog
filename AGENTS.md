# AGENTS.md

Analog: a shared canvas for one human and their agents. One server (`analog-server`)
serves an HTTP API, a web UI (embedded in the binary), and speaks MCP over stdio
(`analog-mcp`) and CLI (`analog`). There is no runtime to install.

## Read this first

The docs in this repo are the source of truth and unusually thorough. Before
changing behavior, read them — they explain *why* everything is the way it is:

- `SPEC.md` — the build spec (product, data model, §3 API, §4.1 feedback)
- `DECISIONS.md` — runtime choices the contract leaves open (layout, feedback
  bucketing, tokens, the Go port). The fastest way to understand this codebase.
- `contracts/README.md` — what is frozen and why; amendment process
- `AMENDMENTS.md` — gaps found in the contract and how they were resolved
- `tests/README.md` — the two kinds of test and what each judges
- `CONTRIBUTING.md` — contribution rules (frozen contract, DCO sign-off, style)

## The contract is frozen

`contracts/` (especially `openapi.json` and the `fixtures/`) and
`internal/store/schema.sql` are frozen. Everything else is generated from or
tested against them. Changing one in a PR that also changes behavior will be
bounced. A contract change is an amendment: edit `openapi.json`, `schema.sql`
and the fixtures **together**, bump `info.version` **and** the `Version` const in
`internal/api/api.go` (they must match; `tests/openapi_test.go` checks),
and update `contracts/README.md`.

The seeded database is calibrated so the API reproduces the fixtures
byte-for-byte: `GET /spaces/redesign/canvas` returns `canvas.json`, and feedback
for `claude-code` returns `feedback.claude-code.since-12.json` because that
actor's cursor is seeded at 12. `tests/roundtrip_test.go`
asserts this. Do not "fix" a fixture to make a test pass — that is an amendment.

## Commands (all verified working)

```bash
go test ./...                    # Go tests beside the code; ~120 of them
gofmt -l .                       # CI enforces gofmt-clean
go vet ./...                     # CI runs this, and go test ./... -race

scripts/build.sh                 # builds bin/{analog,analog-server,analog-mcp}
                                 # cross-compiles: scripts/build.sh darwin/arm64 linux/amd64 ...
                                 # CGO_ENABLED=0 always; modernc.org/sqlite is pure Go

(cd tests && go test ./...)      # the conformance suite, over HTTP against bin/analog-server
                                 # (a separate module; ANALOG_SERVER_BIN to point at
                                 # another binary)

(cd web && npm run build)        # tsc --noEmit && vite build
(cd web && npm run dev)          # HMR on :5173, proxies /api to 127.0.0.1:8787
```

Run the server:

```bash
bin/analog-server seed --reset   # load contracts/fixtures/ into a fresh database
bin/analog-server                # http://127.0.0.1:8787  (/s/redesign is the seeded space)
bin/analog-server token add kai --kind human      # mint a token (shown once)
bin/analog-server token add claude-code --kind agent
```

`seed` refuses to overwrite an existing DB without `--reset`. Data lives in
`./data` (or `ANALOG_DATA_DIR`): `analog.db`, `media/`, `auth.json`.

## Architecture

    cmd/
      analog/          the `analog` CLI (cobra)
      analog-server/   the server; also `seed` and `token` operator subcommands
      analog-mcp/      MCP over stdio (10 tools, SPEC §4.1)
    internal/
      store/           SQLite. EVERY business rule lives here; schema.sql frozen
      api/             HTTP handlers; deliberately thin plumbing only
      auth/            per-actor bearer tokens (JSON file, SHA-256 digests)
      sse/             SSE fan-out broker, in-process
      ids/             ULID with s_/c_/l_/a_/m_ prefixes
      mcp/             the ten MCP tools, thin over client/
      web/             the built bundle is embedded from here (//go:embed)
      apierr/          the contract's Error schema, nothing else
      tokencli/        the `token` command group; exposed by BOTH binaries
                       (`analog token ...` and `analog-server token ...`),
                       runs on the server host, edits auth.json directly
      config/          env-var config + defaults recorded in DECISIONS.md
    client/            exported HTTP client; third parties import this
    web/               React 19 + Vite 8 (src/api.ts is hand-written, NOT generated)
    skill/analog/      the agent skill (what gets shipped to agents, not repo docs)
    scripts/           build.sh; the §7 demo and the deprecated onboard shim (go run ./scripts/…)
    tests/             the conformance suite — a separate go module (see below)
    contracts/         frozen wire format + fixtures
    deploy/            systemd unit and Caddyfile

**Data flow:** HTTP handler (api/) → store/ (single write transaction; pending
events buffered in the tx) → commit → `publish` callback → SSE broker. A rollback
drops the pending events. Handlers never contain rules — if you find yourself
adding logic to `internal/api`, it belongs in `internal/store`.

## Testing approach

Two suites, different jobs:

- **`tests/`** — the conformance suite: the *executable definition of Analog*.
  Written against `contracts/` and `SPEC.md`, not the implementation; reaches the
  server over HTTP as a separate process. It is a **separate go module**, so the
  implementation is structurally unimportable, and `black_box_test.go` asserts
  the whole test-binary dependency graph stays free of it. `coverage_test.go`
  fails if an openapi operation or a fixture is never exercised. It began in
  python and was ported to go under a coverage-parity regime (issue #58); the
  python original retired once parity was proven. If a change makes one of these
  fail, the interesting question is which side is wrong — sometimes it is the
  fixture, and that is an amendment.
- **Go tests beside the code** — anything holding implementation objects: client,
  CLI, MCP tools, token store, `internal/api/contract_test.go` (every documented
  operation must be routed and every route documented).

## Non-obvious gotchas

- **There is no top-level `analog/` directory, and none should be recreated.** It
  was the Python implementation's package; the Go module root is the repo root.
  `tests/unit/` is likewise a leftover that is gone — the Go port rewrote those tests.
- **`internal/web/dist` must exist or the build breaks**: `//go:embed all:dist`
  fails to compile with nothing to embed. Only `.gitkeep` is tracked; the bundle
  is copied in by `scripts/build.sh` (which deletes stale assets first). The
  `.gitignore` anchor `/dist/` exists precisely so `internal/web/dist/.gitkeep`
  stays trackable. Source maps are never embedded (build.sh deletes them; Vite
  uses `sourcemap: "hidden"`).
- **Numbers round-trip as literals.** Card/edge blobs and requests decode through
  `json.Number`; decoding into `float64` turns the fixtures' `"x": 0` into `0.0`
  and the roundtrip test fails byte-for-byte.
- **Timestamps come in two precisions, deliberately.** `store.Now()` is
  milliseconds with `Z` (`2006-01-02T15:04:05.000Z`); `auth.Now()` is seconds.
  Do not unify them without checking what reads each.
- **Feedback buckets are insertion-ordered maps.** Go randomizes map iteration
  and the buckets are compared against a frozen fixture. Use the `bucket` type in
  `internal/store/feedback.go`, not a plain map.
- **Two connection pools: many readers, one writer.** WAL mode, pragmas ride the
  DSN (`busy_timeout(5000)`, `journal_mode(WAL)`, `foreign_keys(ON)`) so every
  pooled connection gets them. Reads inside a write go through the transaction.
  Never write via the read pool.
- **Clients never choose ids.** `POST /cards` and `/import` discard incoming ids
  and return an `id_map`. Ordering is by SQLite `rowid` (insertion order).
- **`sp_deleted_at` is projected at read time**, only under
  `include_deleted=true`; never stored in the node blob.
- **Auth**: tokens map to exactly one `(actor, actor_kind)`. `actor`/`actor_kind`
  stay **required** on every mutation and must match the token or it's a `403` —
  never silently corrected (SPEC §10: misconfigured agents fail loudly). The
  token file is re-read per request, so issuing a token secures a running server.
  A non-loopback bind with no tokens refuses to start. `GET /api/health` is the
  only public path. CORS is the outermost handler so 401s carry the headers.
- **CLI exit codes: 1 error, 2 conflict, 3 auth.** `analog whoami` is the first
  diagnostic when something 401s/403s. On a 409 the error carries the server's
  current node (`client.Error.Current()`); conflicts are surfaced, never
  auto-resolved (SPEC §3).
- **CLI writes are optimistic**: `analog update --if-match <rev>` and
  `--mode replace|branch` (a stale rev is a 409); most commands take `--json`
  for machine-readable output.
- **`analog login` writes `~/.analog.toml` for the user** — an agent running as
  you would inherit your identity. Agents use `ANALOG_URL`/`ANALOG_ACTOR`/
  `ANALOG_TOKEN` env vars (or `analog onboard --wrapper`); `ANALOG_ACTOR` has
  no default because the feedback cursor is keyed by actor.
- **Branch mode** (`updateCard` with branch revision mode) emits **two** events,
  `card.created` then `link.created`, and freezes the superseded card's rev.
  Revising an already-superseded card is a `409`. Setting `sp_superseded_by` is
  bookkeeping and emits nothing.
- **`PATCH /annotations/:id` with no `resolved` key resolves.** `resolved: false`
  reopens and emits no event (there is no `annotation.reopened` type).
- **SSE is consumed via `fetch`, not `EventSource`** (which cannot set headers;
  a token in the query string would leak). Media is fetched into blob URLs for
  the same reason. The web token lives in `localStorage`, deliberately not a
  cookie: a card's sandboxed iframe cannot set an `Authorization` header, so
  agent-authored HTML has no ambient credential.
- **`writeJSON` sets `SetEscapeHTML(false)`** — card text is routinely HTML and
  the wire should say what the client sent.
- **Vite 8 will not run under a Bun or Volta shim.** If `npm --version` looks
  implausible, point at a real Node 22+ install. `web/bun.lock` is gitignored.
- **Release**: tag `v*` triggers cross-compile of 5 platforms; `analog-server`
  must stay under 20 MB and serve the embedded UI from an empty dir. Asset names
  are **unversioned** (`analog-<platform>.tar.gz`) on purpose — the README's
  `/releases/latest/download/` install line resolves exact names only.

## Naming and style

- IDs: ULID with prefixes `s_`, `c_`, `l_`, `a_`, `m_` (see `internal/ids`).
- Node kinds: `md | html | svg | plain` (text nodes); `file` nodes for uploaded
  binaries. Motivations: `commenting | assessing | editing`.
- Comments explain *why*, never *what*. Match the density of the file you edit.
- Go: `gofmt`-clean, `go vet`-clean; types are hand-written, not generated.
- Commits need a `Signed-off-by` line (DCO, no CLA).
