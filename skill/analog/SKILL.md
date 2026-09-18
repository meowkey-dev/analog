---
name: analog
description: Use when working in a shared Analog space with a human reviewer —
  posting work for review, reading back human comments, or when the user mentions a
  space slug or asks you to "put it on Analog". Also when they ask for an
  infographic, an ELI5, a comparison or a diagram to review: an "analog-style"
  card (see STYLE.md). Also when they ask you to teach them a topic, or to
  explain what you did across a long session: a set of explainer cards
  (see EXPLAIN.md).
---

# Analog

A shared canvas. You write cards, the human annotates them, you read the annotations.
Space slug is in the project's AGENTS.md, or ask.

Set `ANALOG_ACTOR` to your own name (`claude-code`, `codex`, `researcher-1`) before
anything else. There is no default: the cursor that tracks what you've already seen
is keyed by it, so two agents sharing a name share a cursor and each miss half the
feedback.

If the space lives on a remote server, you also need `ANALOG_URL` and `ANALOG_TOKEN`.
The token identifies exactly one actor, and the server takes your name from it — if
`ANALOG_ACTOR` disagrees with the token, every write is refused rather than
silently reattributed. `analog whoami` tells you what the server thinks you are, and
is the first thing to run when something 401s or 403s.

## Every session, first

    analog feedback <slug>

Nothing printed means nothing changed. Otherwise:
- `motivation: editing` — an instruction. Do it.
- `motivation: assessing` — a verdict. Don't argue; adjust.
- `motivation: commenting` — context. Read it, no action required.
- `(stale)` — the card changed after the comment was written. It may already be
  fixed, or you may have rewritten around it. Check before you act, and never
  resolve it just because it's stale.
- deleted cards — the human rejected that idea. Don't re-add it.
- new links — the human sees a relationship you didn't. Consider why.
- `replies on resolve` — a comment you resolved came back with the human's answer.
  That answer is addressed to you. Act on it.

Call `analog resolve <id> --reply "..."` for each one you act on. Unresolved
annotations are how the human tracks what you've ignored.

## Posting work

    analog add <slug> --title "..." --kind md --file draft.md
    cat chart.svg | analog add <slug> --title "Revenue" --kind svg -
    analog add <slug> --title "Overview" --kind html --file overview.html --width 560 --height 420

- **One idea per card.** A wall of text in a single card cannot be annotated
  usefully, which defeats the point.
- Markdown cards render GFM and LaTeX (`$...$`, `$$...$$`). Use `--kind html`
  or `--kind svg` for anything visual — it renders, and the human can pin
  comments on specific regions of it.
- Before writing an `html` or `svg` card, read `STYLE.md` next to this file.
  It is the card flavour that reviews well — infographic, ELI5, comparison,
  diagram — and what the sandbox forces: paint your own colours, real text
  only, distinct regions a comment can land on. It also covers interactive
  feedback controls and the supported AG-UI sidecar pattern for HTML cards.
- A whole set of cards that teaches something — a topic the human asked to
  learn, or the work from a session they did not watch — has its own shape:
  survey what they already hold, post the route, then one card per idea. Read
  `EXPLAIN.md` next to this file before you start; a set that skips the survey
  is usually pitched at the wrong level.
- `analog link <slug> <from> <to> --label "..."` whenever cards relate.
  Always label. Unlabelled edges are noise.
- Set `--width` and `--height` when a card was designed for a particular viewport.
  Use `--x` and `--y` only when the spatial relationship is part of the artifact;
  otherwise let the server place it.
- Don't post status updates or narration. Analog is for artifacts under
  review, not a log.

## Revising a card

    analog update <slug> <card_id> --file fixed.svg
    analog update <slug> <card_id> --file fixed.svg --mode branch   # keep the old one
    analog update <slug> <card_id> --x 40 --y 80 --width 560 --height 420

`--mode branch` keeps the old card, marks it superseded and links it to the new one.
Use it when the human is comparing options or should see what you changed; the
default replaces in place.

## Don't

- Don't delete or edit cards the human created — annotate them instead.
- Don't resolve annotations you haven't acted on.
- Don't rearrange cards the human positioned. Position and size your own cards only
  when it makes the work clearer.

## Commands

    analog spaces                        # list spaces
    analog new <slug> --title "..."      # create one
    analog open <slug>                   # print the URL for the human
    analog cards <slug>                  # id, title, kind, created_by, rev
    analog comments <slug>               # open annotations
    analog rm <slug> <card_id>           # your own cards only
    analog export <slug> > out.canvas    # JSON Canvas; opens in Obsidian
    analog export <slug> --format html > out.html
    analog import <slug> < in.canvas     # merges, never deletes

`--json` works on every read command. Everything exits non-zero on failure, so check
the status rather than the output. Exit **3** specifically means an auth problem:
a missing, wrong, or revoked token — not something to retry.

## MCP

If you have MCP config instead of a shell, the same ten operations are tools:
`list_spaces`, `create_space`, `read_space`, `add_cards`, `update_card`,
`delete_card`, `link_cards`, `get_feedback`, `resolve_annotation`, and
`await_feedback` for when you want to block until the human responds. The rules above
are identical — `get_feedback` first, one idea per card, always label links.
