import type { Node } from "./api";
import { describeChange, type CardChange } from "./seen";

/**
 * What changed on the board since this browser last looked (seen.ts): new
 * cards, edits, comments from someone else. Clicking a row goes to the card;
 * "seen" acknowledges it, and the diff is on the card itself.
 */
export function Changes(props: {
  changes: Map<string, CardChange>;
  nodes: Node[];
  onFocus: (id: string) => void;
  onMarkSeen: (id: string) => void;
  onMarkAllSeen: () => void;
  onReview: () => void;
  onClose?: () => void;
}) {
  const rows = props.nodes.filter((n) => props.changes.has(n.id));
  return (
    <div className="panel changes-panel">
      <header>
        <h2>Changes</h2>
        <span className="panel-actions">
          {rows.length > 0 && (
            <>
              <button className="ghost" onClick={props.onReview} title="Walk the changed cards one at a time">
                review
              </button>
              <button className="ghost" onClick={props.onMarkAllSeen}>mark all seen</button>
            </>
          )}
          {props.onClose && (
            <button className="icon" onClick={props.onClose} aria-label="Close">✕</button>
          )}
        </span>
      </header>
      {rows.length === 0 && (
        <p className="empty">Nothing changed since you last looked. Edits, new cards and new
        comments from agents show up here, with a diff on the card.</p>
      )}
      <ul>
        {rows.map((node) => {
          const change = props.changes.get(node.id)!;
          return (
            <li key={node.id} className={`change-row ${change.kind}`} onClick={() => props.onFocus(node.id)}>
              <span className={`dot ${change.kind}`} />
              <span className="title">{node.sp_title || node.id}</span>
              <span className="what">{describeChange(change)}</span>
              <button className="icon" title="Mark as seen"
                      onClick={(e) => { e.stopPropagation(); props.onMarkSeen(node.id); }}>✓</button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
