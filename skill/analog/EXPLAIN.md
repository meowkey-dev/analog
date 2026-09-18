# Analog explainers: ask first, then map, then teach

The shape for a set of cards that brings someone up to speed — on a subject
they asked to learn, or on work you did across a session they did not watch.
SKILL.md covers the workflow and STYLE.md covers how a card looks; this file
covers what goes on it and in what order.

Two subjects, one spine:

- **A topic.** The human wants to understand something outside the repo. Your
  material comes from research.
- **Your own work.** A long session ended and the human has to judge, use or
  hand off what you built. Your material is the diff, the commits and the
  decisions you made on the way.

Both run the same four steps: **probe → route → explainers → recap**. What
differs is where the material comes from and what the human does with it at the
end. The topic ends in understanding; the work ends in a verdict.

## The rules

1. **Skeleton before flesh.** Every subject has two to four *meta-concepts*:
   the ideas everything else leans on, which appear again and again and which
   nothing else makes sense without. Scarcity and trade-off in economics; the
   write transaction in this repo. Find them before you explain anything. A
   learner who missed one does not learn slower, they learn wrong and have to
   go back.
2. **Calibrate before you explain.** You do not know what the human already
   holds, and guessing costs a whole set of cards. Ask, in one card, cheap to
   answer. This is the step that is usually skipped and the one that decides
   whether the rest lands.
3. **A ladder, not a pile.** Order by dependency: nothing appears before the
   thing it rests on. Three rungs — how it works, what it is for, where it
   breaks down — and a card that ties each rung together before you climb.
4. **Every analogy declares where it breaks.** An analogy that stands alone
   hardens into a wrong model that is harder to remove than ignorance. Name it
   as an analogy, then name the one or two places it stops being true.
5. **They explain it back, or it did not land.** The target is the human
   saying it in their own words, not you having said it well.

## 1. Probe

One card. At most five questions. Answerable in two minutes.

Ask about the meta-concepts, not the subject's trivia — you are finding the
floor to build on, not testing anyone. For each one, a plain-language gloss and
a three-way answer: new to me / heard the word / I use this. A word is an
answer; an essay is a chore and you will not get one.

Ask for the target too, in the human's own words: the thing they want to be
able to do, decide or explain when the cards are done. "Read a flame graph and
find the hot path" pulls the right cards after it. "Learn about performance"
pulls everything and finishes nothing.

- **Say what each answer changes.** "If you already use X, I skip two cards
  and go straight to Y." Then answering is obviously worth the two minutes.
- **`md` is enough**, and it is the fastest thing to post. Use `html` when you
  want the human to pin a comment on one concept rather than on the list — then
  give each concept its own region, per STYLE.md.
- **Never grade.** No score, no "level", no right answers. A card that reads
  as an exam gets closed, not answered.
- **Then wait.** `await_feedback` blocks until they respond; `analog feedback`
  polls. Do not start explaining while the probe is unanswered.
- **Do not block forever.** If the human says "just explain it", or does not
  answer, pick a floor, write the assumption on the route card, and go. A
  stated wrong guess gets corrected in one annotation; an unstated one wastes
  ten cards.

Read the answers as a floor, not a ceiling. "Heard the word" means the term is
familiar and the mechanism is not — that concept still needs its card, and the
card can start from the word.

## 2. Route

Post the route card before the explainers. A wrong route costs one card of
annotation; ten wrong explainers cost a rewrite. It carries:

- The target, in one sentence, from the probe.
- The meta-concepts, each with a one-line plain gloss.
- The floor you are building on, and whether the human told you or you assumed
  it. An assumption in writing is an invitation to correct it.
- The rungs, and what each one is going to explain.
- Where the material comes from: sources for a topic, paths and commits for
  your own work.

**The links are the map.** Do not list a table of contents the graph already
holds. Post each explainer as it lands and `analog link` it from the route
card, labelled with the dependency it carries — `needs`, `then`, `contrasts
with`, `decided in`. The human reads the route on the board, and an unlabelled
edge is noise.

Budget six to twelve explainers. Fewer is a briefing, which is fine — say so
instead of padding. More than twelve and nobody finishes; split the subject and
propose the second half separately.

## 3. Explainers

One idea per card, one card per idea, in ladder order. Six beats:

| beat | what it is |
| --- | --- |
| claim | The title is the finding, not the topic. "Every fourth lookup goes to disk", not "Cache behaviour". |
| anchor | The thing they already hold — from the probe, or the card before. Name it explicitly; do not hope they connect it. |
| picture | The mechanism, in the figure. If the prose describes the mechanism the figure is decoration. |
| analogy | One, named as an analogy, in one sentence, reaching for something in the body or the day. |
| boundary | Where that analogy stops being true. One or two points, concrete. |
| foot | The one line worth keeping, plus what to explain back. |

Close each rung with a card that connects its pieces — usually an `svg`
diagram — unless the labelled edges already say it plainly.

A term of art appears once, defined where it appears. Every figure is real and
carries its unit and its source; a placeholder invites a comment about the
placeholder. STYLE.md has the rest.

Put a quiet orientation line at the top of each card: which rung, and which
card of how many. The board shows the graph, not the reading order, and a
learner who cannot tell how much is left rations attention badly.

### Picture first, few words

The discipline that makes these cards work is **a big picture and little
text**. Budget about 150 words of prose per card, labels inside the figure
excluded. The figure carries the mechanism and the prose says what it means;
when the prose explains the mechanism too, the figure has become decoration and
the card is now an essay nobody reads at a glance.

If an idea will not fit in 150 words, it is two ideas. Split it and link them.

### Choosing the shape

STYLE.md has four card shapes and says how each looks. Pick one per card — the
beats above are the content, the shape is the arrangement:

- **ELI5** is the default for a concept card, and it is what the six beats
  above already describe: the claim in plain words, the picture, why it is
  true in a few beats, then the caveat.
- **Infographic** whenever a real number carries the point — a rate, a size, a
  duration, a share, a before and after. Prefer it over a paragraph containing
  the same number: a figure at display size with its unit and scale is the one
  thing a reader keeps, and it is far easier to disagree with precisely, which
  is what the annotation is for. If the card has a number that matters, that
  number leads.
- **Comparison** when the human's real question is "which", or when two things
  are confusable and the confusion is the obstacle. Same slots, same order.
- **Diagram** (`svg`) for anything with parts and arrows — flow, state,
  structure, dependency. It is sanitized, it renders on the card ground, and
  the human can draw on it.

A concept card with no number and no parts is still an ELI5 with a picture,
never a wall of prose. "There is nothing to draw" almost always means the
mechanism is not yet understood well enough to explain.

## 4. Recap

The last card is where the human does the work:

- **The whole route**, as one `svg`: the rungs, the concepts, the dependencies.
- **Three to five explain-back prompts.** "Without looking, tell me why X
  happens." These are the actual assessment.
- **The stall points.** The three places you expect them to dry up, each with
  the hint that unsticks it. Naming them is most of the value — it tells the
  human that getting stuck there is the subject being hard, not them.
- **The analogies**, in a table with their boundaries, and two blank rows for
  their own. Their analogy is better than yours, because it is theirs.
- **Three to five places to go deeper**, chosen, not dumped.

Then read `analog feedback` and act on it. An `assessing` annotation on an
explainer means that card did not land: rewrite it, do not defend it. Use
`analog update --mode branch` when the human should see both versions, the
default replace when the old one was simply wrong. A concept the probe said
they use, explained anyway, is the mistake to avoid next time.

## When the subject is your own work

Same spine, different material and a different ending.

- **The target is a decision, not knowledge.** Ask in the probe what they need
  to do with this: review it, run it, extend it, hand it to someone else. A
  review needs your uncertainties; a handoff needs the vocabulary and the
  invariants. Build for the one they name.
- **The meta-concepts are what they must hold to judge the work** — the
  vocabulary the change is written in, the invariants you had to respect, the
  shape of what moved. Not a summary of the code.
- **The shape of the change is an infographic.** What moved and how much, in
  real units — files touched, the benchmark before and after, tests added,
  what the numbers were when you started. A reviewer orients on that in five
  seconds and cannot orient on a paragraph saying "substantial refactoring".
  Every number comes from a command you actually ran, not an estimate.
- **Cite the work.** `internal/store/cards.go:210`, a commit subject, a test
  name. Cards cannot open links, so the path is the citation; a reviewer who
  wants the code will go there.
- **One card per decision a reasonable person could have made differently**:
  what you chose, what you rejected, why. These are the cards that earn
  annotations, and an `assessing` annotation on one is the human overruling
  you. That is the point of posting it.
- **Name what you are unsure about and what you did not do.** A recap that
  reports only successes wastes the review it asked for.
- **Do not narrate chronology.** Analog is not a log. Order by what the human
  needs to understand first, which is almost never the order you did it in —
  the dead ends you backed out of are one card at most, and usually zero.
- The recap card ends in the verdict you need: what to approve, what to
  redirect, what is still open.

## Don't

- Explain before the probe comes back, when the human is there to answer it.
- Write a probe that grades, or one that takes ten minutes.
- Put a whole rung on one card because it is all connected. Link it instead.
- Write a card that would read the same with the figure deleted.
- Give an analogy no boundary.
- Invent a source, a figure or a citation. A real gap is a card that says so.
- Re-explain a meta-concept the human said they use.
- Leave annotations on your explainers unresolved. They are the measurement.

## Before you post the set

- The probe went first, and the route card states the floor it assumes.
- Two to four meta-concepts, named, glossed in plain words, each with a card.
- Nothing depends on a card that comes after it.
- Every card's title is a claim; every analogy has a boundary.
- Every card is a picture and about 150 words, not prose with a picture under
  it, and each one names its rung and its place in the set.
- Every card that turns on a number leads with that number, in its own unit.
- Every explainer is linked from the route with a labelled edge.
- Six to twelve explainers, one idea each.
- The recap asks for an explain-back, names the stall points, and — for your
  own work — asks for the decision the human actually has to make.
