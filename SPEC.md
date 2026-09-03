# Analog — Build Spec v0.1

A shared canvas for one human and their agents. Not a chat app, not a project
tracker, no bundled LLM. It is a dumb, rich surface that any agent can write to over
HTTP/MCP and that the human can read, rearrange, and annotate.

**Status:** prototype spec. Optimized for speed and reversibility. Where a decision
was arbitrary, it says so.

**v1 scope assumption:** one agent per Space, plus the human. Multi-agent spaces are a
v2 concern; several decisions below are simpler because of this and are flagged where
that's load-bearing.

---

## 1. Product shape

One **Space** = one workstream = one infinite canvas. You will have many, one per
parallel agent thread.

A Space contains:

- **Cards** — positioned rectangles. A card's content is text, markdown, HTML, SVG,
  or an image. An "infographic to review" is just a card of kind `html` or `svg`.
- **Links** — labelled edges between cards.
- **Annotations** — comments attached to a card, optionally to a region within it.
  This is how the human talks back to the agent.

That's it. Three nouns. Everything else is derived.

### The loop this exists to support

1. Agent writes cards / an artifact into a Space.
2. Human opens the Space in a browser: reads, drags, rewrites, draws links, leaves
   comments on the artifact.
3. Agent calls `get_feedback(space, cursor)` at the start of its next turn and
   receives exactly what changed, already diffed server-side.

Point 3 is the whole design. The agent must never have to infer what changed by
comparing full state.

---

## 2. Data model

### 2.1 Wire format: JSON Canvas 1.0

Cards and links use the [JSON Canvas 1.0](https://jsoncanvas.org/) spec
(MIT, `obsidianmd/jsoncanvas`). It defines `nodes[]` and `edges[]`; nodes have
`id, type, x, y, width, height, color`; edges have `id, fromNode, fromSide, toNode,
toSide, label, color`. The spec allows arbitrary extra keys, which is how we extend it.

Why: we don't design a canvas format, we get git-diffable export, and a Space can be
opened in Obsidian. Python validation via `openjsoncanvas` (pypi).

**Our extensions** (namespaced, so other tools ignore them):

```jsonc
{
  "id": "c_01H...",
  "type": "text",            // JSON Canvas types: text | file | link | group
  "x": 0, "y": 0, "width": 320, "height": 200,
  "text": "…",               // for type=text
  "sp_kind": "md",           // md | html | svg | image | plain   ← how to render `text`
  "sp_title": "Option B",
  "sp_created_by": "claude-code",
  "sp_rev": 3,
  "sp_meta": {}              // free-for-all; agents may stash anything here
}
```

**Text node vs file node.** Content the agent generates as text (`md`, `html`, `svg`,
`plain`) is a `type: "text"` node and lives inline in `text`. Anything uploaded as
binary — screenshots, PNGs, PDFs — is a `type: "file"` node whose `file` points at the
URL returned by `POST /media`. `sp_kind` is only meaningful on text nodes.

The payoff is interop: a `file` node actually renders when you export the Space and
open it in Obsidian, which an invented image encoding would not.

### 2.2 Storage schema (SQLite for v1)

```sql
CREATE TABLE space (
  id TEXT PRIMARY KEY, slug TEXT UNIQUE, title TEXT,
  created_at TEXT, seq INTEGER DEFAULT 0        -- monotonic event counter
);

CREATE TABLE card (
  id TEXT PRIMARY KEY, space_id TEXT, node_json TEXT,  -- full JSON Canvas node
  rev INTEGER, created_by TEXT, updated_at TEXT, deleted_at TEXT
);

CREATE TABLE link (
  id TEXT PRIMARY KEY, space_id TEXT, edge_json TEXT,  -- full JSON Canvas edge
  rev INTEGER, created_by TEXT, updated_at TEXT, deleted_at TEXT
);

CREATE TABLE annotation (
  id TEXT PRIMARY KEY, space_id TEXT, card_id TEXT,
  card_rev INTEGER,       -- card's sp_rev when the annotation was made
  selector TEXT,          -- NULL = whole card; else JSON, see 2.3
  body TEXT,
  motivation TEXT,        -- commenting | assessing | editing
  creator TEXT,           -- 'human' or agent name
  creator_kind TEXT,      -- human | agent
  resolved INTEGER DEFAULT 0, resolved_reply TEXT,
  created_at TEXT
);

CREATE TABLE event (
  seq INTEGER, space_id TEXT, ts TEXT,
  type TEXT,              -- card.created | card.updated | card.moved | card.deleted
                          -- link.created | link.deleted
                          -- annotation.created | annotation.resolved
  subject_id TEXT,
  actor TEXT NOT NULL,       -- 'human' or the agent's name
  actor_kind TEXT NOT NULL,  -- human | agent
  payload TEXT,
  PRIMARY KEY (space_id, seq)
);

CREATE TABLE actor_cursor (
  space_id TEXT, actor TEXT, seq INTEGER,
  PRIMARY KEY (space_id, actor)
);
```

**Actor is mandatory.** Every mutating call carries `actor` and `actor_kind`; the API
returns `400` without them. The web UI hardcodes `human`. Agents must pass a name —
there is deliberately no default, so a misconfigured agent fails loudly rather than
writing anonymously. This is what makes the event log worth having.

Storing whole nodes as JSON blobs is deliberate: the schema never needs migrating
when JSON Canvas or our extensions change. Query performance is irrelevant at this
scale.

**Soft delete only.** `deleted_at` set, row kept. Agents need to see that the human
deleted a card — that's feedback.

### 2.4 Revision mode

When an agent replaces a card's content, two behaviours are useful and neither is
right for every space. Configurable per Space (`space.revision_mode`), overridable
per call (`?mode=` on `PATCH`, `--mode` on the CLI).

| Mode | Behaviour |
|---|---|
| `replace` *(default)* | Mutate in place, bump `sp_rev`. Old content survives only in the event log payload. Canvas stays clean. |
| `branch` | Create a new card with the new content, mark the old one `sp_superseded_by: <new_id>`, and auto-link old → new with `label: "revised"`. Both stay visible. |

```sql
ALTER TABLE space ADD COLUMN revision_mode TEXT DEFAULT 'replace';
```

Superseded cards render as a collapsed stub by default — title, revision count, an
expand control — so a long chain doesn't swamp the canvas. Expanding shows the old
content read-only.

**Annotations in `branch` mode stay attached to the card they were made on.** They are
not copied forward. `get_feedback` follows the supersede chain, so the agent still
receives them, and the UI surfaces them on the chain rather than the stub.

A useful side effect: **`branch` mode never produces stale annotations.** The old card
is immutable once superseded, so `card_rev` can't drift and every pin keeps pointing at
the content it was about. If §2.3 staleness turns out to bite in practice, switching a
space to `branch` is the fix.

Rough guidance: `replace` for iterative work where you only care about the current
state; `branch` when you're comparing options or want to see what the agent changed.

### 2.3 Annotation selectors

Loosely shaped after the [W3C Web Annotation model](https://www.w3.org/TR/annotation-model/)
so real selectors can drop in later, but v1 only implements two:

```jsonc
null                                        // whole card
{"type": "point", "x": 0.34, "y": 0.71}     // fraction of the content region
{"type": "rect",  "x": 0.1, "y": 0.2, "w": 0.3, "h": 0.25}
```

Normalized fractions of the card's **content region** — the scrollable body,
excluding header and footer — not of the visible box, so a pin stays on the content
it was placed on when the card scrolls (issue #23). A card whose content does not
scroll measures identically either way. Fractions, not pixels, so annotations
survive resize. Deliberately *not* doing CSS/text selectors in v1 — add
`{"type":"css", ...}` later without touching anything else.

Fractions need a renderer to mean anything, so agents cannot resolve them alone.
The UI therefore resolves the text under a dropped pin at creation time and quotes
it into the comment body (as a blockquote) — the body is the channel agents already
read, and the human reviews the quote before sending.

**Staleness.** Fractions survive a resize but not a content rewrite: once the agent
replaces a card's content, a pin may point at nothing. So annotations record the
`card_rev` they were made against, and anything where `annotation.card_rev <
card.sp_rev` is marked `stale: true` in API responses and greyed in the UI.

Stale annotations are **not** auto-resolved. Usually stale means "the agent already
fixed this," but sometimes it means "the agent rewrote around it," and only the human
can tell those apart.

`motivation` matters to agents: `commenting` is FYI, `assessing` is a verdict,
`editing` is an instruction. Default `commenting`.

---

## 3. HTTP API

Base: `/api`. No auth in v1 — bind to `127.0.0.1`. Add a single shared bearer token
from an env var when you first expose it beyond localhost (one middleware, ten lines).

Every mutating call **requires** `actor` and `actor_kind` (query params or headers);
`400` without them. Each appends exactly one row to `event`.

`PATCH` accepts an optional `If-Match: <sp_rev>` header. On mismatch it returns `409`
with the current node in the body; absent, last-write-wins. Both the agent and the UI
read a card before editing it, so sending it costs nothing. v1 only needs to *surface*
a 409, not resolve it cleverly.

```
POST   /spaces                        {slug, title}            → Space
GET    /spaces                                                 → [Space]
GET    /spaces/:slug                                           → Space + counts
DELETE /spaces/:slug

GET    /spaces/:slug/canvas                                    → {nodes:[], edges:[]}
POST   /spaces/:slug/import           {nodes, edges}           → additive only

POST   /spaces/:slug/cards            {nodes:[…]}              → bulk create
PATCH  /spaces/:slug/cards/:id        {partial node}           → bumps sp_rev
DELETE /spaces/:slug/cards/:id

POST   /spaces/:slug/links            {edges:[…]}              → bulk create
DELETE /spaces/:slug/links/:id

GET    /spaces/:slug/annotations      ?resolved=
POST   /spaces/:slug/annotations      {card_id, selector, body, motivation}
PATCH  /spaces/:slug/annotations/:id  {resolved, reply}

GET    /spaces/:slug/events           ?since=<seq>&limit=      → {events:[], cursor}
GET    /spaces/:slug/events/stream                             → SSE

POST   /spaces/:slug/media            multipart                → {url}
```

**`GET /events?since=` is the load-bearing endpoint.** It returns events in seq order
plus a `cursor`. Everything else is convenience.

**There is no whole-canvas replace.** `POST /import` creates cards and links and never
deletes; deletion is always an explicit `DELETE`. Destructive bulk semantics were the
most dangerous thing in the API and the most complex thing in WP1, and nothing in the
core loop needs them. Export (§4.2) stays, so round-tripping through a `.canvas` file
still works — it just merges rather than overwrites.

---

## 4. Agent interfaces

Three surfaces ship in the box, all thin clients over §3. They share one module,
`client/` — a typed HTTP client generated from `contracts/openapi.json`. Neither the
MCP server nor the CLI contains business logic; if you find yourself writing a rule in
one of them, it belongs in the server.

```
        MCP (stdio)  ─┐
        CLI (shell)  ─┼─→ client/ ─→ HTTP API ─→ store + event log
        raw HTTP     ─┘
```

Rule of thumb: **MCP for agents with config, CLI for agents with a shell, skill to
teach either one the workflow.**

### 4.1 MCP server

Ten tools. [FastMCP](https://github.com/jlowin/fastmcp), stdio transport.

| Tool | Signature | Notes |
|---|---|---|
| `list_spaces` | `()` | |
| `create_space` | `(slug, title)` | |
| `read_space` | `(slug)` | returns nodes + edges + open annotations |
| `add_cards` | `(slug, cards[])` | `card = {title, content, kind, x?, y?}` — auto-layout if x/y omitted |
| `update_card` | `(slug, id, patch)` | |
| `delete_card` | `(slug, id)` | |
| `link_cards` | `(slug, from, to, label?)` | |
| **`get_feedback`** | `(slug, since?)` | **see below** |
| `resolve_annotation` | `(id, reply?)` | |
| `await_feedback` | `(slug, since, timeout_s)` | long-poll; for resident agents |

`add_cards` takes friendly args, not raw JSON Canvas nodes — agents shouldn't have to
compute geometry. Server assigns positions on a grid with collision avoidance if `x`/`y`
are omitted. Crude is fine; the human will rearrange anyway.

### `get_feedback` — the contract

Two different mechanisms, deliberately:

- **Annotations are never governed by the cursor.** Every call returns *all unresolved
  annotations*, every time. `resolved` is already a durable per-item acknowledgment,
  and a better one than a cursor: if an agent reads feedback and then crashes, your
  comments come back next time instead of vanishing.
- **Card, link and reply deltas are governed by a server-side per-actor cursor**,
  advanced on read. Missing one of these once is survivable — the agent can
  `read_space` to resync — and it keeps agents stateless, which matters because a
  fresh session has nowhere sensible to persist `since=58`.

`replies` exists because a human's reply on resolve used to go nowhere: resolving is
the acknowledgment, and `resolved_reply` was read only by a human in the UI, so an
instruction typed beside the resolve button — the obvious gesture — was stored and
read by nobody. A reply is now delivered once, cursor-governed, to the resolver's
counterpart. A resolve **without** a reply stays pure acknowledgment and lands in no
bucket.

Explicit `since=` stays available for replay and debugging.

Events authored by the calling actor are filtered out of its own deltas — an agent
must never read its own writes back as feedback.

```jsonc
// call:   get_feedback(slug="redesign")        // actor cursor was at 41
// return:
{
  "cursor": 58,
  "annotations": [                                  // ALL unresolved, cursor-independent
    {"id":"a_…","card_id":"c_…","card_title":"Option B",
     "selector":{"type":"point","x":0.3,"y":0.6},
     "body":"this axis is misleading","motivation":"editing","creator":"human",
     "stale": false}
  ],
  "replies": [                                      // your comments they resolved with an answer
    {"id":"a_…","card_id":"c_…","card_title":"Option B",
     "body":"this axis is misleading","motivation":"editing",
     "creator":"human","creator_kind":"human",
     "reply":"rebased axis at 0","actor":"human","resolved_at":"…"}
  ],
  "cards_edited":   [{"id":"c_…","title":"Option B","changed":["text","width"]}],
  "cards_deleted":  [{"id":"c_…","title":"Option D"}],
  "cards_moved":    [{"id":"c_…","title":"Option A"}],
  "links_added":    [{"from":"c_…","to":"c_…","label":"contradicts"}],
  "links_removed":  [],
  "summary": "3 comments, 1 card edited, 1 deleted, 1 new link."
}
```

Position-only changes are bucketed separately as `cards_moved` — the human dragging
things around is usually noise, and the agent should be able to ignore it cheaply.

The returned `cursor` is informational — agents don't need to store it.

### 4.2 CLI

`analog`. Same ten operations, plus a couple only useful from a shell. Exists because
not every agent has MCP config: CI steps, shell-only agents, `bash` tool calls, and you
at a terminal. It's also the substrate the skill teaches, which means one set of
instructions works across every agent that can run a command.

```bash
analog spaces                                  # list
analog new redesign --title "Nav redesign"
analog open redesign                           # print the URL, --browser to launch

analog feedback redesign                       # ← the important one
analog feedback redesign --json                # machine-readable, same shape as §4.1
analog feedback redesign --watch               # blocks on SSE, prints as it arrives

analog add redesign --title "Option E" --kind md --file draft.md
cat chart.svg | analog add redesign --title "Revenue" --kind svg -
analog add redesign --title "Prototype" --kind html --file out/index.html

analog cards redesign                          # id, title, kind, created_by
analog update redesign c_7f --file fixed.svg
analog update redesign c_7f --file fixed.svg --mode branch   # keep the old card
analog rm redesign c_7f
analog link redesign c_a c_c --label "depends on"
analog resolve a_7f --reply "rebased axis at 0"

analog export redesign > redesign.canvas       # JSON Canvas, opens in Obsidian
analog import redesign < redesign.canvas

analog onboard claude-code --issue --claude-env ~/src/my-project --verbose
```

Design notes:

- `-` as a filename means stdin, so agents can pipe generated content straight in
  without a temp file.
- Default output is human-readable; `--json` on every read command.
- Config from `ANALOG_URL` and `ANALOG_ACTOR` env vars, or `~/.analog.toml`.
  `ANALOG_ACTOR` is what populates `sp_created_by` and keys the actor cursor — set it
  per agent (`claude-code`, `codex`, `researcher-1`) or the cursors collapse into one.
- Exit non-zero on error with a message on stderr, so agents notice failures.
- `analog feedback` with no new events prints nothing and exits 0. Silence means
  nothing changed.
- `analog onboard <actor>` is the one-command setup the README promises: a token
  (`--issue`, which composes `token add` and so runs on the server host), and the
  wiring: `--config-via skill|mcp|skip` (default `skill`: install the skill into
  `~/.claude/skills`, skipped when it is already there; `--config-dir DIR`
  overrides the destination and overwrites), plus `--claude-env`, `--wrapper`,
  and `--verbose` for the printed instructions. The skill is embedded in the
  binary (§4.3), so a release can onboard with no checkout.

### 4.3 Skill

Ship `skill/analog/SKILL.md` — an agent skill in the standard folder format,
copied into `.claude/skills/` or an equivalent path. It teaches the *workflow*, not the
API; the CLI's `--help` covers syntax. The `analog` binary embeds a copy of it, so
`analog onboard` works from a bare release and the taught workflow
cannot drift from the binary that serves it.

Why a skill rather than just an `AGENTS.md` paragraph: skills load on demand, so the
instructions aren't burning context in every unrelated session, and one skill folder
works across agents that support the format. It also gives you somewhere to put the
conventions that actually determine whether Analog stays usable.

```md
---
name: analog
description: Use when working in a shared Analog space with a human reviewer —
  posting work for review, reading back human comments, or when the user mentions a
  space slug or asks you to "put it on Analog".
---

# Analog

A shared canvas. You write cards, the human annotates them, you read the annotations.
Space slug is in the project's AGENTS.md, or ask.

## Every session, first

    analog feedback <slug>

Nothing printed means nothing changed. Otherwise:
- `motivation: editing` — an instruction. Do it.
- `motivation: assessing` — a verdict. Don't argue; adjust.
- `motivation: commenting` — context. Read it, no action required.
- deleted cards — the human rejected that idea. Don't re-add it.
- new links — the human sees a relationship you didn't. Consider why.

Call `analog resolve <id> --reply "..."` for each one you act on. Unresolved
annotations are how the human tracks what you've ignored.

## Posting work

    analog add <slug> --title "..." --kind md --file draft.md

- **One idea per card.** A wall of text in a single card cannot be annotated
  usefully, which defeats the point.
- Use `--kind html` or `--kind svg` for anything visual. It renders; the human
  can pin comments on specific regions of it.
- `analog link <slug> <from> <to> --label "..."` whenever cards relate.
  Always label. Unlabelled edges are noise.
- Don't post status updates or narration. Analog is for artifacts under
  review, not a log.

## Don't

- Don't delete or edit cards the human created — annotate them instead.
- Don't resolve annotations you haven't acted on.
- Don't rearrange the canvas. Positions are the human's.
```

The last three lines matter more than they look. Agents tidying a canvas the human
spatially organized is the fastest way to make this tool annoying.

---

## 5. Frontend

React + Vite. One route: `/s/:slug`.

- **Canvas**: pan/zoom via CSS transform on a container. Cards absolutely positioned.
  Drag with raw pointer events — no drag library needed for v1.
- **Links**: one `<svg>` layer beneath cards; each edge is a bezier between anchor
  points. Draw a new link by dragging from a card's edge handle.
- **Card rendering** by `sp_kind`:
  - `plain` → `<pre>`
  - `md` → `react-markdown` with GFM and KaTeX (`$...$`, `$$...$$`)
  - `image` → `<img>`
  - `svg` → inlined, sanitized
  - `html` → `<iframe sandbox="allow-scripts" srcdoc={...}>`, **no** `allow-same-origin`
- **Annotation layer**: a transparent overlay *above* each card, in the parent
  document. Click to drop a pin, shift-drag for a rect, type a comment. Because the
  overlay is in the parent frame and the iframe has no same-origin access, agent HTML
  can neither read nor forge annotations.
- **Editing**: double-click a card to edit its text inline. Human edits `PATCH` and
  bump `sp_rev`.
- **HTML cards scroll**, they don't scale — an iframe with its own scrollbar, plus a
  pop-out control for a full-window view. Scaling a page down to card size makes text
  unreadable, which defeats the purpose of rendering it.
- **Auto-layout**: cards created without `x`/`y` go in a column to the right of the
  current bounding box, top-down. Deterministic and dull; with one agent per space
  there is nothing to collide with.
- **Stale annotations** render greyed with a "content changed since" tooltip.
- **Activity sidebar**: a collapsible right-hand panel reading the event log in
  reverse order — "agent added 3 cards · 11:04", "you deleted Option D · 11:12".
  Grouped by actor and coalesced within a short window so a bulk `add_cards` is one
  line, not eight. Clicking an entry pans the canvas to the subject card. Collapsed
  by default; the canvas is the primary view, the feed is for catching up on what
  happened while you were away.
- **Live**: subscribe to `/events/stream` (SSE), apply events to local state. Falls
  back to 2s polling if the stream drops.

Region annotation on `image`/`svg` cards can use
[Annotorious](https://annotorious.dev/) (BSD-3, emits W3C annotations natively) if
hand-rolling the overlay proves annoying. Optional.

**Conflict handling:** last-write-wins on `sp_rev` mismatch, with a toast. Do not
build CRDTs. The human and an agent editing the same card in the same second is rare,
and the event log makes it recoverable. If this genuinely becomes a problem, swap in
Yjs + Hocuspocus later — the node-blob storage makes that a contained change.

---

## 6. Repo layout

```
analog/
  server/           FastAPI + SQLite
    main.py         routes
    models.py       pydantic + openjsoncanvas
    store.py        db access, event emission
    events.py       SSE
    schema.sql
  client/
    __init__.py     typed HTTP client, generated from openapi.json
  mcp/
    server.py       FastMCP over client/
  cli/
    main.py         `analog` entrypoint over client/ (typer)
  skill/
    analog/
      SKILL.md
  web/
    src/
      Canvas.tsx    pan/zoom, layout
      Card.tsx      renders by sp_kind
      Links.tsx     svg edge layer
      Annotations.tsx
      Activity.tsx   event-log sidebar
      api.ts        generated from openapi.json
  contracts/
    openapi.json    ← frozen first, single source of truth
    fixtures/       sample spaces for testing without a server
  docker-compose.yml
```

---

## 7. Work packages (parallel agents)

**WP0 must be completed and frozen by a single agent before anything else starts.**
Everyone else codes against `contracts/`, and no agent edits another WP's directory.

| WP | Owner scope | Depends on | Done when |
|---|---|---|---|
| **WP0 Contracts** | `contracts/`, `server/schema.sql` | — | `openapi.json` covers every §3 endpoint; fixtures contain a 6-card space with 4 links and 3 annotations; schema.sql applies cleanly |
| **WP1 Backend** | `server/` | WP0 | All §3 endpoints pass a contract test suite; every mutation emits exactly one event; `?since=` returns correct deltas; both `revision_mode` values behave per §2.4 |
| **WP2a Client** | `client/` | WP0 only | Every §3 endpoint callable; typed; retries once on connection error; unit-tested against fixtures |
| **WP2b MCP** | `mcp/` | WP2a | All 10 tools callable; `get_feedback` returns the §4.1 shape; works against a mocked client |
| **WP2c CLI + skill** | `cli/`, `skill/` | WP2a | Every §4.2 command works incl. stdin `-`, `--json`, `--watch`, `--mode`; `ANALOG_ACTOR` drives `sp_created_by`; SKILL.md present |
| **WP3 Canvas UI** | `web/src/Canvas.tsx, Card.tsx, Links.tsx` | WP0 | Renders fixture space; pan/zoom/drag/resize; create+delete cards and links; all `sp_kind` renderers work; superseded cards collapse to stubs |
| **WP4 Annotation UI** | `web/src/Annotations.tsx` | WP0 | Pin and rect annotation on any card kind; comment list panel; resolve; stale marking; correct behaviour over a sandboxed iframe card |
| **WP4b Activity sidebar** | `web/src/Activity.tsx` | WP0 | Renders the event log reverse-chronologically from fixtures; coalesces bulk writes; click-to-pan; collapsed by default |
| **WP5 Live + glue** | `web/src/api.ts`, `events.py`, compose | WP1 | SSE updates the canvas without reload; docker-compose brings up server+web+mcp |
| **WP6 Smoke** | `tests/` | WP1–5 | End-to-end: MCP adds cards → browser shows them → annotation via API → `get_feedback` returns it with correct cursor |

After WP0: WP1, WP2a, WP3, WP4, WP4b start in parallel. WP2b and WP2c both unblock the
moment WP2a lands and can then run alongside each other. Nothing outside `web/`
touches `web/`.

### Acceptance demo

One script, start to finish:

```
1. create_space("demo")
2. Agent A: add_cards — 4 option cards + 1 html chart card
3. Agent A: link_cards — "option B" contradicts "option D"
4. Human (browser): drags cards, deletes option D, pins a comment on the chart
   reading "y-axis starts at 40, fix", adds a link A→C labelled "depends on"
5. Agent B — a *different* agent, reaching the same space over the CLI rather than
   MCP: `analog feedback demo` → sees the comment, the deletion, and both links
6. Agent B: `cat fixed.svg | analog add demo --kind svg -`, then
   `analog resolve a_7f --reply "rebased axis at 0"`
7. Agent A calls get_feedback again → sees the human's edits (its own cursor is
   independent of Agent B's), and does NOT see Agent B's writes replayed as feedback
```

If that runs, the tool works.

---

## 8. Explicitly out of scope for v1

Kept out on purpose; each is additive later.

- Auth, users, permissions (single shared token when needed)
- CRDT / true simultaneous co-editing
- Separate-origin artifact serving (v1 uses `srcdoc` + sandbox; upgrade to a
  distinct origin per space before this touches a network)
- Card version history and rollback (the event log already records enough to add it)
- Search
- Webhooks (SSE covers the resident-agent case; add webhooks when something external
  needs push)
- Threaded replies on annotations (flat comments + `reply` on resolve is enough)
- Mobile layout

## 9. Decisions that were arbitrary

Flagged so nobody treats them as load-bearing:

- SQLite over Postgres — swap when concurrent writers hurt.
- Whole-node JSON blobs over normalized columns — chosen for schema stability.
- Normalized-fraction selectors over W3C CSS/text selectors — chosen for speed; the
  UI folds a quote of the targeted text into the comment body to bridge the gap.
- SSE over WebSocket — one-directional is all we need; writes go over HTTP.
- FastAPI/Python for the server because the MCP ecosystem is Python-heavy; Hono/TS
  would be equally fine and would let the whole thing share types with the frontend.

## 10. Decisions that were NOT arbitrary

These were considered and settled; don't relitigate without a reason.

| Decision | Why |
|---|---|
| Annotations excluded from cursor semantics | `resolved` is a durable per-item ack; a crash after reading must not lose the human's comments |
| Cursor governs card/link deltas only, advanced on read | Keeps agents stateless; a missed delta is recoverable via `read_space` |
| Agent's own events filtered from its own deltas | Otherwise an agent reads its own writes back as feedback |
| No whole-canvas replace | Destructive bulk semantics were the biggest footgun in the API for the least benefit |
| `actor` mandatory, no default | A misconfigured agent should fail loudly, not write anonymously |
| `If-Match` optional, 409 surfaced but not auto-resolved | Cheap real signal; clever merge logic isn't worth it at v1 |
| Stale annotations flagged, never auto-resolved | "Agent fixed it" and "agent rewrote around it" look identical to a machine |
| Text nodes inline, file nodes for binary | Interop — file nodes render on Obsidian export |
| HTML cards scroll rather than scale | Scaled-down pages are unreadable |
