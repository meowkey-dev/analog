import { useState } from "react";
import type { Annotation, Motivation, Node, Selector } from "./api";

/**
 * The annotation layer (SPEC §5).
 *
 * The overlay lives in the parent document, *above* the card — including above an
 * html card's iframe. Because the iframe is sandboxed without allow-same-origin,
 * agent HTML can neither read the annotations nor forge one. The overlay only
 * accepts pointer events while annotate mode is on, so cards stay interactive
 * the rest of the time.
 */

export interface DraftAnnotation {
  cardId: string;
  selector: Selector;
}

interface OverlayProps {
  node: Node;
  annotations: Annotation[];
  active: boolean;
  draft: DraftAnnotation | null;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  onDraft: (draft: DraftAnnotation) => void;
}

/** Rendered inside each card. Pins are fractions of the card box, so they survive resize. */
export function AnnotationOverlay(props: OverlayProps) {
  const { node, annotations, active, draft, selectedId } = props;
  const [rect, setRect] = useState<{ x: number; y: number; w: number; h: number } | null>(null);

  const toFraction = (event: React.PointerEvent) => {
    const box = event.currentTarget.getBoundingClientRect();
    return {
      x: Math.min(1, Math.max(0, (event.clientX - box.left) / box.width)),
      y: Math.min(1, Math.max(0, (event.clientY - box.top) / box.height)),
    };
  };

  const onPointerDown = (event: React.PointerEvent) => {
    if (!active) return;
    event.stopPropagation();
    event.preventDefault();
    const start = toFraction(event);
    const target = event.currentTarget as HTMLElement;
    target.setPointerCapture(event.pointerId);

    // Shift-drag draws a rect; a plain click drops a point.
    if (!event.shiftKey) {
      props.onDraft({ cardId: node.id, selector: { type: "point", ...start } });
      return;
    }
    setRect({ ...start, w: 0, h: 0 });

    const move = (moveEvent: PointerEvent) => {
      const box = target.getBoundingClientRect();
      const x = Math.min(1, Math.max(0, (moveEvent.clientX - box.left) / box.width));
      const y = Math.min(1, Math.max(0, (moveEvent.clientY - box.top) / box.height));
      setRect({
        x: Math.min(start.x, x), y: Math.min(start.y, y),
        w: Math.abs(x - start.x), h: Math.abs(y - start.y),
      });
    };
    const up = (upEvent: PointerEvent) => {
      target.releasePointerCapture(event.pointerId);
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      const box = target.getBoundingClientRect();
      const x = Math.min(1, Math.max(0, (upEvent.clientX - box.left) / box.width));
      const y = Math.min(1, Math.max(0, (upEvent.clientY - box.top) / box.height));
      const shape = {
        x: Math.min(start.x, x), y: Math.min(start.y, y),
        w: Math.abs(x - start.x), h: Math.abs(y - start.y),
      };
      setRect(null);
      props.onDraft({
        cardId: node.id,
        selector: shape.w < 0.02 || shape.h < 0.02
          ? { type: "point", ...start }
          : { type: "rect", ...shape },
      });
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
               style={{ left: `${selector.x * 100}%`, top: `${selector.y * 100}%` }}
               title={annotation?.body ?? "new comment"} onPointerDown={onDown} />
        ) : (
          <div key={key} className={`pin rect ${classes}`}
               style={{
                 left: `${selector.x * 100}%`, top: `${selector.y * 100}%`,
                 width: `${selector.w * 100}%`, height: `${selector.h * 100}%`,
               }}
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
  onSubmit: (body: string, motivation: Motivation) => void;
}) {
  const [body, setBody] = useState("");
  const [motivation, setMotivation] = useState<Motivation>("commenting");
  const where = props.draft.selector === null
    ? "whole card"
    : props.draft.selector.type === "point" ? "a point" : "a region";

  return (
    <form
      className="composer"
      onSubmit={(event) => {
        event.preventDefault();
        if (body.trim()) props.onSubmit(body.trim(), motivation);
      }}
    >
      <div className="composer-head">
        Comment on <strong>{props.cardTitle}</strong> · {where}
      </div>
      <textarea autoFocus value={body} placeholder="What should the agent know?"
                onChange={(e) => setBody(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Escape") props.onCancel();
                  if (e.key === "Enter" && (e.metaKey || e.ctrlKey) && body.trim()) {
                    props.onSubmit(body.trim(), motivation);
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
            </div>
            <p className="comment-body">{a.body}</p>
            <div className="comment-meta">{a.creator} · {new Date(a.created_at).toLocaleString()}</div>
            {a.resolved ? (
              <div className="comment-reply">
                <strong>resolved</strong>{a.resolved_reply ? `: ${a.resolved_reply}` : ""}
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
