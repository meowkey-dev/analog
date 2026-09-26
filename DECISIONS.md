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
| Data directory | `~/.analog`, or `ANALOG_DATA_DIR` | Server state is user-global, not relative to whichever directory launched the binary (#84). |

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

    ~/.analog/                 ANALOG_DATA_DIR
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
  can discover that authentication exists. `version` stays the contract
  (`openapi.json` info.version). `release` is the binary, the same string
  `--version` prints, so the spaces list can show which analog-server this is
  without amending a frozen schema. Extra properties are allowed.
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

## Markdown math (2026-09-02, #68)

Agents writing a derivation should not have to drop into an `html` card for `$x^2$`.
The card kind is still `md`; math is a renderer plugin, not a new `sp_kind`.

- **KaTeX, not MathJax.** KaTeX is render-only (no TeX engine at runtime), so the
  cost is fonts + CSS rather than a second interpreter in the bundle. `remark-math`
  parses `$...$` / `$$...$$`; `rehype-katex` turns those nodes into HTML. The text
  on the wire stays the TeX source — export, diff, and the editor see `$e^{i\pi}$`,
  not a formula image.
- **Malformed TeX does not take the card down.** `throwOnError: false`, so a bad
  formula is a red error glyph, not a blank body.
- **Math inherits the markdown theme.** KaTeX does not set a fill colour, so
  nord/paper/solar (#41) keep working. Display formulas `overflow-x: auto` inside
  the card so a wide integral does not spill onto the board.
- **woff2 only.** KaTeX's CSS references woff2, woff and ttf for each face; Vite
  would have embedded all three. The extra copies are dropped at build time.

## File upload from the board (2026-09-02, #67)

The API and CLI already placed file nodes; the human had no way to. A screenshot
is a file node, not a new kind — `POST /media` then `POST /cards` with `nodes`.

- **Drop or paste** onto the board places a file card at the pointer (or the
  viewport centre, for paste). The card is sized to the image, capped at the
  sketch box so a 4K screenshot does not fill the space.
- **Upload** in the topbar is the same path without a drop point, so the server
  auto-lays the card out like `analog upload`.
- Accepted types and the 25 MB cap match the server; a rejected file is a toast,
  not a 400 the human has to decode.

## Portable export (2026-09-03, #72)

JSON Canvas is the git-diffable interop format; HTML and PDF are for handing the
board to someone who is not running analog-server.

- **No new endpoint.** Both formats are a presentation of `GET /canvas` plus
  `GET /media`. The contract stays frozen.
- **HTML is the portable snapshot.** Cards keep their canvas positions, file
  nodes become data URIs, html cards stay in a sandboxed `srcdoc` iframe. The
  UI export serialises the live DOM (markdown, KaTeX, themes, drawings already
  rendered). The CLI rebuilds the same layout in Go with goldmark for `md`
  cards — math stays as `$...$` there, because KaTeX is a renderer plugin, not
  a second copy of the web bundle inside `analog`.
- **Portability has an explicit boundary.** Analog's captured styles (including
  the markdown/KaTeX styles present in the live document) and uploaded file-card
  media are embedded in the HTML. A sandboxed html card remains an iframe, so
  the exporter cannot safely inspect or rewrite its user-authored document;
  external images, stylesheets, fonts, scripts, and other HTML dependencies in
  that iframe may remain external. Parent-document resources are fetched without
  ambient credentials when they can be captured; CORS or an unavailable resource
  leaves its original URL in place rather than making export fail.
- **PDF is that HTML, printed.** The topbar Export → PDF opens a print dialog
  (`window.print()`) only after the exported page reports that fonts, media, and
  iframe documents have settled. `analog export --format pdf` shells out to
  Chrome/Chromium (`ANALOG_CHROME` overrides) with `--print-to-pdf`. The CLI
  requires a structurally complete PDF (xref/root plus the final `%%EOF`) and
  waits for stable bytes before stopping a headless process that stays alive;
  otherwise it waits for clean exit and reports an incomplete file. It never
  disables Chrome's sandbox. analog-server stays CGO-free and under 20 MB; a Go
  PDF library that could paint HTML cards, SVG and KaTeX would be neither.
  Without Chrome, save HTML and Print to PDF.
- **Annotations, handles, and the rest of the chrome stay off the snapshot.**
  The export is the board, not the conversation.

## HTML card network boundary (2026-09-04, #62/#64)

An HTML card is an opaque-origin `srcdoc` document with exactly
`sandbox="allow-scripts"`. The sandbox protects Analog's parent document,
credentials and annotation layer from agent-authored HTML; it is not a network
firewall. We do not add `allow-same-origin`, `allow-forms`, or a persisted
per-space opt-in. Forms are unrelated to the supported network path.

AG-UI's `HttpAgent` sends a JSON `POST` with `Content-Type: application/json`
and `Accept: text/event-stream`, then consumes streamed SSE events from the
response body. A card can use that same shape with a sidecar endpoint. Because
the sandboxed document's origin is `null`, the sidecar must handle the CORS
preflight and return `Access-Control-Allow-Origin: null`, allow `POST` and the
headers the card sends, and keep the SSE response CORS-readable. An agent
endpoint that needs an authorization header must allow that header in its
preflight; Analog never injects its bearer token into the card. `Origin: null`
is shared by arbitrary opaque origins and is not an authentication signal. The
credentialless loopback demo must not be copied by embedding a long-lived
bearer token in stored card HTML; trusted cards arrange user-supplied or
short-lived credentials out of band and the sidecar authenticates them.

The main card, its pop-out, and both portable export paths preserve this
scripts-only iframe boundary. Export preserves the card document but neither
proxies nor embeds its sidecar, so an exported card is interactive only while
that external endpoint remains available and permits the opaque origin. The
checked-in `examples/ag-ui` sidecar, card, and `scripts/ag-ui-smoke.sh` prove
the path in a real browser. Analog remains a canvas and review surface, not an
agent runtime.

## Canvas polish (2026-09-10, #78/#79/#82)

- **Drag gestures cancel their native pointer action immediately.** Applying
  `user-select: none` after drag state renders is too late to stop the browser from
  beginning a selection on pointerdown. Pan, card move and resize all cancel it at
  the gesture boundary; card bodies remain selectable when no drag starts.
- **The space switcher scrolls inside the viewport.** Its list has no product-level
  limit, so clipping the menu would make later spaces unreachable.
- **A popped-out HTML card gets a centred 1200px-wide viewport on wide displays.**
  The authored document is not rewritten or styled from the parent. Its
  scripts-only sandbox remains identical to the main card; only the iframe chrome
  is constrained.

## Comment presentation (2026-09-10, #80/#83)

- **Comment author colors follow actor kind everywhere.** Human author labels use
  `var(--ok)` and agent labels use `var(--accent)`, matching the activity panel.
  Motivation badges, selection, and pins keep their own semantic colors.
- **Card threads are history; pins and counts are open work.** A card thread keeps
  resolved comments in insertion order and shows their resolution replies. The
  badge and overlay still contain only unresolved comments, so closing a comment
  removes its pin without hiding the discussion that led to the resolution.

## In-card search (2026-09-11, #81)

- **⌘/Ctrl-F searches the selected card; ⌘K still locates cards.** The two scopes
  stay separate: board search chooses a card, while in-card search selects and
  scrolls through rendered matches inside that card. A header control makes the
  scoped search discoverable without knowing the shortcut.
- **HTML search stays inside the existing sandbox.** The injected read-only helper
  finds and selects text in its own document and reports only the match count and
  index through `postMessage`. The iframe remains scripts-only with an opaque
  origin; no DOM access or additional sandbox capability is granted to the parent.

## Analog style (2026-09-14, #76)

- **The skill folder gains `STYLE.md`, a sibling of `SKILL.md`.** SKILL.md
  teaches the workflow and stays short because it loads on demand; the card
  guidance is longer and only matters when an agent writes an `html` or `svg`
  card, so it lives in a second file SKILL.md points at. A file, not a
  subdirectory: the embed test rejects directories, and `analog onboard`
  copies the whole folder, so no install code changes.
- **A card paints itself, in Nord, single-theme.** The sandbox passes nothing
  in and the frame behind an unpainted document is white, so the guide fixes
  `color-scheme: dark`, the exact Nord values `md-theme-nord` already uses, and
  an explicit ground; it forbids `prefers-color-scheme` switching so the human,
  the export and the agent's own check all see the same card. Nord rather than
  a per-card palette because a board mixes markdown and html cards, and one
  palette keeps them from reading as two products.
- **The column is fluid until it is centred and capped.** The main card is
  resizable and the pop-out is 1200px wide (#82), while the parent never
  restyles the document. The guide therefore makes the card body fill the
  available frame with `width: 100%` and `box-sizing: border-box`, then caps its
  measure and centres it with `margin-inline: auto`.
- **Real text and addressable regions are the design constraint.** Pins,
  rectangles and in-card search all walk DOM text (#23, #81). The guide
  therefore bans text in canvas or raster and asks for spaced regions, so a
  shift-drag selects one claim. This is the reason the style exists at all,
  not a taste.
- **No external resources.** Portable export (#72) carries the document and
  nothing else, and the guide follows that boundary: no CDN scripts, no remote
  images, system fonts by default.
- **`analog skill install|status|cat` is the update path, not an optional
  actor on `onboard`.** Every onboard flag takes its meaning from the actor, so
  making the argument optional would turn one command into two modes where
  half the flags become errors. Refreshing an installed skill after a binary
  upgrade needs no identity and wants a changed/unchanged answer, which
  onboard's banner has no room for. `onboard` keeps its polite skip (#63) and
  now names the refresh command; both share `installSkill`.

## Agent card geometry (2026-09-15, #90)

- **Geometry uses the existing JSON Canvas fields.** Agents set `x`, `y`, `width`
  and `height`; there is no separate scale property or transform to reconcile with
  export, links, annotations and the human's resize handles.
- **Both agent surfaces expose the same controls.** MCP advertises geometry on
  `add_cards` and `update_card`; the CLI accepts `--x`, `--y`, `--width` and
  `--height` on both `add` and `update`. Omitted creation coordinates still invoke
  server auto-layout, including when a coordinate would otherwise be zero.
- **Geometry-only updates retain move semantics.** They do not bump `sp_rev` or
  stale annotations. Agents may deliberately lay out and size their own work, but
  the skill still tells them not to rearrange cards the human positioned.

## User-global server state (2026-09-17, #84)

- **The unconfigured data directory is `~/.analog`, not `./data`.** Starting the
  same binary from a different working directory must not produce an apparently
  empty server. A user-owned home directory also works for release binaries without
  requiring elevated permissions.
- **Existing overrides are unchanged.** `ANALOG_DATA_DIR` still relocates all
  state; `ANALOG_DB` and `ANALOG_AUTH_FILE` still override their individual files.
  Deployments already use these explicit paths.
- **There is no implicit migration from `./data`.** A working directory is not a
  stable installation identity, so choosing and moving one automatically could
  select the wrong data or collide with an existing global installation.

## Explainer sets (2026-09-18)

- **A third skill file, `EXPLAIN.md`, not a second skill.** SKILL.md teaches the
  workflow, STYLE.md how one card looks, and this one the shape of a *set* of
  cards. The three load on demand and only the relevant one costs context; a
  separate skill would need its own wiring, its own install path and its own
  copy of the auth and feedback rules. A file for the same reason STYLE.md is a
  file (#76): the embed test rejects directories and `onboard` copies the whole
  folder, so no install code changes.
- **The survey is a step, not a sentence.** The methodology this borrows from
  confirms the learner's level in passing. Analog makes asking cheap — a probe
  card is one `md` post and `await_feedback` blocks on the answer — and makes
  guessing wrong expensive, because the whole set has to be rewritten at the
  right level. So the probe is step one and has its own rules: five questions,
  a three-way answer, never a grade.
- **The probe asks about meta-concepts, not the topic.** The two to four ideas
  everything else leans on are the only ones whose answer changes the route. A
  quiz on details would measure knowledge the cards are there to supply.
- **The links are the map.** The source methodology publishes a table of
  contents page because a folder of HTML files has no other structure. Analog
  already has a graph, so the route card carries the target, the meta-concepts
  and the assumed floor, and labelled edges carry the dependencies. A listed
  contents page would duplicate the graph and go stale against it.
- **No clock.** The original budgets 180 minutes across four acts. A board is
  not paced: the human scrolls, annotates and comes back. The budget is six to
  twelve cards on a dependency ladder, which is the thing that actually bounds
  the work.
- **EXPLAIN.md chooses the shape; STYLE.md draws it.** The source methodology
  makes every page an eli5 infographic — a big picture, little text — and that
  discipline is the reason its pages work at all. STYLE.md already has the four
  shapes and how each looks, so EXPLAIN.md does not restate them: it says which
  shape a card should be (eli5 by default, infographic whenever a real number
  carries the point, comparison for a "which", `svg` for parts and arrows) and
  keeps the one rule STYLE.md has no place for, the ~150-word prose budget per
  card. An idea that does not fit is two ideas.
- **The set says where you are, because the graph does not.** The original
  stamps each page with its act and its minute. Dropping the clock dropped the
  orientation with it, so each card names its rung and its place in the set. A
  board shows structure, never reading order or how much is left.
- **One spine covers a topic and a session recap.** Both are a human who must
  hold ideas they do not yet hold; only the material differs (research versus
  the diff) and the ending (understanding versus a verdict). Two skills would
  have diverged on the four rules they share. The recap half inherits SKILL.md's
  rule that Analog is not a log: order by what must be understood first, never
  by chronology.

## Working context (2026-09-26)

- **A fourth skill file, `CONTEXT.md`, for the reasons EXPLAIN.md is one.** It
  loads on demand, `onboard` and `skill install` copy the whole folder, and the
  auth and feedback rules stay in SKILL.md alone.
- **The space is the handoff, so there is no handoff document.** The test is
  that an agent with only the space and the repo can take the next step. A
  separate document would be a second source that drifts from the cards and
  that the human cannot annotate.
- **Only what the repo cannot rebuild, and only what still matters.** Code,
  diffs and history are pointed at, never copied. What a conversation holds and
  a repo does not — the goal in the human's words, chat instructions, decisions
  and what was rejected, coined vocabulary, hypotheses, dead ends, what the
  dirty tree is for — is the whole content. Over-preserving is a failure too: a
  context that cannot be read in two minutes is skimmed.
- **Five cards, split by how often they change.** Brief and Decisions change
  rarely, State constantly. Replacing a card bumps `sp_rev` and marks its
  annotations stale, so the human's comments belong on cards that do not churn;
  questions get their own card (Open threads) for the same reason. Decisions is
  one ledger rather than a card per decision, because reload cost is what the
  set is budgeted by.
- **A title prefix, not a feature.** `⌂` marks the context cards in a shared
  space and a dedicated one alike. `sp_meta` could carry a role, but the CLI
  does not expose it and a convention needs no code; a command that prints the
  set can come once the convention has settled.
- **Replace in place, with `--if-match` and an explicit `--mode replace`.**
  The cards are the current truth, so they are the one bounded exception to
  "Analog is not a log". An omitted mode inherits the space's revision mode,
  and in a branch-mode space that would leave two cards with one title and a
  Brief pointing at the superseded one, so the mode is always spelled out. A
  `409` is merged, not overwritten, because the other writer may be the human.
- **Nothing about compaction.** The triggers are start, checkpoint and pick
  up, and none of them asks why the work was interrupted. Pick up folds in any
  newer notes the reader holds — a summary, a handoff someone else's tooling
  left — so the space composes with such tools without naming them, and
  promoting durable learnings to the repo's docs is left to whoever owns them;
  a finding only carries a `(durable)` hint.

## Toolchain

- Go **1.23+**. `CGO_ENABLED=0` everywhere.
- Node **22+** with a real `npm`. Vite 8 will not run under a Bun or Volta shim, so
  if `npm --version` looks wrong, resolve it to an actual Node install first.
- The MCP command is **`cmd/analog-mcp`**. The Python note about `mcp_server/` vs
  `mcp/` was about a `sys.path` collision with the `mcp` PyPI package; Go has no such
  hazard, and `internal/mcp` is named plainly.
