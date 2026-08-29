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
  `tests/contract/test_feedback.py`.
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

## Toolchain

- Python **3.11+**, pinned deps in `pyproject.toml` (resolved 2026-08-28 on 3.14).
- Node **22+** with a real `npm`. Vite 8 will not run under a Bun or Volta shim, so
  if `npm --version` looks wrong, resolve it to an actual Node install first.
- The MCP package directory is **`mcp_server/`**, not `mcp/` as in SPEC §6. A
  top-level `mcp/` on `sys.path` shadows the `mcp` PyPI package that FastMCP imports,
  which breaks the MCP server and every test run from the repo root. Nesting
  everything under `analog/` removed that hazard — `analog.mcp` would be safe now —
  but renaming buys nothing, so it stays.
