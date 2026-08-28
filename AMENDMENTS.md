# Contract amendment requests

`contracts/` and `server/schema.sql` are frozen, so these are **requests, not
changes**. Each says what I implemented in the meantime so nothing is blocked.
Per `contracts/README.md`, adopting any of them means editing `openapi.json` /
`schema.sql` / fixtures together and bumping `info.version`.

---

### 1. `GET /spaces/{slug}/media/{filename}` is undocumented — *needed*

`POST /media` returns a `url`, and `contracts/fixtures/canvas.json` contains a file
node pointing at `/api/spaces/redesign/media/m_01.png`. No operation in
`openapi.json` serves those bytes, so a client generated from the contract cannot
render a single `file` node.

**Implemented:** `GET /spaces/{slug}/media/{filename}` → the bytes with their stored
content type, `404` otherwise. Scoped to the space, so one space's media is not
reachable through another's path.

**Ask:** add the operation to `openapi.json`.

---

### 2. `sp_deleted_at` is used by a fixture but absent from the `Node` schema

`canvas.with-deleted.json` sets `sp_deleted_at` on `c_opt_d`. `Node` has
`additionalProperties: true` so it validates, but it is not discoverable from the
contract, and it is the only signal that distinguishes a tombstone from a live card
in an `include_deleted=true` response.

**Implemented:** projected at read time from `card.deleted_at`, present only when
`include_deleted=true`.

**Ask:** add `sp_deleted_at` to `Node.properties` with a note that it is read-only
and computed.

---

### 3. `c_opt_d`'s tombstone disagrees with its deletion event

`canvas.with-deleted.json` has `sp_deleted_at: "2026-08-28T11:00:00Z"`, but
`events.json` event 14 (`card.deleted`, subject `c_opt_d`) has
`ts: "2026-08-28T14:00:00Z"`. Both cannot be right. The node also carries a creation
event at 11:00 (event 4), so the tombstone appears to have copied the creation time.

**Implemented:** `scripts/seed.py` honours the node's `sp_deleted_at`, because
`canvas.with-deleted.json` is what WP3 renders against and the roundtrip test
compares byte-for-byte.

**Ask:** set `sp_deleted_at` to `2026-08-28T14:00:00Z` to match the event.

---

### 4. A branch-mode `PATCH` cannot emit exactly one event

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

**Ask:** reword the criterion as "every mutation emits an event for every object it
creates or changes, and nothing else".

---

### 5. `POST /spaces` and `DELETE /spaces` require an actor but can emit no event

SPEC §3 says every mutating call "appends exactly one row to `event`". The
`event.type` CHECK constraint in `schema.sql` has no `space.*` member, and the event
table is keyed by `space_id`, so a space deletion cascades its own log away.

**Implemented:** `actor`/`actor_kind` are required as the contract says, and no event
is written.

**Ask:** either add `space.created` / `space.deleted` to the enum, or note the
exception in §3.

---

### 6. `Feedback` cannot express the supersede chain

SPEC §2.4 says `get_feedback` "follows the supersede chain" for annotations on
superseded cards. `Annotation` carries `card_id` and `card_title` only, with no field
naming the current head of the chain, so an agent receiving a comment on a
superseded card has no contract-visible way to find the card that replaced it
without a separate `GET /canvas`.

**Implemented:** annotations on superseded cards are returned unchanged. They are
unresolved, so they are returned regardless; nothing is lost, but the agent has to
look up the head itself.

**Ask:** add an optional `card_superseded_by` to `Annotation`, or confirm that "still
receives them" is all that was meant.

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
