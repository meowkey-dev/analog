import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Body, loadMdTheme } from "./Card";
import { DiffView, diffFor } from "./Diff";
import type { DraftAnnotation } from "./Annotations";
import type { AnalogEvent, Annotation, Edge, Node } from "./api";
import { boardOrder, neighbors, ORDERS, readingOrder, type ReadingOrder } from "./reading-order";
import { describeChange, type CardChange } from "./seen";

/**
 * One card at a time, full width, prev/next by swipe, arrow keys or the bar.
 *
 * A free canvas is the wrong shape for a phone: cards are either too small to
 * read or too big to see around. The reader walks the same cards in an order
 * (reading-order.ts) and keeps the things a canvas shows spatially — links,
 * revisions, comments, what changed — as one tap away from the card.
 */

/** Horizontal travel, px, that counts as a swipe. */
const SWIPE = 60;

const NOOP = () => {};

export interface ReaderProps {
  /** Live (not deleted) cards, superseded ones included so revisions can be visited. */
  nodes: Node[];
  edges: Edge[];
  annotations: Annotation[];
  events: AnalogEvent[];
  changes: Map<string, CardChange>;
  lastChanges: Map<string, string>;
  currentId: string | null;
  order: ReadingOrder;
  /** Bumped to open the editor on the current card (a quick-added card). */
  editRequest: number;
  onOrder: (order: ReadingOrder) => void;
  onNavigate: (id: string) => void;
  onMarkSeen: (id: string) => void;
  onMarkAllSeen: () => void;
  onEdit: (id: string, text: string, title: string) => void;
  onDraft: (draft: DraftAnnotation) => void;
  onResolve: (id: string, reply: string) => void;
  onReopen: (id: string) => void;
  onShowOnCanvas: (id: string) => void;
  onFind: () => void;
  onPopOut: (node: Node) => void;
  onQuickAdd: () => void;
  notify: (message: string) => void;
  /** Told when scrolling the card should hide (true) or bring back (false) the app's top bar. */
  onTopbar?: (hidden: boolean) => void;
}

/** Scroll travel, px, in one direction before the top bar follows it. */
const TOPBAR_TRAVEL = 24;
/** Hiding the top bar grows the body and can clamp its scroll; ignore that echo. */
const TOPBAR_SETTLE_MS = 300;

export interface TopbarScroll { anchor: number; last: number; settleUntil: number }

/**
 * Whether a scroll to `y` should hide (true) or show (false) the top bar, or leave
 * it (null). Direction is measured from where it last turned, so a slow drag still
 * counts; `room` is how far the body can scroll, and a card that barely overflows
 * never hides it, or the bar would flap as the body grows and shrinks.
 */
export function topbarAfterScroll(s: TopbarScroll, y: number, room: number, now: number): boolean | null {
  if (now < s.settleUntil) {
    s.anchor = s.last = y;
    return null;
  }
  if ((y - s.last) * (s.last - s.anchor) < 0) s.anchor = s.last;
  s.last = y;
  if (y <= TOPBAR_TRAVEL) return false;
  if (y - s.anchor > TOPBAR_TRAVEL) return room > 4 * TOPBAR_TRAVEL ? true : null;
  if (s.anchor - y > TOPBAR_TRAVEL) return false;
  return null;
}

/** Whether something between `start` and `stop` scrolls sideways, and so owns the swipe. */
function scrollsSideways(start: Element | null, stop: Element): boolean {
  for (let el = start; el && el !== stop; el = el.parentElement) {
    if (!(el instanceof HTMLElement)) continue;
    const style = getComputedStyle(el);
    if (/auto|scroll/.test(style.overflowX) && el.scrollWidth > el.clientWidth) return true;
  }
  return false;
}

export function Reader(props: ReaderProps) {
  const { nodes, changes, order } = props;
  const byId = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);
  const changed = useMemo(() => new Set(changes.keys()), [changes]);

  // "new since last look" is a queue: marking a card seen must not pull it out
  // from under the reader, so the list only ever grows while that order is on.
  const queue = useRef<string[]>([]);
  const sequence = useMemo(() => {
    const current = nodes.filter((n) => !n.sp_superseded_by);
    if (order !== "changes") {
      queue.current = [];
      return readingOrder(order, current, props.events, props.annotations, changed);
    }
    const live = new Map(current.map((n) => [n.id, n]));
    const kept = queue.current.filter((id) => live.has(id));
    const fresh = boardOrder(current.filter((n) => changed.has(n.id) && !kept.includes(n.id)));
    queue.current = [...kept, ...fresh.map((n) => n.id)];
    return queue.current.map((id) => live.get(id)!);
  }, [nodes, order, props.events, props.annotations, changed]);

  // An empty queue is "caught up", whatever card was showing before it was chosen.
  const emptyQueue = order === "changes" && sequence.length === 0;
  const node = emptyQueue
    ? undefined
    : (props.currentId ? byId.get(props.currentId) : undefined) ?? sequence[0];
  const index = node ? sequence.findIndex((n) => n.id === node.id) : -1;
  // Off the sequence (a revision reached by its link, a card outside the queue),
  // prev/next return to where the reader left it.
  const anchor = useRef(0);
  if (index >= 0) anchor.current = index;

  // Tell the app which card is showing, so the URL and "seen" can follow it.
  useEffect(() => {
    if (node && node.id !== props.currentId) props.onNavigate(node.id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [node?.id]);

  // A new order starts from its top.
  const lastOrder = useRef(order);
  useEffect(() => {
    if (lastOrder.current === order) return;
    lastOrder.current = order;
    const first = sequence[0];
    if (first) props.onNavigate(first.id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [order, sequence]);

  const go = useCallback((step: -1 | 1) => {
    if (sequence.length === 0) return;
    const target = index >= 0
      ? index + step
      : Math.min(anchor.current, sequence.length - 1);
    const next = sequence[target];
    if (next) props.onNavigate(next.id);
  }, [index, sequence, props]);

  const [view, setView] = useState<"content" | "diff">("content");
  const [sheet, setSheet] = useState<"comments" | "edit" | null>(null);
  const [offset, setOffset] = useState(0);

  useEffect(() => {
    setView("content");
    setSheet((s) => (s === "edit" ? null : s));
  }, [node?.id]);

  useEffect(() => {
    if (props.editRequest > 0) setSheet("edit");
  }, [props.editRequest]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      const tag = target?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      if (event.key === "ArrowLeft" || event.key === "k") go(-1);
      if (event.key === "ArrowRight" || event.key === "j") go(1);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [go]);

  // --- swipe -------------------------------------------------------------------
  // The body is touch-action: pan-y, so a sideways drag reaches us as pointer
  // events while an upward one still scrolls the card natively.

  const stage = useRef<HTMLDivElement>(null);
  const topbar = useRef<TopbarScroll & { hidden: boolean }>({ anchor: 0, last: 0, settleUntil: 0, hidden: false });
  const { onTopbar } = props;
  const setTopbar = useCallback((hidden: boolean) => {
    const t = topbar.current;
    if (t.hidden === hidden) return;
    t.hidden = hidden;
    t.settleUntil = performance.now() + TOPBAR_SETTLE_MS;
    onTopbar?.(hidden);
  }, [onTopbar]);
  const onScroll = (event: React.UIEvent<HTMLDivElement>) => {
    if (!onTopbar) return;
    const el = event.currentTarget;
    const want = topbarAfterScroll(topbar.current, el.scrollTop, el.scrollHeight - el.clientHeight, performance.now());
    if (want !== null) setTopbar(want);
  };
  // Every card starts at its top, with the bar back.
  useEffect(() => {
    stage.current?.scrollTo?.(0, 0);
    topbar.current.anchor = topbar.current.last = 0;
    setTopbar(false);
  }, [node?.id, setTopbar]);
  useEffect(() => () => onTopbar?.(false), [onTopbar]);
  const swipe = useRef<{ id: number; x: number; y: number } | null>(null);

  const onPointerDown = (event: React.PointerEvent) => {
    if (event.pointerType === "mouse" || sheet) return;
    if (scrollsSideways(event.target as Element, event.currentTarget)) return;
    swipe.current = { id: event.pointerId, x: event.clientX, y: event.clientY };
  };
  const onPointerMove = (event: React.PointerEvent) => {
    const s = swipe.current;
    if (!s || s.id !== event.pointerId) return;
    const dx = event.clientX - s.x;
    const dy = event.clientY - s.y;
    if (Math.abs(dx) > Math.abs(dy)) setOffset(dx * 0.35);
  };
  const endSwipe = (event: React.PointerEvent) => {
    const s = swipe.current;
    swipe.current = null;
    setOffset(0);
    if (!s || s.id !== event.pointerId || event.type === "pointercancel") return;
    const dx = event.clientX - s.x;
    const dy = event.clientY - s.y;
    if (Math.abs(dx) >= SWIPE && Math.abs(dx) > 1.5 * Math.abs(dy)) go(dx < 0 ? 1 : -1);
  };

  const share = async () => {
    if (!node) return;
    const url = `${window.location.origin}${window.location.pathname}#${node.id}`;
    try {
      if (navigator.share) {
        await navigator.share({ title: node.sp_title || node.id, url });
        return;
      }
      await navigator.clipboard.writeText(url);
      props.notify("Link to this card copied.");
    } catch {
      // the share sheet was dismissed; nothing to report
    }
  };

  // --- empty states --------------------------------------------------------------

  const orderPicker = (
    <select className="reader-order" value={order} aria-label="Reading order"
            onChange={(e) => props.onOrder(e.target.value as ReadingOrder)}>
      {ORDERS.map((o) => (
        <option key={o.id} value={o.id}>
          {o.id === "changes" ? `${o.label} (${changes.size})` : o.label}
        </option>
      ))}
    </select>
  );

  if (!node) {
    const caughtUp = order === "changes";
    return (
      <div className="reader">
        <div className="reader-head">
          <span className="reader-pos">0 / 0</span>
          {orderPicker}
        </div>
        <div className="reader-empty">
          {caughtUp ? (
            <>
              <p className="big">You're all caught up.</p>
              <p>Nothing on this board changed since you last looked.</p>
              <button onClick={() => props.onOrder("board")}>Read the board in order</button>
            </>
          ) : (
            <>
              <p className="big">This board is empty.</p>
              <button onClick={props.onQuickAdd}>Add a card</button>
            </>
          )}
        </div>
      </div>
    );
  }

  const kind = node.type === "file" ? "file" : (node.sp_kind ?? "plain");
  const superseded = Boolean(node.sp_superseded_by);
  const successor = node.sp_superseded_by ? byId.get(node.sp_superseded_by) : undefined;
  const change = superseded ? undefined : changes.get(node.id);
  const diff = diffFor(node, superseded ? successor ?? {} : undefined,
    change?.kind === "edited" ? change.before : undefined, props.lastChanges.get(node.id));
  const thread = props.annotations.filter((a) => a.card_id === node.id);
  const open = thread.filter((a) => !a.resolved).length;
  const links = neighbors(node.id, props.edges, byId);
  const editable = node.type === "text" && !superseded && kind !== "svg";
  const atEnd = index >= 0 && index === sequence.length - 1;

  return (
    <div className="reader">
      <div className="reader-head">
        <button className="reader-pos" onClick={props.onFind} title="Jump to a card (⌘K)">
          {index >= 0 ? `${index + 1} / ${sequence.length}` : `– / ${sequence.length}`}
        </button>
        {orderPicker}
        <span className="spacer" />
        <button className="icon" onClick={share} title="Share a link to this card">⤴</button>
        <button className="icon" onClick={() => props.onShowOnCanvas(node.id)}
                title="Show this card on the canvas">◱</button>
      </div>

      {order !== "changes" && changes.size > 0 && (
        <button className="reader-nudge" onClick={() => props.onOrder("changes")}>
          {changes.size === 1 ? "1 card changed" : `${changes.size} cards changed`} since you last
          looked — review
        </button>
      )}

      <div
        ref={stage}
        className="reader-body"
        style={offset ? { transform: `translateX(${offset}px)` } : undefined}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endSwipe}
        onPointerCancel={endSwipe}
        onScroll={onScroll}
      >
        {/* The title scrolls with the card: on a phone a fixed title row is one bar too many. */}
        <div className="reader-title">
          <h1>{node.sp_title || node.id}</h1>
          <span className="card-kind">{kind}</span>
          {(node.sp_rev ?? 1) > 1 && <span className="badge">rev {node.sp_rev}</span>}
          {kind === "html" && (
            <button className="icon" title="Open full window" onClick={() => props.onPopOut(node)}>⤢</button>
          )}
        </div>

        {superseded && (
          <div className="superseded-note">
            <span>superseded — read only</span>
            {successor && (
              <button className="linkish" onClick={() => props.onNavigate(successor.id)}>
                go to the current revision →
              </button>
            )}
          </div>
        )}
        {(change || diff) && (
          <div className={`change-note ${change?.kind ?? "last"}`}>
            <span className="what">{change ? describeChange(change) : diff!.label}</span>
            {diff && (
              <span className="diff-toggle">
                <button className={view === "content" ? "on" : ""} onClick={() => setView("content")}>content</button>
                <button className={view === "diff" ? "on" : ""} onClick={() => setView("diff")}
                        title={`Show ${diff.label}`}>diff</button>
              </span>
            )}
            {change && (
              <button className="seen" onClick={() => { setView("content"); props.onMarkSeen(node.id); }}>
                ✓ seen
              </button>
            )}
          </div>
        )}

        {view === "diff" && diff
          ? <DiffView before={diff.before} after={diff.after} same={diff.same} />
          : <Body key={node.id} node={node} mdTheme={loadMdTheme(node.id)} bodyRef={NOOP} onHTMLLoad={NOOP} />}

        {links.length > 0 && (
          <nav className="reader-links" aria-label="Linked cards">
            {links.map(({ edge, node: other, direction }) => (
              <button key={edge.id} className={`chip ${direction}`}
                      style={edge.color ? { borderColor: edge.color } : undefined}
                      onClick={() => props.onNavigate(other.id)}>
                <span className="dir">{direction === "out" ? "→" : "←"}</span>
                {edge.label && <span className="label">{edge.label}</span>}
                <span className="title">{other.sp_title || other.id}</span>
              </button>
            ))}
          </nav>
        )}

        {atEnd && order === "changes" && (
          <div className="reader-end">
            <span>That's everything that changed.</span>
            <button onClick={() => { props.onMarkAllSeen(); props.onOrder("board"); }}>
              Mark all seen
            </button>
          </div>
        )}
      </div>

      <div className="reader-bar">
        <button onClick={() => go(-1)} disabled={index === 0} aria-label="Previous card">‹</button>
        <button className={sheet === "comments" ? "on" : ""}
                onClick={() => setSheet((s) => (s === "comments" ? null : "comments"))}>
          💬{open > 0 ? ` ${open}` : thread.length > 0 ? "" : " +"}
        </button>
        <button onClick={() => setSheet("edit")} disabled={!editable}
                title={editable ? "Edit this card" : "This card can't be edited here"}>✎</button>
        <button onClick={props.onQuickAdd} title="Add a card">＋</button>
        <button onClick={() => go(1)} disabled={atEnd} aria-label="Next card">›</button>
      </div>

      {sheet === "comments" && (
        <div className="sheet" role="dialog" aria-label="Comments on this card">
          <header>
            <h2>Comments</h2>
            <button className="icon" onClick={() => setSheet(null)} aria-label="Close">✕</button>
          </header>
          <Thread annotations={thread} onResolve={props.onResolve} onReopen={props.onReopen} />
          {!superseded && (
            <button className="sheet-action"
                    onClick={() => { setSheet(null); props.onDraft({ cardId: node.id, selector: null }); }}>
              Comment on this card
            </button>
          )}
        </div>
      )}

      {sheet === "edit" && editable && (
        <Editor key={node.id} node={node}
                onCancel={() => setSheet(null)}
                onSave={(text, title) => { setSheet(null); props.onEdit(node.id, text, title); }} />
      )}
    </div>
  );
}

function Thread(props: {
  annotations: Annotation[];
  onResolve: (id: string, reply: string) => void;
  onReopen: (id: string) => void;
}) {
  const [replies, setReplies] = useState<Record<string, string>>({});
  if (props.annotations.length === 0) return <p className="empty">No comments on this card yet.</p>;
  // Open first: they are the work; resolved ones are history.
  const sorted = [...props.annotations].sort((a, b) => Number(a.resolved) - Number(b.resolved));
  return (
    <ul className="sheet-list">
      {sorted.map((a) => (
        <li key={a.id} className={`comment${a.resolved ? " resolved" : ""}${a.stale ? " stale" : ""}`}>
          <div className="comment-head">
            <span className={`motivation ${a.motivation}`}>{a.motivation}</span>
            <span className={`comment-author ${a.creator_kind}`}>{a.creator}</span>
            {a.resolved && <span className="badge resolved-status">resolved</span>}
            {a.stale && <span className="badge stale">content changed since</span>}
          </div>
          <p className="comment-body">{a.body}</p>
          {a.resolved ? (
            <div className="comment-reply">
              {a.resolved_reply && (
                <div className="reply-bubble"><span className="who">reply</span>{a.resolved_reply}</div>
              )}
              <button className="ghost" onClick={() => props.onReopen(a.id)}>reopen</button>
            </div>
          ) : (
            <div className="comment-actions">
              <input placeholder="reply (optional)" value={replies[a.id] ?? ""}
                     onChange={(e) => setReplies({ ...replies, [a.id]: e.target.value })} />
              <button onClick={() => props.onResolve(a.id, replies[a.id] ?? "")}>resolve</button>
            </div>
          )}
        </li>
      ))}
    </ul>
  );
}

/** Plain text editing, deliberately: title and source, no toolbar. */
function Editor(props: { node: Node; onCancel: () => void; onSave: (text: string, title: string) => void }) {
  const [title, setTitle] = useState(props.node.sp_title ?? "");
  const [text, setText] = useState(props.node.text ?? "");
  const dirty = title !== (props.node.sp_title ?? "") || text !== (props.node.text ?? "");
  return (
    <form className="reader-editor" onSubmit={(e) => { e.preventDefault(); props.onSave(text, title); }}>
      <header>
        <button type="button" className="ghost" onClick={props.onCancel}>Cancel</button>
        <span className="card-kind">{props.node.sp_kind ?? "plain"}</span>
        <button type="submit" disabled={!dirty}>Save</button>
      </header>
      <input value={title} placeholder="Title" aria-label="Title" onChange={(e) => setTitle(e.target.value)} />
      <textarea value={text} autoFocus aria-label="Content"
                placeholder={props.node.sp_kind === "md" ? "Markdown…" : "Content…"}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Escape") props.onCancel();
                  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) props.onSave(text, title);
                }} />
    </form>
  );
}
