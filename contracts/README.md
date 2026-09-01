# contracts/ — frozen

This directory is owned by WP0. **No other work package may edit it.**

Everything else in the repo is generated from or tested against these files. If you
are working on WP1–WP6 and you need something that isn't here, stop and request an
amendment — do not add it locally. A field invented in one work package and not in
another is the exact failure this directory exists to prevent.

## Contents

| File | What it is |
|---|---|
| `openapi.json` | Every endpoint. Validated 3.1.0. The frontend generates `web/src/api.ts` from it; `client/` generates its types from it. |
| `../internal/store/schema.sql` | Storage. Field names and types settled. |
| `fixtures/space.json` | Space metadata for slug `redesign`. |
| `fixtures/canvas.json` | 6 live cards, 4 links. Valid JSON Canvas 1.0. |
| `fixtures/canvas.with-deleted.json` | Same plus the soft-deleted card. |
| `fixtures/annotations.json` | 3 annotations: one stale, one current, one resolved. |
| `fixtures/events.json` | 19 events, contiguous seq. |
| `fixtures/feedback.claude-code.since-12.json` | Expected `GET /feedback` for actor `claude-code`. |
| `fixtures/feedback.human.json` | Expected `GET /feedback` for actor `human`, who has no stored cursor and so starts at zero. |

## What the fixtures deliberately exercise

The `redesign` fixture is not decorative — each piece pins down a rule that is easy
to get wrong:

- **Own-event filtering.** Events 18 and 19 are authored by `claude-code`. Neither
  appears in that actor's feedback. An agent must never read its own writes back.
- **Reply delivery.** Event 18 resolved `a_3` with the reply `added position:sticky`.
  For `human`, whose window includes it, that is one entry in `replies` — the answer
  reaches the resolver's counterpart exactly once. For `claude-code` itself the
  resolve is its own event, so its `replies` is empty: nobody reads their own reply
  back. A resolve without a reply is pure acknowledgment and lands in no bucket.
- **Cursor-independent annotations.** `a_1` was created at seq 12, before the cursor.
  It still appears, because unresolved annotations ignore the cursor entirely.
- **Resolved exclusion.** `a_3` is resolved and therefore absent.
- **Staleness.** `a_1` has `card_rev: 1`; `c_chart` is now `sp_rev: 2` (event 19), so
  `stale: true`. `a_2` matches its card's rev, so `stale: false`.
- **`card.moved` vs `card.updated`.** Event 15 moved `c_opt_a` without bumping its
  rev, and lands in `cards_moved`, not `cards_edited`.
- **Soft delete.** `c_opt_d` is absent from `canvas.json`, present in
  `canvas.with-deleted.json`, and reported in `cards_deleted`.
- **All four render paths.** `md`, `svg`, `html` text nodes plus one `file` node.
  The `html` card contains a `<script>` — it must render inside the sandbox and must
  not execute in the parent frame.

## Working without a server

That's the point. WP2b asserts its `get_feedback` output equals
`feedback.claude-code.since-12.json` with a mocked client. WP3 renders
`canvas.json` with no database. WP4 tests stale marking against `annotations.json`.
Nothing waits on WP1.

## Amendments

1. Open an issue describing the gap and which work packages it affects.
2. WP0 edits `openapi.json` / `schema.sql` / fixtures together — never one alone.
3. Bump `info.version`.
4. Everyone re-generates clients.

A contract that stops describing the running system is worse than no contract,
because people still trust it.

## Applied amendments

### 0.2.0 — 2026-08-29

Requested in `../AMENDMENTS.md`, approved on the `decisions` space.

| # | Change |
|---|---|
| 1 | `GET /spaces/{slug}/media/{filename}` (`getMedia`) added. A file node's `file` URL points at it, so without it a generated client cannot render one. |
| 2 | `Node.sp_deleted_at` added, read-only, present only under `include_deleted=true`. |
| 4 | `updateCard` and `schema.sql` note 4 now state that a branch-mode content change emits `card.created` + `link.created` and no `card.updated`. |
| 5 | `space.created` and `space.deleted` added to the `event.type` enum here and in `schema.sql`. `space.created` is seq 1 of every new space; `space.deleted` reaches live subscribers but does not survive its own cascade (`schema.sql` note 5). |
| 6 | `Annotation.card_superseded_by` added, so an agent can follow a supersede chain without a second `GET /canvas`. |

Fixtures were **not** renumbered: they depict a space whose log begins at
`card.created`, which stays valid — `link.deleted` has likewise always been an enum
member with no fixture.

### 0.2.1 — 2026-08-29

| # | Change |
|---|---|
| 3 | `canvas.with-deleted.json`: `c_opt_d.sp_deleted_at` `11:00:00Z` → `14:00:00Z`, to agree with event 14, the `card.deleted` that produced it. 11:00 was the card's creation time and had been copied by mistake. |

The only fixture edit so far. `scripts/seed.py` reads the value straight from the
fixture, so re-seeding is all that is needed to pick it up.

### 0.3.0 — 2026-08-29 · going remote

SPEC §3 anticipated this ("add a single shared bearer token ... when you first expose
it beyond localhost") but a shared token gatekeeps the *server*, not *identity*, and
§2.2/§10 make `actor` load-bearing: an event log is only worth keeping if attribution
cannot simply be claimed. So a token identifies exactly one actor.

| Change |
|---|
| `securitySchemes.bearerAuth`, and a top-level `security` of `[{bearerAuth: []}, {}]` — a token is accepted, and a server with none configured is still a valid deployment. |
| `Error.error` gains `unauthorized` and `forbidden`. |
| `GET /health` — public, and the only public operation. A client cannot be asked to authenticate before it can discover that authentication exists. |
| `GET /whoami` — the actor a token writes as. The web UI reads it instead of hardcoding `human`, which §2.2 could assume only while the server was loopback-only. |

`actor` and `actor_kind` stay **required** on every mutation rather than being
inferred from the token. The contract says they are required, and SPEC §10 wants a
misconfigured agent to fail loudly — a silently corrected actor is not loud. A
declared actor that disagrees with the token is `403`.

The bearer token is deliberately not a cookie. A card's sandboxed iframe cannot set
an `Authorization` header, so agent-authored HTML has no ambient credential to ride;
a cookie would have handed it one. That is the concern SPEC §8 filed under
"separate-origin artifact serving ... before this touches a network".

No fixture changed: none of them contain a token or an identity.

### 0.4.0 — 2026-08-30 · replies on resolve reach the agent

Requested in `../AMENDMENTS.md` #9, filed as issue #22. Resolving is the durable
acknowledgment (§4.1), and `resolved_reply` was only ever read by a human in the
UI — so when the human typed an instruction into the reply box beside resolve
("okay. you fix it."), the agent never saw it. The affordance invited the gesture;
the contract swallowed it.

| Change |
|---|
| `Feedback.replies` added: comments *another* actor resolved with a non-empty reply since the cursor, one entry per resolve event, carrying the reply and the comment's context. Cursor-governed like the card/link deltas, so each is delivered exactly once and own-event filtering holds. |
| New schema `Reply`; `replies` is required on `Feedback`. |
| `fixtures/feedback.human.json` added — the same log viewed by `human` (no stored cursor, starts at zero), where event 18 arrives as one reply. `feedback.claude-code.since-12.json` gains only `"replies": []`: its window contains no other-actor resolve. |

Deliberately *not* changed: a resolve **without** a reply still lands in no bucket.
Resolving remains the acknowledgment; only an answer is a message. Whether
replies-on-resolve become a conversational channel is issue #22's open question.

### 0.6.0 — 2026-08-31 · selectors anchor to content, not the visible box

Requested in `../AMENDMENTS.md` #8, filed as issue #23. A pin was stored as a
fraction of the *visible* card box, so the moment the body scrolled the pin stayed
glued to the box and pointed at whatever content happened to pass under it. The
reference is now the card's **content region**: the scrollable body, excluding
header and footer.

| Change |
|---|
| `Selector.description` reworded. No shape changed: still `null \| point \| rect` with 0..1 fractions, and a card whose content does not scroll measures identically either way. |

No fixture changed: the wire JSON is untouched; only what a fraction *means*.
Text-quote selectors (W3C `TextQuoteSelector`) were considered and declined — the
content-relative rect covers "this part", and quote anchoring adds a staleness
problem (reflow) that `card_rev` cannot express. Issue #23 tracks the anchoring fix.

## One correction to the spec

`GET /spaces/{slug}/feedback` is in `openapi.json` but was not in §3 of the build
spec, where `get_feedback` appeared only as an MCP tool. Delta computation is
business logic and belongs in the server; MCP and the CLI are thin proxies over this
endpoint. Otherwise the rule would have to be implemented twice and would drift.

`advance=false` was added at the same time so a client can peek without consuming the
cursor — useful for the CLI's `--watch` and for debugging.
