# Contract amendment requests

`contracts/` and `server/schema.sql` are frozen, so these were **requests, not
changes**. Each says what I implemented in the meantime so nothing was blocked.

**#1, #2, #4, #5 and #6 were approved on 2026-08-29** (on the `decisions` space) and
are applied: `openapi.json` is at **0.2.0**, `schema.sql` gained the two `space.*`
event types, and the server matches. **#3 is still open.** **#8 is new.**

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

### 3. `c_opt_d`'s tombstone disagrees with its deletion event — STILL OPEN

`canvas.with-deleted.json` has `sp_deleted_at: "2026-08-28T11:00:00Z"`, but
`events.json` event 14 (`card.deleted`, subject `c_opt_d`) has
`ts: "2026-08-28T14:00:00Z"`. Both cannot be right. The node also carries a creation
event at 11:00 (event 4), so the tombstone appears to have copied the creation time.

**Implemented:** `scripts/seed.py` honours the node's `sp_deleted_at`, because
`canvas.with-deleted.json` is what WP3 renders against and the roundtrip test
compares byte-for-byte.

**Ask:** set `sp_deleted_at` to `2026-08-28T14:00:00Z` to match the event.

**2026-08-29 — you said "i don't understand".** Restated: the fixture says this card
was deleted at 11:00, while the deletion event in the same fixture set says 14:00.
One of the two is a typo, and 11:00 happens to be the card's *creation* time, so it
looks copied from there. Nothing depends on which wins — it is one character in one
fixture — but a fixture that contradicts itself gets trusted by someone eventually.
Say the word and I will change it to 14:00.

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

### 8. Annotations cannot target a text selection — new, unanswered

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
