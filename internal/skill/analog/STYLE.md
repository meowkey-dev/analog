# Analog style: html and svg cards

The card flavour that reviews well: an infographic, an ELI5 explainer, a
comparison, a diagram. One idea, understood in thirty seconds, built so the
human can pin a comment on exactly the part they disagree with. Read this
before writing `--kind html` or `--kind svg`; SKILL.md covers the workflow.

## What a card is, and what that forces

- **A sandboxed iframe, scripts only, origin `null`.** Nothing from Analog
  reaches in: no theme, no stylesheet, no fonts, no token. Paint every colour
  and set every font yourself. Links cannot open (no popups), so cite in text.
- **320×200 to start, resized by the human, popped out to at most 1200 wide.**
  It scrolls; it never scales. Treat the frame like a mobile viewport: make the
  column fill every available pixel as the card is resized, then stop it at a
  readable `max-width`. Use `width: 100%` with `box-sizing: border-box` so the
  gutter is included rather than causing overflow. Do not fix the column to one
  card size or wait for a breakpoint to make it fluid.
- **A dark sheet on a dark desk.** Analog's chrome is near-black and its
  markdown cards default to Nord, so an `html` card paints the same Nord
  ground and the board reads as one surface. Declare `color-scheme: dark` and
  an explicit `body` background; the frame behind an unpainted document is
  white and glares. No `prefers-color-scheme` switching: a card must look the
  same to the human, in the export, and in your own check.
- **The column is centred.** A card is 320–600px wide but a pop-out is 1200,
  and a left-aligned column leaves two thirds of it empty. Cap the measure and
  centre it with `margin-inline: auto`, so the same document reads in both.
- **Comments land on regions, and Analog quotes the text under them.** Pins,
  shift-drag rectangles and in-card search all walk real DOM text. Text baked
  into a canvas or raster is invisible to all three. Build the card as a few
  distinct regions with space between them, so a rectangle selects one claim.
- **Export carries the document and nothing else.** No CDN scripts, no remote
  images, no library stylesheets. Inline SVG for pictures, data URIs for the
  rare small raster. System font stacks by default; a Google Fonts link is
  fine only with a real fallback stack, so the card holds when it never loads.

## Calibrate

These cards are explanatory, not promotional. No hero, no `100vh` opener, no
entrance animation, no numbered markers unless the content is a sequence. The
first frame shows the whole point at rest; a reviewer skimming twenty cards
gives each one a glance. Every figure is real, in its own units and scale; a
placeholder number invites a comment about the placeholder.

Pick the shape from the idea:

- **ELI5.** The claim in one plain sentence, then the picture, then why it is
  true in at most three beats, then the caveat. Name the analogy as an analogy.
  A term of art appears once, defined where it appears.
- **Infographic.** Lead with the figure that matters, large, with unit and
  scale. One scale per chart; every tick names a value the chart reaches. The
  emphasized mark takes the accent and everything else stays neutral. Close
  with a source line: the human is reviewing, and will ask where it came from.
- **Comparison.** Every option gets the same slots in the same order, so the
  eye can move across rows. Same edges, same baselines, same inner padding.
- **Diagram.** Prefer `--kind svg`. It is sanitized (no scripts, no external
  references), inlined on the dark card ground rather than on white, and the
  human can draw on it. Give it a `viewBox`, an explicit fill on every shape,
  `<text>` for every label, and leave room inside the viewBox for the
  outermost labels.

Split when parts could get different verdicts. Three linked cards with
labelled edges review better than one long page; one card with three regions
is right when the parts only make sense together.

## Tokens

The palette is Nord, the same values the web UI's markdown theme uses, so a
markdown card and an html card sit side by side without a seam:

| role | token | Nord |
| --- | --- | --- |
| ground | `--paper: #2e3440` | nord0 |
| raised block, code | `--panel: #3b4252` | nord1 |
| rule, bar track | `--rule: #434c5e` | nord2 |
| ink | `--ink: #d8dee9` | nord4 |
| muted, labels, source | `--muted: #a7adba` | between nord3 and nord4 |
| accent, one place | `--accent: #88c0d0` | nord8 |
| link | `--link: #81a1c1` | nord9 |
| warning, a cap or limit | `--warn: #ebcb8b` | nord13 |
| critical | `--danger: #bf616a` | nord11 |
| good | `--ok: #a3be8c` | nord14 |

Declare them as custom properties on `:root` and derive every colour from
them; never write a literal into a component. One accent, spent in one place.
The semantic colours are for meaning, not decoration, and do not count as the
accent. Skip the looks that read as generated: a gradient hero, emoji as
section markers, one radius on every block, three accent colours fighting.

Type: base 15–16px, line-height about 1.45, running text at most 65
characters, a scale of three sizes (label, body, display figure) and stay on
it. Display figures and any column of digits use `tabular-nums`. Uppercase
labels get a touch of letter-spacing; headings get `text-wrap: balance`.

Layout: `body` carries the 16–20px gutter, `box-sizing: border-box`, `width: 100%`,
a `max-width` around 62ch and `margin-inline: auto`. That makes the content fluid
below the cap and centred at the cap above it; the ground still fills the frame.
Sibling groups sit in flex or grid with `gap`, and nothing carries per-element
margins that collapse. Regions separate by space or a single rule, not by
card-on-card borders. Nothing is `position: fixed` or `sticky`; the frame scrolls
and a pinned bar eats a card-sized viewport. Only a wide table or code block may
exceed the width, each inside its own `overflow-x: auto`. No `height: 100vh`
anywhere.

## Skeleton

```html
<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Why the cache misses</title>
<style>
  :root {
    color-scheme: dark;
    --paper: #2e3440; --panel: #3b4252; --rule: #434c5e;
    --ink: #d8dee9; --muted: #a7adba; --accent: #88c0d0; --warn: #ebcb8b;
    font: 15px/1.45 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
  }
  html { background: var(--paper); }
  body { box-sizing: border-box; width: 100%; margin: 0 auto; padding: 18px 20px;
         max-width: 62ch; color: var(--ink);
         display: flex; flex-direction: column; gap: 18px; }
  h1 { font-size: 20px; line-height: 1.2; margin: 0; text-wrap: balance; }
  .label { font-size: 11px; letter-spacing: .06em; text-transform: uppercase; color: var(--muted); }
  .figure { font-size: 40px; font-weight: 650; font-variant-numeric: tabular-nums; color: var(--accent); }
  section { display: flex; flex-direction: column; gap: 6px; }
  .source { font-size: 12px; color: var(--muted); border-top: 1px solid var(--rule); padding-top: 8px; }
  @media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
</style>
<h1>Every fourth lookup goes to disk</h1>
<section>
  <span class="label">Miss rate, last 24 h</span>
  <span class="figure">26 %</span>
</section>
<section>
  <p>The cache holds 8 GB; the working set is 11 GB. Whatever does not fit
     is evicted and read again next time.</p>
</section>
<p class="source">Source: Prometheus <code>cache_miss_ratio</code>, 2026-09-13.</p>
```

Everything is real text, the regions are addressable, the figure carries its
unit, and the source is on the card. Size it when you post it: MCP `add_cards`
takes `width` and `height`, and the CLI takes `--width` and `--height`, so send
the dimensions the card was designed for. A 480×360 ELI5 or a 560×420
infographic lands readable; the human resizes from there.

## Before you post

- Opens at rest with the whole point visible, no scroll needed for the claim.
- Every colour comes from the Nord tokens; `html` paints the ground.
- The column fills a narrow frame, then stays capped and centred as it widens.
- All text is DOM text or SVG `<text>`. Nothing readable lives in a canvas.
- Every number has a unit, and the source is on the card.
- Regions have space between them; a rectangle can select one claim.
- No CDN, no remote image, no `fixed`, no `100vh`, no link that needs a click.
- `svg` cards: `viewBox`, explicit fills, no scripts.
