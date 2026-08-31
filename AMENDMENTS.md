# Contract amendment requests

`contracts/` and `server/schema.sql` are frozen, so these were **requests, not
changes**. Each says what I implemented in the meantime so nothing was blocked.

**#1, #2, #4, #5 and #6 were approved on 2026-08-29** (on the `decisions` space) and
are applied: `openapi.json` is at **0.2.0**, `schema.sql` gained the two `space.*`
event types, and the server matches. **#3 was approved and applied on the same space (0.2.1).** **#8 is open.**
**#9 was approved and applied in 0.4.0** (issue #22).
**#10 and #11 came out of going remote and are applied in 0.3.0.**

---

### 1. `GET /spaces/{slug}/media/{filename}` — APPLIED in 0.2.0

`POST /media` returns a `url`, and `contracts/fixtures/canvas.json` contains a file
node pointing at `/api/spaces/redesign/media/m_01.png`. No operation in
`openapi.json` serves those bytes, so a client generated from the contract cannot
render a single `file` node.

**Implemented:** `GET /spaces/{slug}/media/{filename}` → the bytes with their stored
content type, `404` otherwise. Scoped to the space, so one space's media is not
reachable through another's path.

**Applied:** added as `getMedia`, filename constrained to `^[A-Za-z0-9_.-]{1,128}$`.

---

### 2. `sp_deleted_at` absent from `Node` — APPLIED in 0.2.0

`canvas.with-deleted.json` sets `sp_deleted_at` on `c_opt_d`. `Node` has
`additionalProperties: true` so it validates, but it is not discoverable from the
contract, and it is the only signal that distinguishes a tombstone from a live card
in an `include_deleted=true` response.

**Implemented:** projected at read time from `card.deleted_at`, present only when
`include_deleted=true`.

**Applied:** added to `Node.properties` as `readOnly`, documented as present only
under `include_deleted=true`.

---

### 3. `c_opt_d`'s tombstone disagreed with its deletion event — APPLIED in 0.2.1

`canvas.with-deleted.json` has `sp_deleted_at: "2026-08-28T11:00:00Z"`, but
`events.json` event 14 (`card.deleted`, subject `c_opt_d`) has
`ts: "2026-08-28T14:00:00Z"`. Both cannot be right. The node also carries a creation
event at 11:00 (event 4), so the tombstone appears to have copied the creation time.

**Implemented:** `scripts/seed.py` honours the node's `sp_deleted_at`, because
`canvas.with-deleted.json` is what WP3 renders against and the roundtrip test
compares byte-for-byte.

**Ask:** set `sp_deleted_at` to `2026-08-28T14:00:00Z` to match the event.

**Applied 2026-08-29** ("okay. you fix it."): `canvas.with-deleted.json` now says
`14:00:00Z`, matching event 14. `scripts/seed.py` reads the fixture directly, so a
re-seed is the whole migration.

---

### 4. A branch-mode `PATCH` cannot emit exactly one event — APPLIED in 0.2.0

WP1's acceptance criterion is "every mutation emits exactly one event". A branching
`PATCH` (SPEC §2.4) creates a card *and* an auto-link labelled `revised`. Emitting
one event means either the auto-link appears on the canvas with nothing in the log
to explain it — invisible to the activity sidebar and to `get_feedback` — or the
supersede pointer is written as `card.updated` on the old card, which bumps its rev
and contradicts `schema.sql` note 4 ("its rev freezes, and its annotations can never
go stale").

**Implemented:** two events, `card.created` then `link.created`. Setting
`sp_superseded_by` on the old card is bookkeeping and emits nothing, so its rev
stays frozen.

**Applied:** `updateCard`'s description and `schema.sql` note 4 now state the
two-event behaviour, and the top-level API description reads "emits an event for each
object it creates or changes".

---

### 5. Space mutations emitted no event — APPLIED in 0.2.0

SPEC §3 says every mutating call "appends exactly one row to `event`". The
`event.type` CHECK constraint in `schema.sql` has no `space.*` member, and the event
table is keyed by `space_id`, so a space deletion cascades its own log away.

**Implemented:** `actor`/`actor_kind` are required as the contract says, and no event
is written.

**Applied:** both added to the `event.type` enum in `schema.sql` and `openapi.json`.
`space.created` is emitted and is seq 1 of every new space. `space.deleted` is emitted
and pushed to live SSE subscribers — an open tab now leaves the space instead of
404ing — but the row does not survive its own cascade, recorded as `schema.sql` note 5.

**Still open, smaller:** retaining `space.deleted` needs soft-deleted spaces or a log
not keyed by `space_id`. Neither belongs in v1; say if you want it.

**Note:** the fixtures were not renumbered — they depict a space whose log starts at
`card.created`, which is still valid. `link.deleted` has likewise always been an enum
member with no fixture.

---

### 6. `Feedback` cannot express the supersede chain — APPLIED in 0.2.0

SPEC §2.4 says `get_feedback` "follows the supersede chain" for annotations on
superseded cards. `Annotation` carries `card_id` and `card_title` only, with no field
naming the current head of the chain, so an agent receiving a comment on a
superseded card has no contract-visible way to find the card that replaced it
without a separate `GET /canvas`.

**Implemented:** annotations on superseded cards are returned unchanged. They are
unresolved, so they are returned regardless; nothing is lost, but the agent has to
look up the head itself.

**Applied:** `Annotation.card_superseded_by` added, populated in branch mode and
absent while the annotated card is current, so the shape the fixtures pin is
unchanged.

---

### 7. Non-blocking drift, noted only

- SPEC §2.2 shows a `rev` column on `link`; `schema.sql` has none. Nothing needs it.
  `schema.sql` is authoritative.
- `Event.payload` is absent on `link.created` events 8–10 but present on 16.
  Implemented: new links always carry `{from, to, label}`, which the activity
  sidebar needs. Seeded events are stored verbatim, so the fixture still round-trips.
- The `summary` example in SPEC §4.1 ("3 comments, 1 card edited, …") does not match
  the grammar in `feedback.claude-code.since-12.json` ("2 open comments (1 stale),
  …"). The fixture wins; the §4.1 line is illustrative.
- `CardDraft` has no way to express a `file` node, so uploading a screenshot and
  placing it needs the raw `nodes` form of `POST /cards`. Fine for v1.

---

### 8. Annotations cannot target a text selection — ANSWERED in 0.6.0

Raised on the `decisions` space: *"it seems that we are missing selected-then-comment
type of annotation, which targets the selected parts only."*

Two separate things behind that.

**Region annotation already existed and was undiscoverable.** Shift-drag on any card
draws a rect selector. Nothing in the UI said so — the hint bar listed six other
gestures and not that one. Fixed: it now says so while comment mode is on. That
covers "comment on this part of the chart", which is the case §2.3 was designed for.

**Text selection is genuinely not in v1, and it is a contract change.** §2.3 says so
outright: "Deliberately *not* doing CSS/text selectors in v1 — add
`{"type":"css", ...}` later without touching anything else." `Selector` is a frozen
`oneOf` of `null | point | rect`, so a text-quote selector needs a fourth branch, and
the UI needs to resolve a DOM `Range` to a quote and back. For `md` and `plain` cards
that is straightforward. For `html` cards it is not: the text lives inside a
sandboxed cross-origin iframe the parent cannot read a selection from — and that
isolation is exactly what makes agent-authored HTML safe to render at all.

**Ask:** confirm you want text-quote selectors, and whether v1 may restrict them to
text-node kinds (`md`, `plain`), leaving `html` cards with point and rect. That
restriction is what keeps the sandbox intact.

**Resolved (0.6.0, issue #23): text-quote selectors declined; the underlying defect
was anchoring, and that is fixed.** The discussion surfaced what was actually broken:
a pin was a fraction of the *visible* box, so scrolling a card left the pin glued to
the box, pointing at whatever content passed beneath — the feeling that a selection
"didn't stick". Selectors are now fractions of the card's **content region** and the
UI tracks scroll, so pins stay under what was pointed at; on `html` cards a tiny
script injected at render time reports scroll metrics out of the sandbox (it grants
the frame nothing). Since fractions still tell an agent nothing without a renderer,
the UI also captures the text under a dropped pin and quotes it into the comment
body — agents read bodies already, and the human reviews the quote before sending.
Wire shapes are unchanged; see `contracts/README.md` 0.6.0.

---

### 9. A human's reply on resolve never reaches the agent — APPLIED in 0.4.0

Found the hard way. You resolved amendment #3's comment with the reply
*"okay. you fix it."* — an instruction. `analog feedback decisions` did not show it.
I only found it by reading the raw event log and `analog comments --all`.

This is the design working as specified, and that is the problem. §4.1: every call
returns *all unresolved annotations*, and `resolved` is the durable acknowledgment.
So resolving is itself the signal, and `resolved_reply` is only ever read by a human
in the UI. The field exists for the agent→human direction ("rebased axis at 0").

The trap is that the UI puts a **reply box directly beside the resolve button** on
every open comment, including comments the human wrote themselves. Typing an
instruction there is the obvious move, and it silently goes nowhere. Either the
affordance or the contract is wrong.

Three ways out, cheapest first:

1. **UI only, no contract change.** Relabel the box "reply (for your own record)",
   or hide it on comments the human authored. Honest, and free.
2. **Feed replies back.** Add `replies` to `Feedback`: annotations resolved by
   *another* actor since the cursor, carrying `resolved_reply`. Cursor-governed, so
   each is delivered once. Fits the existing model; needs a `Feedback` field.
3. **Say resolve means done, and route instructions elsewhere.** In the UI, a reply
   on your own open comment becomes a *new* annotation instead of a resolution —
   which `get_feedback` already delivers, with no contract change at all.

I would take (3), with (1) as the fallback if it feels like sleight of hand.

**Ask:** which, or neither.

**Applied, as (2) — decided on issue #22, 2026-08-30:** `Feedback` gains `replies`:
comments *another* actor resolved with a non-empty reply since the cursor,
delivered exactly once. Resolving without a reply stays pure acknowledgment and
lands in no bucket, so the acknowledgment model is untouched — only an answer is
now a message. The expensive option after all: the question was whether
replies-on-resolve are a real channel, and swallowing an instruction was the one
failure mode this design exists to prevent. See `contracts/README.md` 0.4.0.

---

### 10. Going remote needs auth the contract could not express — APPLIED in 0.3.0

SPEC §3: "Add a single shared bearer token from an env var when you first expose it
beyond localhost (one middleware, ten lines)." That gatekeeps the server. It does not
gatekeep *identity* — anyone holding the shared token could still write as any actor,
and §2.2/§10 spend real effort making `actor` trustworthy. On loopback the query
param was fine because only you could reach the port. On a network it is a claim.

**Applied:** a token identifies exactly one `(actor, actor_kind)`. `actor` and
`actor_kind` stay required on every mutation — the contract says so, and SPEC §10
wants a misconfigured agent to fail loudly, which a silently corrected actor is not —
but a claim that disagrees with the token is `403`. Contract gains `bearerAuth`,
`unauthorized`/`forbidden`, `GET /health` (public) and `GET /whoami`.

**Still open, and yours to decide later:** there is no way to *rotate* a token
without a window where the old one is dead and the new one has not reached the agent.
`analog token add <same actor>` revokes the old one immediately. Fine for two actors,
irritating for twenty.

---

### 11. `actor=human` was a loopback assumption — APPLIED in 0.3.0

SPEC §2.2: "The web UI hardcodes `human`." True while the only person who could
reach the server was the person running it. With tokens, two people can hold two
tokens, and both would have written as `human` — indistinguishable in the event log,
and sharing one cursor.

**Applied:** the UI calls `GET /whoami` and writes as whatever the token says. On a
server with no tokens it still says `human`, so nothing about the loopback case
changed and no fixture moved.

**Consequence worth knowing:** `human` is now just a name. `analog token add kai
--kind human` produces an event log that says `kai`, not `human`. Existing spaces
keep whatever they recorded.

---

### 12. `schema.sql` moved, and it is still frozen — APPLIED

Not a contract change: the file's bytes are untouched and every test that pins it
still passes. But `contracts/README.md` and the freeze instruction both name a path,
and that path moved, so it is recorded here rather than done quietly.

`server/schema.sql` → **`analog/server/schema.sql`**.

**Why.** A wheel built from the old layout claimed four top-level names — `server`,
`client`, `cli`, `mcp_server` — so installing Analog put `import server` and
`import client` into site-packages. That is the same failure this repo already hit
once from the other direction (a top-level `mcp/` shadowing the `mcp` package
FastMCP imports; see DECISIONS.md), and pointing it outward at anyone who installs
the project is worse. One top-level name, `analog`, fixes it.

Done before the first release, because after one it is an import-breaking change
with users attached.

`contracts/README.md` was edited for the pointer only. `SPEC.md` was left alone: it
is the brief this was built from, and a brief that gets quietly edited to match what
happened is no longer a record of anything.
