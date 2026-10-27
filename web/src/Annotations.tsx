import { useEffect, useRef, useState } from "react";
import type { Annotation, Motivation, Node, Selector } from "./api";
import { isPending } from "./optimistic";

/**
 * The annotation layer (SPEC §5).
 *
 * The overlay lives in the parent document, *above* the card — including above an
 * html card's iframe. Because the iframe is sandboxed without allow-same-origin,
 * agent HTML can neither read the annotations nor forge one. The overlay only
 * accepts pointer events while annotate mode is on, so cards stay interactive
 * the rest of the time — and text stays selectable, which is how most comments
 * start (#125): selecting text offers a Comment button, with no mode to enter.
 * A selection becomes an ordinary rect selector plus its quote, so the wire
 * shape is unchanged; points and regions stay in comment mode, for images and
 * charts where there is no text to select.
 *
 * Selectors are fractions of the card's *content*, not the visible box (issue #23),
 * so a pin stays on the words it was placed on when the body scrolls. The overlay
 * tracks the content's extent and scroll offset — measured directly for kinds the
 * parent renders, and postMessaged out of the sandboxed iframe by a script the
 * renderer injects (see Card.tsx).
 *
 * Selectors never reach agents in usable form — fractions need a renderer to mean
 * anything. So the overlay also resolves the text under a dropped pin into a quote
 * that is folded into the comment body: the browser is the one component that
 * knows what a pin points at, and the body is the one channel agents already read.
 */

/** Layout of a card's scrollable content, as far as the pin math is concerned. */
export interface ScrollMetrics {
  cw: number; ch: number; // content extent, px
  vw: number; vh: number; // visible extent (the overlay box), px
  sx: number; sy: number; // scroll offset, px
}

export interface DraftAnnotation {
  cardId: string;
  selector: Selector;
  /** Text under the pin at drop time, quoted into the comment body on submit. */
  quote?: string;
  /** What the composer starts with: the words of a save that failed (#127). */
  body?: string;
  /** Where the gesture ended, client px: the composer opens beside it. */
  at?: { x: number; y: number };
  /** Made from a text selection rather than a pin or a dragged region. */
  fromSelection?: boolean;
}

/** A text selection on one card, waiting for its Comment button to be pressed. */
interface PendingSelection {
  selector: Exclude<Selector, null | { type: "point" }>;
  quote: string;
}

/**
 * One selection at a time across every card. A parent-rendered card loses its
 * selection when another is made, but each html card is its own document and
 * keeps one: the overlay that takes the selection announces it so the others
 * drop theirs.
 */
const SELECTION_OWNER = "analog-selection-owner";

interface OverlayProps {
  node: Node;
  /** The scrollable body element (the iframe itself for html cards). */
  bodyRef: React.RefObject<HTMLElement | null>;
  annotations: Annotation[];
  active: boolean;
  draft: DraftAnnotation | null;
  selectedId: string | null;
  onSelect: (id: string) => void;
  onDraft: (draft: DraftAnnotation) => void;
}

const clamp01 = (v: number) => Math.min(1, Math.max(0, v));

/** A viewport fraction becomes a content fraction by adding the scroll offset. */
function toContent(frac: { x: number; y: number }, m: ScrollMetrics | null): { x: number; y: number } {
  const vw = m?.vw || 1, vh = m?.vh || 1;
  const cw = m?.cw || vw, ch = m?.ch || vh;
  return {
    x: clamp01((frac.x * vw + (m?.sx ?? 0)) / cw),
    y: clamp01((frac.y * vh + (m?.sy ?? 0)) / ch),
  };
}

/**
 * A selection's bounding box, as fractions of the visible box, becomes a rect
 * selector over the content — the same shape a shift-drag region produces.
 */
export function selectionSelector(
  view: { x: number; y: number; w: number; h: number },
  m: ScrollMetrics | null,
): PendingSelection["selector"] {
  const a = toContent({ x: view.x, y: view.y }, m);
  const b = toContent({ x: view.x + view.w, y: view.y + view.h }, m);
  return { type: "rect", x: a.x, y: a.y, w: b.x - a.x, h: b.y - a.y };
}

/**
 * Where the Comment button sits: just under the selection's end, kept inside
 * the visible box so a selection that runs off the bottom still offers it.
 */
export function selectionButtonStyle(
  s: { x: number; y: number; w: number; h: number },
  m: ScrollMetrics | null,
): React.CSSProperties {
  if (!m) return { left: `${(s.x + s.w) * 100}%`, top: `${(s.y + s.h) * 100}%` };
  const left = (s.x + s.w) * m.cw - m.sx;
  const top = (s.y + s.h) * m.ch - m.sy + 4;
  return {
    left: Math.min(Math.max(left, 72), m.vw - 4),
    top: Math.min(Math.max(top, 4), m.vh - 30),
  };
}

const reducedMotion = () =>
  typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

/**
 * Map a content-relative selector to inline position styles. Point selectors
 * deliberately leave their dimensions unset so `.pin.point` can keep its
 * circular CSS size; region selectors provide both dimensions here.
 */
export function pinStyle(
  s: { x: number; y: number; w?: number; h?: number },
  m: ScrollMetrics | null,
): React.CSSProperties {
  const style: React.CSSProperties = m
    ? { left: s.x * m.cw - m.sx, top: s.y * m.ch - m.sy }
    : { left: `${s.x * 100}%`, top: `${s.y * 100}%` };
  if (s.w !== undefined) style.width = m ? s.w * m.cw : `${s.w * 100}%`;
  if (s.h !== undefined) style.height = m ? s.h * m.ch : `${s.h * 100}%`;
  return style;
}

const QUOTE_MAX = 240;

/** Whitespace-collapse, trim and cap a captured quote. */
function cleanQuote(raw: string): string {
  const text = raw.replace(/\s+/g, " ").trim();
  return text.length > QUOTE_MAX ? text.slice(0, QUOTE_MAX) + "…" : text;
}

/**
 * The text of every text node whose rendered lines intersect `target` (client
 * coordinates). Hit-testing the caret would only see the overlay itself — it
 * covers the card while annotate mode is on — so intersect with line boxes instead.
 */
function quoteUnderRect(root: HTMLElement | null, target: DOMRect): string {
  if (!root) return "";
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  const parts: string[] = [];
  let range: Range | null = null;
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    if (!n.nodeValue?.trim()) continue;
    range ??= document.createRange();
    range.selectNodeContents(n);
    for (const r of range.getClientRects()) {
      if (r.left <= target.right && r.right >= target.left &&
          r.top <= target.bottom && r.bottom >= target.top) {
        parts.push(n.nodeValue);
        break;
      }
    }
  }
  return cleanQuote(parts.join(" "));
}

/** Rendered inside each card. Pins are fractions of the content, so they survive resize. */
export function AnnotationOverlay(props: OverlayProps) {
  const { node, annotations, active, draft, selectedId, bodyRef } = props;
  const [rect, setRect] = useState<{ x: number; y: number; w: number; h: number } | null>(null);
  const [metrics, setMetrics] = useState<ScrollMetrics | null>(null);
  const [pending, setPending] = useState<PendingSelection | null>(null);
  const layerRef = useRef<HTMLDivElement>(null);
  // Handlers fire between renders; the mirrors are what they read.
  const metricsRef = useRef<ScrollMetrics | null>(null);
  const draftRef = useRef<DraftAnnotation | null>(draft);
  draftRef.current = draft;

  const kind = node.type === "file" ? "file" : (node.sp_kind ?? "plain");
  // Text kinds read a region as a passage, so it draws as a highlight; on an
  // image or a drawing it stays an outlined box.
  const highlights = kind === "md" || kind === "plain" || kind === "html";

  // Holding a selection claims it; any other card's is dropped.
  const offer = (next: PendingSelection | null) => {
    setPending(next);
    if (next) window.dispatchEvent(new CustomEvent(SELECTION_OWNER, { detail: node.id }));
  };

  useEffect(() => {
    const onOwner = (event: Event) => {
      if ((event as CustomEvent<string>).detail === node.id) return;
      setPending(null);
      const el = bodyRef.current;
      if (el instanceof HTMLIFrameElement) {
        el.contentWindow?.postMessage({ type: "analog-selection-clear" }, "*");
      }
    };
    window.addEventListener(SELECTION_OWNER, onOwner);
    return () => window.removeEventListener(SELECTION_OWNER, onOwner);
  }, [bodyRef, node.id]);

  // Parent-rendered text is selected in this document. The button is offered
  // when the gesture ends, not on every selectionchange, so it does not chase
  // the pointer mid-drag; only a gesture that began in this body counts, so
  // the in-card search, which selects its matches, never offers one.
  // The body is looked up per event: it remounts when the card toggles to its
  // diff and back, and a listener bound to the old element would go deaf.
  useEffect(() => {
    if (kind === "file") return;
    const body = () => {
      const el = bodyRef.current;
      return el && !(el instanceof HTMLIFrameElement) ? el : null;
    };
    let armed = false;
    const arm = (event: PointerEvent) => {
      armed = Boolean(body()?.contains(event.target as globalThis.Node));
    };
    const settle = () => {
      if (!armed) return;
      armed = false;
      // After the click that ends a selection has been applied to it.
      setTimeout(() => {
        const el = body();
        const layer = layerRef.current;
        const sel = window.getSelection();
        if (!el || !layer || !sel || sel.isCollapsed || !sel.rangeCount) return offer(null);
        const range = sel.getRangeAt(0);
        const quote = cleanQuote(sel.toString());
        if (!quote || !el.contains(range.commonAncestorContainer)) return offer(null);
        const box = layer.getBoundingClientRect();
        const r = range.getBoundingClientRect();
        if (!box.width || !box.height) return offer(null);
        offer({
          quote,
          selector: selectionSelector({
            x: (r.left - box.left) / box.width, y: (r.top - box.top) / box.height,
            w: r.width / box.width, h: r.height / box.height,
          }, metricsRef.current),
        });
      }, 0);
    };
    const onChange = () => {
      const el = body();
      if (!el) return; // an html card's selection is its frame's to report
      const sel = window.getSelection();
      if (!sel || sel.isCollapsed || !sel.rangeCount ||
          !el.contains(sel.getRangeAt(0).commonAncestorContainer)) setPending(null);
    };
    document.addEventListener("pointerdown", arm, true);
    document.addEventListener("pointerup", settle);
    document.addEventListener("selectionchange", onChange);
    return () => {
      document.removeEventListener("pointerdown", arm, true);
      document.removeEventListener("pointerup", settle);
      document.removeEventListener("selectionchange", onChange);
    };
  }, [bodyRef, kind, node.id]);

  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    if (el instanceof HTMLIFrameElement) {
      // The sandbox blocks reading the iframe document, so the injected reporter
      // measures it from inside and posts here. event.source — not origin, which
      // is opaque — proves the message came from this frame. A forged value can
      // only displace pins or alter a quote the human still reviews before
      // submitting; it cannot create or read an annotation.
      // Read at message time: an updated card swaps in a new frame (html-card.tsx).
      const onMessage = (event: MessageEvent) => {
        const frame = bodyRef.current;
        if (!(frame instanceof HTMLIFrameElement) || event.source !== frame.contentWindow) return;
        const d = event.data as Record<string, unknown> | null;
        if (!d) return;
        if (d.type === "analog-scroll" && typeof d.cw === "number") {
          const next: ScrollMetrics = {
            cw: d.cw as number, ch: d.ch as number,
            vw: d.vw as number, vh: d.vh as number,
            sx: (d.sx as number) || 0, sy: (d.sy as number) || 0,
          };
          metricsRef.current = next;
          setMetrics(next);
          return;
        }
        // An html card's selection, reported from inside its sandbox in the
        // frame's own px; the frame's visible extent turns them into fractions.
        // A forged one can only offer a button the human then has to press.
        if (d.type === "analog-selection") {
          const m = metricsRef.current;
          const text = typeof d.text === "string" ? cleanQuote(d.text) : "";
          if (!text || !m?.vw || !m.vh) {
            setPending(null);
            return;
          }
          offer({
            quote: text,
            selector: selectionSelector({
              x: Number(d.vx) / m.vw, y: Number(d.vy) / m.vh,
              w: Number(d.vw) / m.vw, h: Number(d.vh) / m.vh,
            }, m),
          });
          return;
        }
        if (d.type === "analog-quote" && typeof d.text === "string" && d.text) {
          const cur = draftRef.current;
          if (cur && cur.cardId === node.id && !cur.quote) {
            props.onDraft({ ...cur, quote: d.text });
          }
        }
      };
      window.addEventListener("message", onMessage);
      return () => window.removeEventListener("message", onMessage);
    }
    const read = () => {
      const next: ScrollMetrics = {
        cw: el.scrollWidth, ch: el.scrollHeight,
        vw: el.clientWidth, vh: el.clientHeight,
        sx: el.scrollLeft, sy: el.scrollTop,
      };
      metricsRef.current = next;
      setMetrics(next);
    };
    read();
    el.addEventListener("scroll", read, { passive: true });
    const ro = new ResizeObserver(read);
    ro.observe(el);
    return () => {
      el.removeEventListener("scroll", read);
      ro.disconnect();
    };
  }, [bodyRef, node.id, kind]);

  // Choosing a comment elsewhere (the list, a thread) brings its pin into view
  // inside the card, not just the card onto the screen.
  useEffect(() => {
    const target = annotations.find((a) => a.id === selectedId)?.selector;
    const el = bodyRef.current;
    const m = metricsRef.current;
    if (!target || !el || !m || m.ch <= m.vh) return;
    const y = target.y * m.ch;
    const bottom = target.type === "rect" ? (target.y + target.h) * m.ch : y;
    if (y >= m.sy && bottom <= m.sy + m.vh) return;
    const top = Math.max(0, y - m.vh / 3);
    const behavior: ScrollBehavior = reducedMotion() ? "auto" : "smooth";
    if (el instanceof HTMLIFrameElement) {
      el.contentWindow?.postMessage(
        { type: "analog-scroll-to", sx: m.sx, sy: top, smooth: behavior === "smooth" }, "*");
    } else if (typeof el.scrollTo === "function") {
      el.scrollTo({ top, behavior });
    }
    // Only a change of selection scrolls; a scroll must not re-trigger it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedId]);

  const takeSelection = (event: React.MouseEvent) => {
    if (!pending) return;
    offer(null);
    props.onDraft({
      cardId: node.id,
      selector: pending.selector,
      quote: pending.quote,
      at: { x: event.clientX, y: event.clientY },
      fromSelection: true,
    });
    const el = bodyRef.current;
    if (el instanceof HTMLIFrameElement) {
      el.contentWindow?.postMessage({ type: "analog-selection-clear" }, "*");
    } else {
      window.getSelection()?.removeAllRanges();
    }
  };

  const fracIn = (box: DOMRect, clientX: number, clientY: number) => ({
    x: Math.min(1, Math.max(0, (clientX - box.left) / box.width)),
    y: Math.min(1, Math.max(0, (clientY - box.top) / box.height)),
  });

  // The quote for a drop, in client coordinates. Parent-rendered kinds are read
  // synchronously; html content can only be read from inside its sandbox, so the
  // request goes in and the reply updates the draft (see the message listener).
  const attachQuote = (next: DraftAnnotation, target: DOMRect) => {
    const el = bodyRef.current;
    if (el && !(el instanceof HTMLIFrameElement)) {
      const quote = quoteUnderRect(el, target);
      if (quote) return { ...next, quote };
    } else if (el) {
      const box = el.getBoundingClientRect();
      el.contentWindow?.postMessage({
        type: "analog-quote",
        vx: target.left - box.left, vy: target.top - box.top,
        vw: target.width, vh: target.height,
      }, "*");
    }
    return next;
  };

  const onPointerDown = (event: React.PointerEvent) => {
    if (!active) return;
    event.stopPropagation();
    event.preventDefault();
    const box = event.currentTarget.getBoundingClientRect();
    const startFrac = fracIn(box, event.clientX, event.clientY);
    const start = toContent(startFrac, metricsRef.current);
    const target = event.currentTarget as HTMLElement;
    target.setPointerCapture(event.pointerId);

    // Shift-drag draws a rect; a plain click drops a point.
    if (!event.shiftKey) {
      const selector: Selector = { type: "point", ...start };
      props.onDraft(attachQuote({ cardId: node.id, selector, at: { x: event.clientX, y: event.clientY } },
        new DOMRect(event.clientX - 1, event.clientY - 1, 2, 2)));
      return;
    }
    // The live rect stays in viewport fractions; only the final selector converts.
    setRect({ ...startFrac, w: 0, h: 0 });

    const move = (moveEvent: PointerEvent) => {
      const f = fracIn(target.getBoundingClientRect(), moveEvent.clientX, moveEvent.clientY);
      setRect({
        x: Math.min(startFrac.x, f.x), y: Math.min(startFrac.y, f.y),
        w: Math.abs(f.x - startFrac.x), h: Math.abs(f.y - startFrac.y),
      });
    };
    const up = (upEvent: PointerEvent) => {
      target.releasePointerCapture(event.pointerId);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      const upBox = target.getBoundingClientRect();
      const endFrac = fracIn(upBox, upEvent.clientX, upEvent.clientY);
      const end = toContent(endFrac, metricsRef.current);
      const shape = {
        x: Math.min(start.x, end.x), y: Math.min(start.y, end.y),
        w: Math.abs(end.x - start.x), h: Math.abs(end.y - start.y),
      };
      setRect(null);
      const selector: Selector = shape.w < 0.02 || shape.h < 0.02
        ? { type: "point", ...start }
        : { type: "rect", ...shape };
      const quoteTarget = new DOMRect(
        upBox.left + Math.min(startFrac.x, endFrac.x) * upBox.width,
        upBox.top + Math.min(startFrac.y, endFrac.y) * upBox.height,
        Math.abs(endFrac.x - startFrac.x) * upBox.width,
        Math.abs(endFrac.y - startFrac.y) * upBox.height,
      );
      props.onDraft(attachQuote(
        { cardId: node.id, selector, at: { x: upEvent.clientX, y: upEvent.clientY } }, quoteTarget));
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  const pins = [
    ...annotations.map((a) => ({ annotation: a, selector: a.selector })),
    ...(draft && draft.cardId === node.id
      ? [{ annotation: null as Annotation | null, selector: draft.selector }]
      : []),
  ];

  return (
    <div ref={layerRef} className={`annotation-layer${active ? " active" : ""}`} onPointerDown={onPointerDown}>
      {pins.map(({ annotation, selector }, index) => {
        if (!selector) {
          return annotation ? (
            <div key={annotation.id}
                 className={`pin whole${annotation.stale ? " stale" : ""}${selectedId === annotation.id ? " selected" : ""}`}
                 title={annotation.body}
                 onPointerDown={(e) => { e.stopPropagation(); props.onSelect(annotation.id); }} />
          ) : null;
        }
        const key = annotation?.id ?? `draft-${index}`;
        const classes = [
          annotation?.stale ? "stale" : "",
          annotation && selectedId === annotation.id ? "selected" : "",
          annotation ? "" : "draft",
        ].join(" ");
        const onDown = (e: React.PointerEvent) => {
          if (!annotation) return;
          e.stopPropagation();
          props.onSelect(annotation.id);
        };
        return selector.type === "point" ? (
          <div key={key} className={`pin point ${classes}`}
               style={pinStyle({ x: selector.x, y: selector.y }, metrics)}
               title={annotation?.body ?? "new comment"} onPointerDown={onDown} />
        ) : (
          // A highlight covers the very text it is about, so outside comment
          // mode only its marker takes the pointer: the text under it stays
          // selectable and an html card's controls stay clickable.
          <div key={key} className={`pin rect${highlights ? " highlight" : ""} ${classes}`}
               style={pinStyle(selector, metrics)}
               title={annotation?.body ?? "new comment"} onPointerDown={onDown}
               data-annotation-id={annotation?.id}>
            {annotation && <span className="pin-marker" title={annotation.body} onPointerDown={onDown} />}
          </div>
        );
      })}
      {pending && !active && !(draft && draft.cardId === node.id) && (
        <button type="button" className="selection-comment"
                style={selectionButtonStyle(pending.selector, metrics)}
                title="Comment on the selected text"
                // Pressing must not collapse the selection it is about to quote.
                onPointerDown={(e) => { e.preventDefault(); e.stopPropagation(); }}
                onMouseDown={(e) => e.preventDefault()}
                onClick={takeSelection}>
          Comment
        </button>
      )}
      {rect && (
        <div className="pin rect draft"
             style={{
               left: `${rect.x * 100}%`, top: `${rect.y * 100}%`,
               width: `${rect.w * 100}%`, height: `${rect.h * 100}%`,
             }} />
      )}
    </div>
  );
}

const MOTIVATIONS: { id: Motivation; hint: string }[] = [
  { id: "commenting", hint: "context, no action" },
  { id: "assessing", hint: "a verdict" },
  { id: "editing", hint: "an instruction" },
];

const COMPOSER_W = 440;
const COMPOSER_H = 230;

/**
 * Beside the gesture that made the draft: below it where there is room, else
 * above, and always on screen. Null leaves the composer docked at the bottom,
 * which is also where it stays on a phone.
 */
export function composerPosition(
  at: { x: number; y: number } | undefined,
  viewport: { w: number; h: number },
): { left: number; top: number } | null {
  if (!at || viewport.w <= 760) return null;
  const w = Math.min(COMPOSER_W, viewport.w - 16);
  const left = Math.min(Math.max(at.x - 24, 8), viewport.w - w - 8);
  const below = at.y + 14;
  const top = below + COMPOSER_H <= viewport.h - 8 ? below : Math.max(8, at.y - COMPOSER_H - 14);
  return { left, top };
}

/** The composer that appears once a pin has been dropped or text selected. */
export function AnnotationComposer(props: {
  draft: DraftAnnotation;
  cardTitle: string;
  onCancel: () => void;
  onDismissQuote?: () => void;
  onSubmit: (body: string, motivation: Motivation) => void;
}) {
  const [body, setBody] = useState(props.draft.body ?? "");
  const [motivation, setMotivation] = useState<Motivation>("commenting");
  const where = props.draft.selector === null
    ? "whole card"
    : props.draft.fromSelection ? "selected text"
    : props.draft.selector.type === "point" ? "a point" : "a region";
  const quote = props.draft.quote;
  // The quote travels in the body: agents read bodies, and no client needs to
  // learn a new field. The human sees exactly what will be sent.
  const withQuote = (text: string) => (quote ? `> ${quote}\n\n${text}` : text);
  const submit = () => {
    if (body.trim()) props.onSubmit(withQuote(body.trim()), motivation);
  };
  const place = composerPosition(props.draft.at, { w: window.innerWidth, h: window.innerHeight });

  return (
    <form
      className={`composer${place ? " anchored" : ""}`}
      style={place ?? undefined}
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <div className="composer-head">
        Comment on <strong>{props.cardTitle}</strong> · {where}
      </div>
      {quote && (
        <div className="composer-quote">
          <span>“{quote}”</span>
          <button type="button" className="ghost" title="Don't quote the selected text"
                  onClick={props.onDismissQuote}>×</button>
        </div>
      )}
      <textarea autoFocus value={body} placeholder="What should the agent know?"
                onChange={(e) => setBody(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Escape") props.onCancel();
                  // Enter sends, as in a chat; Shift-Enter is the line break. An
                  // IME's Enter only commits its candidate.
                  if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    submit();
                  }
                }} />
      <div className="composer-foot">
        <div className="motivation-chips" role="radiogroup" aria-label="Kind of comment">
          {MOTIVATIONS.map((m) => (
            <button key={m.id} type="button" role="radio" aria-checked={motivation === m.id}
                    className={`chip ${m.id}${motivation === m.id ? " on" : ""}`}
                    title={`${m.id} — ${m.hint}`}
                    onClick={() => setMotivation(m.id)}>
              {m.id}
            </button>
          ))}
        </div>
        <button type="button" className="ghost" onClick={props.onCancel}>Cancel</button>
        <button type="submit" disabled={!body.trim()}>Comment</button>
      </div>
      <div className="composer-hint">Enter to comment · Shift-Enter for a new line · Esc to cancel</div>
    </form>
  );
}

/** The right-hand comment list. */
export function AnnotationPanel(props: {
  annotations: Annotation[];
  showResolved: boolean;
  selectedId: string | null;
  onToggleResolved: () => void;
  onSelect: (id: string) => void;
  onResolve: (id: string, reply: string) => void;
  onReopen: (id: string) => void;
  /** Present when the panel is a dismissable sheet (narrow screens). */
  onClose?: () => void;
}) {
  const [replies, setReplies] = useState<Record<string, string>>({});
  const listRef = useRef<HTMLUListElement>(null);
  const shown = props.annotations.filter((a) => props.showResolved || !a.resolved);

  // A comment chosen on a card is brought into view here, the other half of
  // the list → pin link.
  useEffect(() => {
    if (!props.selectedId) return;
    const item = Array.from(listRef.current?.querySelectorAll<HTMLElement>("[data-annotation-id]") ?? [])
      .find((el) => el.dataset.annotationId === props.selectedId);
    if (item && typeof item.scrollIntoView === "function") {
      item.scrollIntoView({ block: "nearest", behavior: reducedMotion() ? "auto" : "smooth" });
    }
  }, [props.selectedId]);

  return (
    <div className="panel comments-panel">
      <header>
        <h2>Comments</h2>
        <span className="panel-actions">
          <label className="toggle">
            <input type="checkbox" checked={props.showResolved} onChange={props.onToggleResolved} />
            resolved
          </label>
          {props.onClose && (
            <button className="icon" onClick={props.onClose} aria-label="Close">✕</button>
          )}
        </span>
      </header>
      {shown.length === 0 && <p className="empty">No comments yet. Select text on a card to comment on it, or use comment mode to pin a point.</p>}
      <ul ref={listRef}>
        {shown.map((a) => (
          <li key={a.id} data-annotation-id={a.id}
              className={`comment${a.resolved ? " resolved" : ""}${a.stale ? " stale" : ""}${props.selectedId === a.id ? " selected" : ""}${isPending(a) ? " pending" : ""}`}
              onClick={() => props.onSelect(a.id)}>
            <div className="comment-head">
              <span className={`motivation ${a.motivation}`}>{a.motivation}</span>
              <span className="on">{a.card_title || a.card_id}</span>
              {a.resolved && <span className="badge resolved-status">resolved</span>}
              {a.stale && <span className="badge stale" title="The card changed after this was written">content changed since</span>}
              {a.card_superseded_by && (
                <span className="badge" title={`Revised into ${a.card_superseded_by}`}>revised</span>
              )}
            </div>
            <p className="comment-body">{a.body}</p>
            <div className="comment-meta">
              <span className={`comment-author ${a.creator_kind}`}>{a.creator}</span>
              {" · "}{new Date(a.created_at).toLocaleString()}
            </div>
            {/* A comment the server has not named yet has nothing to resolve. */}
            {isPending(a) ? null : a.resolved ? (
              <div className="comment-reply">
                {a.resolved_reply && (
                  <div className="reply-bubble">
                    <span className="who">reply</span>
                    {a.resolved_reply}
                  </div>
                )}
                <button className="ghost" onClick={(e) => { e.stopPropagation(); props.onReopen(a.id); }}>reopen</button>
              </div>
            ) : (
              <div className="comment-actions" onClick={(e) => e.stopPropagation()}>
                <input placeholder="reply (optional)" value={replies[a.id] ?? ""}
                       onChange={(e) => setReplies({ ...replies, [a.id]: e.target.value })} />
                <button onClick={() => props.onResolve(a.id, replies[a.id] ?? "")}>resolve</button>
              </div>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
