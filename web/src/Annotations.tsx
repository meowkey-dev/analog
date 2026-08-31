import { useEffect, useRef, useState } from "react";
import type { Annotation, Motivation, Node, Selector } from "./api";

/**
 * The annotation layer (SPEC §5).
 *
 * The overlay lives in the parent document, *above* the card — including above an
 * html card's iframe. Because the iframe is sandboxed without allow-same-origin,
 * agent HTML can neither read the annotations nor forge one. The overlay only
 * accepts pointer events while annotate mode is on, so cards stay interactive
 * the rest of the time.
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
}

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
  // Handlers fire between renders; the mirrors are what they read.
  const metricsRef = useRef<ScrollMetrics | null>(null);
  const draftRef = useRef<DraftAnnotation | null>(draft);
  draftRef.current = draft;

  const kind = node.type === "file" ? "file" : (node.sp_kind ?? "plain");

  useEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    if (el instanceof HTMLIFrameElement) {
      // The sandbox blocks reading the iframe document, so the injected reporter
      // measures it from inside and posts here. event.source — not origin, which
      // is opaque — proves the message came from this frame. A forged value can
      // only displace pins or alter a quote the human still reviews before
      // submitting; it cannot create or read an annotation.
      const onMessage = (event: MessageEvent) => {
        if (event.source !== el.contentWindow) return;
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
      props.onDraft(attachQuote({ cardId: node.id, selector },
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
      props.onDraft(attachQuote({ cardId: node.id, selector }, quoteTarget));
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
    <div className={`annotation-layer${active ? " active" : ""}`} onPointerDown={onPointerDown}>
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
          <div key={key} className={`pin rect ${classes}`}
               style={pinStyle(selector, metrics)}
               title={annotation?.body ?? "new comment"} onPointerDown={onDown} />
        );
      })}
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

/** The composer that appears once a pin has been dropped. */
export function AnnotationComposer(props: {
  draft: DraftAnnotation;
  cardTitle: string;
  onCancel: () => void;
  onDismissQuote?: () => void;
  onSubmit: (body: string, motivation: Motivation) => void;
}) {
  const [body, setBody] = useState("");
  const [motivation, setMotivation] = useState<Motivation>("commenting");
  const where = props.draft.selector === null
    ? "whole card"
    : props.draft.selector.type === "point" ? "a point" : "a region";
  const quote = props.draft.quote;
  // The quote travels in the body: agents read bodies, and no client needs to
  // learn a new field. The human sees exactly what will be sent.
  const withQuote = (text: string) => (quote ? `> ${quote}\n\n${text}` : text);

  return (
    <form
      className="composer"
      onSubmit={(event) => {
        event.preventDefault();
        if (body.trim()) props.onSubmit(withQuote(body.trim()), motivation);
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
                  if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && body.trim()) {
                    props.onSubmit(withQuote(body.trim()), motivation);
                  }
                }} />
      <div className="composer-foot">
        <select value={motivation} onChange={(e) => setMotivation(e.target.value as Motivation)}>
          <option value="commenting">commenting — context, no action</option>
          <option value="assessing">assessing — a verdict</option>
          <option value="editing">editing — an instruction</option>
        </select>
        <button type="button" className="ghost" onClick={props.onCancel}>Cancel</button>
        <button type="submit" disabled={!body.trim()}>Comment</button>
      </div>
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
}) {
  const [replies, setReplies] = useState<Record<string, string>>({});
  const shown = props.annotations.filter((a) => props.showResolved || !a.resolved);

  return (
    <div className="panel comments-panel">
      <header>
        <h2>Comments</h2>
        <label className="toggle">
          <input type="checkbox" checked={props.showResolved} onChange={props.onToggleResolved} />
          resolved
        </label>
      </header>
      {shown.length === 0 && <p className="empty">No comments yet. Turn on comment mode and click a card.</p>}
      <ul>
        {shown.map((a) => (
          <li key={a.id}
              className={`comment${a.resolved ? " resolved" : ""}${a.stale ? " stale" : ""}${props.selectedId === a.id ? " selected" : ""}`}
              onClick={() => props.onSelect(a.id)}>
            <div className="comment-head">
              <span className={`motivation ${a.motivation}`}>{a.motivation}</span>
              <span className="on">{a.card_title || a.card_id}</span>
              {a.stale && <span className="badge stale" title="The card changed after this was written">content changed since</span>}
              {a.card_superseded_by && (
                <span className="badge" title={`Revised into ${a.card_superseded_by}`}>revised</span>
              )}
            </div>
            <p className="comment-body">{a.body}</p>
            <div className="comment-meta">{a.creator} · {new Date(a.created_at).toLocaleString()}</div>
            {a.resolved ? (
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
