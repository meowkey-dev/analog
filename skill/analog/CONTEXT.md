# Analog working context: the space is the memory

The shape for a space that holds the working context of a piece of work, so the
work survives whatever interrupts it. When the human says to use the space as
the working context, the space — not your memory, not a handoff document — is
what the next reader continues from. That reader may be you after your context
was cut, another agent joining, or you tomorrow. SKILL.md covers the workflow;
this file covers which cards hold the context, what goes on them, and when you
write them.

The test for every card set: **an agent with only this space and the repo can
take the next step correctly.** Nothing else is assumed to survive — not your
conversation, not a summary of it, not a file outside the repo.

## What goes on the cards

Two questions for every candidate:

|                        | **matters later**          | **won't matter later** |
| ---------------------- | -------------------------- | ---------------------- |
| **rebuildable**        | leave it; point to it      | leave it               |
| **not rebuildable**    | **write it down**          | let it go              |

Rebuildable means a reader can get it back from the repo or the server: code,
diffs, git history, test output, PR state, what is already in the repo's own
docs. Point at it — a path, a commit, a command — and never copy it; a copy
goes stale against the source and costs the reader tokens twice.

What is not rebuildable is exactly what a conversation holds and a repo does
not, and it is the first thing lost when the conversation goes:

- the goal in the human's own words, and what counts as done;
- instructions and preferences the human gave in chat;
- decisions, why they were made, and what was rejected and why;
- words and framings coined along the way, which the cards and the human keep
  using;
- hypotheses in flight and directions not chosen yet;
- dead ends and facts that were expensive to learn;
- what the uncommitted changes in the tree are for.

Over-preserving is a failure too. A context nobody can read in two minutes is
skimmed, and a skimmed context is a wrong one. When something stops mattering,
take it off the card.

## The cards

Five `md` cards, one of each, every title starting with **`⌂`**. The prefix is
the marker: it is how a reader finds the context among the review cards in a
shared space, and it means the same thing in a space made just for the
context. Use it for nothing else, and never have two cards with the same `⌂`
title — a reader looks them up by title.

The whole set stays readable in a couple of minutes: about 1,500 words across
all five, the Brief and State well under a third each.

### `⌂ Brief` — read first, changes rarely

```md
**Goal:** one or two sentences, in the human's words.
**Done when:** something a reader can check.
**Constraints / non-goals:** what must not change, what is out of scope.
**Vocabulary:** term — meaning. Only terms coined here or used unusually.
**Working with <human>:** how they want to work, as they said or showed it.
**Where the work lives:** repo, branch, PR or issue, the commands that matter.
**Cards:** ⌂ State c_…, ⌂ Decisions c_…, ⌂ Open threads c_…, ⌂ Findings c_…
```

### `⌂ Decisions` — one ledger, a line per decision

```md
1. <what was decided> — because <why>. Rejected: <alternative> (<why not>).
2. ~~<a reversed decision>~~ — reversed by 5.
```

A reversed decision keeps its line and points at what replaced it: why the
first answer was wrong is part of the context. The ledger settles questions —
a reader does not reopen a line without the human.

### `⌂ State` — where the work is, changes constantly

```md
**Resume here:** the single next action, specific enough to start on.
**In progress:** what, and how far.
**Next:** in order.
**Blocked:** on what or whom.
**Done:** only what a reader would otherwise redo; point at the commits.
**Uncommitted:** what each dirty file is — intended, experimental, half-done.
```

### `⌂ Open threads` — unfinished thinking

```md
**Questions for <human>:** numbered, so an annotation can answer one.
**Hypotheses:** what we think, and what would show it is wrong.
**Undecided:** directions still open, with what would decide them.
```

When a thread closes, remove it; its outcome goes on Decisions or Findings.
This is where the human's answers land, so it changes less than State and their
annotations stay current longer.

### `⌂ Findings` — what was expensive to learn

```md
- Tried <X> → fails because <Y>. Evidence: <path, command or commit>.
- <fact> (durable)
```

Mark a finding `(durable)` when it holds beyond this piece of work. It is a
hint for whoever keeps the repo's own docs; the space does not move it there.

## Start

Triggered by "use the analog space as the working context", or the same in
other words.

1. Find the slug: the one the human named, the project's AGENTS.md, or ask. A
   context can share a space with review cards or have one of its own; go with
   the human's choice, and `analog new` if the space does not exist.
2. `analog cards <slug>`. If a `⌂ Brief` is already there, this is a
   **pick up**, not a start — never build a second set.
3. Write all five cards from the conversation so far. A card with nothing on it
   yet says so in one line; the set stays five so the map never changes.
4. Put the other four ids on the Brief's **Cards** line, and link the Brief to
   each one — labelled `state`, `decisions`, `open threads`, `findings`.
5. `analog open <slug>` and ask the human to read the Brief. A wrong goal costs
   one annotation now and the whole piece of work later.

## Keep it current

Write at the moment it happens, not at the end. The context can be cut at any
point, without warning; whatever was only in the conversation is gone.

| when | card |
| --- | --- |
| The human gives an instruction or a preference in chat | Brief or Decisions, now — chat is the first thing lost |
| A decision is made | Decisions |
| A step starts or finishes | State |
| Something fails in a way worth knowing | Findings |
| A question for the human comes up | Open threads |
| A thread closes | remove it from Open threads; the outcome goes on Decisions |
| Before anything long-running or hard to undo | State, with an exact **Resume here** |

Every write replaces the card in place, against the rev you read:

    analog cards <slug>                                        # ids and revs
    analog update <slug> <card_id> --file state.md --if-match <rev>

A `409` means someone else — the human, or another agent — wrote the card
since you read it. Read it again, merge, and retry; never overwrite it. Never
`--mode branch` a `⌂` card: a branch leaves two cards with the same title.

These five cards are the one exception to SKILL.md's rule that Analog is not a
log. They are the current truth, replaced in place and bounded in size. The
exception does not stretch to a sixth card: no session log, no progress
narration.

**Annotations on these cards are about the context itself.** An `editing`
annotation on the Brief changes the goal; an answer on Open threads settles a
question. Replacing a card marks its open annotations stale, so run `analog
feedback` first and act on and resolve what is there before you rewrite the
card. If the human edits a `⌂` card directly, their words stand; write around
them.

## Checkpoint

Triggered by "checkpoint the space", "sync the space" or the same in other
words — the human may ask before a break, before handing the work on, or
before anything else that interrupts it. Why does not change what you do.

1. Go back over the conversation since the last checkpoint and ask the two
   questions of everything in it. Small writes drift; this is where they are
   reconciled.
2. Update every `⌂` card that is behind. Prune what stopped mattering.
3. Make **Resume here** exact: a reader starts on it without asking anything.
4. Tell the human in one line which cards changed.

## Pick up

Triggered by "pick up from the space", "continue from <slug>", or finding
yourself pointed at a space with a `⌂ Brief` you do not remember writing.

1. `analog feedback <slug>`, handled as in SKILL.md. What the human said since
   the cards were last written outranks the cards.
2. Read the context: `analog cards <slug> --json` carries each card's text
   (`read_space` over MCP). Brief first, then State, Decisions, Open threads,
   Findings.
3. Check it against live state — `git status`, `git log`, the branch, the PR.
   Trust in this order: live state, then the cards, then anything you remember
   or were given as a summary. Where the cards are behind, fix the cards first.
4. Fold in anything newer. If you hold notes about this work the cards do not
   have — a summary of an earlier conversation, a handoff someone left, what the
   human just told you — put what is missing on the cards now. The space stays
   the one source.
5. If something you needed was not there, add it where it belonged. The next
   reader needs it too.
6. Start from **Resume here**. Do not reopen a settled line on Decisions
   without the human.

## Don't

- Copy what the repo already holds: diffs, file contents, test output, logs.
- Narrate. The context is five cards; there is no sixth.
- Branch a `⌂` card, duplicate a `⌂` title, or use `⌂` for anything else.
- Overwrite a card after a `409` without reading what changed.
- Rewrite a card over open annotations you have not acted on.
- Let **Resume here** describe the step before last.

## Before you stop

- Could an agent with only this space and the repo take the next step
  correctly? If not, the missing piece belongs on a card.
- Every instruction the human gave in chat is on the Brief or Decisions.
- **Resume here** names the actual next action; **Uncommitted** matches
  `git status`.
- Open threads holds only what is still open.
- Nothing on any card could be fetched from the repo instead.
