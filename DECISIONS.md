# Runtime decisions

Things `contracts/` leaves open, decided once here so WP1–WP4b don't each guess.
Contract-derived values are marked; the rest are reversible defaults.

## Network

| | | Why |
|---|---|---|
| API port | **8787** | Contract. `openapi.json` → `servers[0].url` = `http://127.0.0.1:8787/api`. |
| Bind address | `127.0.0.1` | SPEC §3: no auth in v1. `ANALOG_HOST` overrides. |
| API prefix | `/api` | Contract, same source. |
| Web dev server | `5173` (Vite default, `strictPort`) | Proxies `/api` → 8787, so the app is same-origin in dev exactly as in prod. This matters: SPEC §5's iframe-sandbox reasoning assumes the annotation overlay and the artifact iframe are not same-origin with each other, and a cross-origin dev setup would have hidden a mistake there. |
| CORS | allowlist `http://localhost:5173`, `http://127.0.0.1:5173`; `ANALOG_CORS_ORIGINS` overrides | Only needed if someone runs the web app without the proxy. Not `*` — cheap to keep narrow. |
| Data directory | `./data`, or `ANALOG_DATA_DIR` | A binary has no checkout to sit beside, so it makes a `data/` where you ran it. |

## Identifiers

ULID with the prefixes `schema.sql` already specifies: `s_`, `c_`, `l_`, `a_`, and
`m_` for media. Chosen over UUID4 because ULIDs sort by creation time, so
`ORDER BY id` and the event log agree without another column.

**Clients never choose ids.** `POST /cards` with raw `nodes` and `POST /import` both
discard incoming ids and return an `id_map`. An id chosen by a client can collide
with one already in the space, and `POST /import` is documented as remapping anyway.

**Ordering.** `GET /canvas`, `/annotations` and the feedback annotation list order by
SQLite `rowid`, i.e. insertion order. The frozen fixtures are in creation order but
their readable ids (`c_opt_a`, `c_chart`) do not sort that way, so ordering by id
would not round-trip them.

## Storage

    data/                       ANALOG_DATA_DIR
      analog.db                 ANALOG_DB overrides the full path
      media/<space_id>/<m_ulid>.<ext>

Media is keyed by **space id, not slug**, so renaming a space cannot orphan its
files. Server-assigned filenames; the client's filename is advisory and never
touches the filesystem. Accepted types: PNG, JPEG, GIF, WebP, SVG, PDF. 25 MB cap.

## Behaviour the contract does not pin

- **Auto-layout** (SPEC §5): cards created without `x`/`y` go to the right of the
  live bounding box, 40px gutter, top-down; default card box 320×200. Deleted cards
  are excluded from the bounding box. First card in an empty space lands at `(0,0)`.
  **A column wraps into a new one past 900px** (`LAYOUT_MAX_COLUMN`) — the literal
  reading of §5 put five cards in a 1280px strip you had to zoom out to read.
  Approved 2026-08-29.
- **`sp_deleted_at`** is projected onto nodes at read time from `card.deleted_at`,
  only when `include_deleted=true`. It is never stored in the node blob, so
  `GET /canvas` cannot leak a tombstone.
- **Feedback bucketing**: one row per subject. `changed` is the union across all
  `card.updated` events in the window. A deletion supersedes an edit or a move for
  the same card; an edit supersedes a move. A link created and removed inside the
  same window appears in neither bucket.
- **`summary` grammar** is pinned by the fixture and asserted in
  `tests/feedback_test.go`.
- **`PATCH /annotations/:id` with no `resolved` key resolves.** Every caller surface
  (`analog resolve`, MCP `resolve_annotation`) only ever resolves. Setting
  `resolved: false` reopens and emits no event — there is no `annotation.reopened`
  type.
- **Actor via headers.** SPEC §3 allows "query params or headers"; `openapi.json`
  documents query only. Query is the contract and what `client/` sends;
  `X-Analog-Actor` / `X-Analog-Actor-Kind` are accepted as a fallback for `curl`.
- **Branch mode**: the new card is auto-placed rather than stacked on the card it
  supersedes, and revising an already-superseded card is a `409` — the chain has one
  head.

## Added after the first review (2026-08-29)

- **A space index at `/` and a switcher in the topbar.** SPEC §5 specifies one route,
  `/s/:slug`, which left the app with no entry point: `/` was a dead end. The
  switcher lists every space including the current one, ticked.
- **Wheel over a card scrolls the card, not the board.** A card body with
  `overflow: auto` scrolls natively *and* bubbles the wheel up to the canvas pan
  handler, so both happened at once. The canvas now declines the event while an
  ancestor still has room to scroll that way, and takes it back at the end.
- **`Cache-Control: no-cache` on `index.html`.** Asset filenames are content-hashed
  and may cache forever, but the document naming them must not, or every rebuild is
  invisible until a hard reload.
- **Shift-drag for a region annotation is now in the hint bar.** It always worked;
  nothing told you it existed.

## Going remote (2026-08-29)

- **Per-actor bearer tokens, not the shared token SPEC §3 sketched.** A shared token
  gatekeeps the server but not identity, and §2.2/§10 make `actor` load-bearing. A
  token maps to exactly one `(actor, actor_kind)`; the server compares the declared
  actor against it and returns `403` on a mismatch rather than quietly correcting it,
  because a silently corrected actor is not "failing loudly".
- **Tokens live in a JSON file, not a table.** `schema.sql` is frozen, and
  credentials are operator state rather than canvas data. SHA-256 digests, mode 600.
- **A non-loopback bind with no tokens is refused at startup.** The one failure mode
  worth being rude about.
- **`GET /api/health` is public.** A client cannot be asked to authenticate before it
  can discover that authentication exists.
- **A header, never a cookie.** A card's sandboxed iframe cannot set an
  `Authorization` header, so agent HTML has no ambient credential to ride. A cookie
  would have handed it one — which is the risk SPEC §8 raised about this touching a
  network.
- **SSE moved from `EventSource` to `fetch`.** `EventSource` cannot set headers, and
  a token in the query string leaks into logs and referrers. Parsing SSE framing is
  about fifteen lines, and reconnection got more honest as a side effect.
- **Media is fetched into a blob URL**, because `<img src>` carries no header either.
- **The web bundle takes its server as data** (`web/src/connection.ts`), so one build
  serves both the same-origin browser and the Tauri shell.
- **A desktop shell gets no second API client.** It loads the bundle this server
  serves; a second implementation of the auth rules is a second thing to get wrong.
  `server/config.py` allows the `tauri://localhost` origins for that reason, and
  `web/src/connection.ts` is what makes one build serve both cases.

## Go (2026-08-29)

The core moved from Python to Go (#13). Nothing about the product changed: the HTTP
API, the database schema, the fixtures and the web UI are the same, and the
conformance suite that proves it was written before any Go existed.

- **`modernc.org/sqlite`**, not `mattn/go-sqlite3`. Pure Go, so `CGO_ENABLED=0
  GOOS=windows go build` works from any machine and a release needs no C toolchain
  per platform. That is the whole reason the port is worth doing.
- **Types are hand-written, not generated from `contracts/openapi.json`.** The
  tempting argument is that generation makes the contract structurally load-bearing.
  The bodies that matter here are free-form JSON Canvas blobs with arbitrary `sp_*`
  keys, so a generator emits `map[string]any` for them anyway — and for the ten named
  schemas it would emit response models, which is the one thing `models.py` explicitly
  refused to do because a response model can silently drop what the contract requires.
  `internal/api/contract_test.go` gets the benefit a different way: every documented
  operation must be routed and every route documented, checked on every run.
- **Card and edge blobs decode through `json.Number`.** Decoding into `float64` would
  turn the fixtures' `"x": 0` into `0.0` and lose precision on a large integer in
  `sp_meta`. Numbers now round-trip as the literal they arrived as.
- **Pending events belong to the write transaction, not to the store.** In Python they
  were thread-local, and before that a class attribute shared between requests — a bug
  that had already been fixed once. Scoping them to the transaction removes the
  category rather than the instance. They publish after commit; a rollback drops them.
- **Two connection pools: many readers, one writer.** SQLite in WAL mode allows
  exactly that, and Go's `database/sql` would otherwise hand concurrent writers a
  `SQLITE_BUSY`. Reads inside a write go through the transaction so they still see
  uncommitted state. `busy_timeout=5000`, `journal_mode=WAL` and `foreign_keys=ON`
  ride on the DSN, so every pooled connection gets them rather than whichever one a
  query lands on first.
- **The feedback buckets are insertion-ordered maps.** Go randomises map iteration and
  the buckets are compared against a frozen fixture; Python's dict order was load-bearing
  and nothing said so.
- **CORS wraps authentication.** In FastAPI that meant registering it last; in Go it is
  the outer `http.Handler`. Either way a 401 has to carry the headers, or the browser
  reports an opaque network error instead of "unauthorized".
- **The web bundle is embedded with `//go:embed`.** `scripts/build.sh` copies
  `web/dist` into `internal/web/dist` before building, so `analog-server` alone serves
  the UI with no repo beside it. ~11 MB with the bundle inside.
- **Source maps are not embedded, and not released.** A source map is a debugging
  artifact, the same category as a `.dSYM` — it belongs beside a build, not inside
  the executable, and it was 2 MB of every binary. Shipping them with a release would
  earn its keep if something ingested minified stack traces, but Analog has no
  telemetry by design and any tagged commit rebuilds the same bundle from source that
  is already public. Vite still writes the map into `web/dist` for debugging a
  production build locally; `sourcemap: "hidden"` keeps the `sourceMappingURL` comment
  out of the bundle, so nothing goes looking for a file the server does not have.
- **`analog-server` grew `seed` and `token` subcommands.** They are operator commands
  on the data directory rather than API calls, and putting them on the server binary is
  what lets the conformance harness run with no Python of its own in the path.
- **`client/` is not under `internal/`.** Third parties import it, so it defines its
  own types rather than exposing `internal/store`'s — which Go would not let an
  outside package name anyway.

### The conformance suite moved to Go (2026-09-01, #58/#59)

The harness began in Python and stayed there through the Go port. The reasons were
real and are kept here because they shaped everything since: it was written against
`contracts/` and `SPEC.md` before any Go existed, so it could not have been shaped by
the implementation it judged; a judge in another language could not quietly reach for
the server's own objects; and red-to-green against it is what made the port tractable
at all.

It was ported anyway — on evidence rather than trust. The Go suite first ran *beside*
the Python one under a coverage-parity regime: a correspondence table mapping every
Python test to its Go counterpart, a test failing if either suite stopped referencing
an openapi operation or a fixture, and CI running both suites against one binary.
Only after that run went green on both platforms did the Python original retire.

What the port preserved, by construction rather than by promise:

- **Outside observer over a real socket.** The suite spawns a server process and
  speaks HTTP; it judges any binary that answers, including a future rewrite.
- **The module boundary instead of the language boundary.** `tests/` is a separate
  Go module, so `internal/` is structurally unimportable from it — and
  `black_box_test.go` asserts the whole test-binary dependency graph stays free of
  the implementation. The Python suite's import scan was a weaker form of the same
  check.
- **Types hand-written against `contracts/openapi.json`**, never against
  `client/types.go`; byte-level fixture comparison; stdlib only, plus the pure-Go
  sqlite driver for the frozen-schema tests.

What was traded away, and accepted: the judge now shares language and idioms with
the implementation, so its discipline is convention plus the module boundary rather
than physics. One language for contributors, no Python in the toolchain, and a
suite that doubles as a stdlib-only demonstration of the protocol is the return.

Everything that genuinely needed the implementation's objects — the token store, the
client, the CLI, the MCP tools — remains a Go test next to the code. `tests/README.md`
has the split.

## Loopback CORS (2026-08-30)

- **Loopback origins are echoed by default** (#42): `http://localhost:<any port>`
  and `http://127.0.0.1:<any port>`, alongside the tauri origins. The desktop app
  moved its UI to a local sidecar, so the page's origin is a loopback port the
  server cannot know in advance — the tauri whitelist anticipated the shell being
  the origin, but the sidecar-served page is what makes the cross-origin calls.
  Browsers set `Origin` truthfully, so a loopback origin can only come from a page
  actually served on the user's machine: the same trust class the tauri schemes
  were for, generalized over the port instead of special-casing one. A suffix like
  `localhost.evil.example` and other schemes stay denied.
- **An explicit `ANALOG_CORS_ORIGINS` replaces the defaults wholesale**, loopback
  matching included — the way it already replaces the tauri origins. A custom list
  is a deliberate policy, not a delta on top of the defaults.

## Drawing cards (2026-09-01, #61)

A sketch is an `svg` card, not a new kind. The contract already has svg; a pen is
how the human authors it. Agents still read the SVG, pins still land on it, export
still opens in Obsidian.

- **Shift-double-click empty space** drops a 480×360 svg card and opens the pen.
  Double-click (or ✎) on an existing svg card draws on it rather than opening the
  source. Agent markup stays; new strokes append as `data-analog-stroke` paths so a
  later edit can find them without a second `sp_kind`.
- **Escape discards, clicking outside commits, ⌘Z undoes a stroke** while the pen
  is down. Analog's own undo then takes the whole edit back after commit.

## Toolchain

- Go **1.23+**. `CGO_ENABLED=0` everywhere.
- Node **22+** with a real `npm`. Vite 8 will not run under a Bun or Volta shim, so
  if `npm --version` looks wrong, resolve it to an actual Node install first.
- The MCP command is **`cmd/analog-mcp`**. The Python note about `mcp_server/` vs
  `mcp/` was about a `sys.path` collision with the `mcp` PyPI package; Go has no such
  hazard, and `internal/mcp` is named plainly.
