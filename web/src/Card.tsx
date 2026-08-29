import { memo, useMemo } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import DOMPurify from "dompurify";
import type { Node } from "./api";

/**
 * Renders one card by sp_kind (SPEC §5).
 *
 *   plain -> <pre>            md -> react-markdown
 *   svg   -> inlined, sanitized
 *   html  -> <iframe sandbox="allow-scripts"> with NO allow-same-origin, so agent
 *            HTML can neither read the parent document nor forge an annotation.
 *   file  -> <img>, because binary content is a JSON Canvas file node (§2.1).
 */
function Body({ node }: { node: Node }) {
  const kind = node.type === "file" ? "file" : (node.sp_kind ?? "plain");

  const svg = useMemo(
    () => (kind === "svg" ? DOMPurify.sanitize(node.text ?? "", { USE_PROFILES: { svg: true, svgFilters: true } }) : ""),
    [kind, node.text],
  );

  switch (kind) {
    case "md":
      return (
        <div className="card-body md">
          <Markdown remarkPlugins={[remarkGfm]}>{node.text ?? ""}</Markdown>
        </div>
      );
    case "svg":
      return <div className="card-body svg" dangerouslySetInnerHTML={{ __html: svg }} />;
    case "html":
      return (
        <iframe
          className="card-body html"
          sandbox="allow-scripts"
          srcDoc={node.text ?? ""}
          title={node.sp_title ?? node.id}
        />
      );
    case "file": {
      const isImage = !/\.pdf$/i.test(node.file ?? "");
      return isImage ? (
        <div className="card-body file">
          <img src={node.file} alt={node.sp_title ?? ""} draggable={false} />
        </div>
      ) : (
        <div className="card-body file">
          <a href={node.file} target="_blank" rel="noreferrer">{node.file}</a>
        </div>
      );
    }
    default:
      return <pre className="card-body plain">{node.text ?? ""}</pre>;
  }
}

export interface CardProps {
  node: Node;
  selected: boolean;
  editing: boolean;
  openCount: number;
  revisions: number;
  collapsed: boolean;
  onToggleCollapsed: (id: string) => void;
  onPointerDownHeader: (event: React.PointerEvent, node: Node) => void;
  onPointerDownResize: (event: React.PointerEvent, node: Node) => void;
  onPointerDownLink: (event: React.PointerEvent, node: Node) => void;
  onSelect: (id: string) => void;
  onStartEdit: (id: string) => void;
  onCommitEdit: (id: string, text: string) => void;
  onCancelEdit: () => void;
  onDelete: (id: string) => void;
  onPopOut: (node: Node) => void;
  overlay?: React.ReactNode;
}

function CardView(props: CardProps) {
  const { node, selected, editing, collapsed, revisions, openCount } = props;
  const superseded = Boolean(node.sp_superseded_by);
  const kind = node.type === "file" ? "file" : (node.sp_kind ?? "plain");

  const style: React.CSSProperties = {
    left: node.x,
    top: node.y,
    width: node.width,
    height: superseded && collapsed ? undefined : node.height,
  };

  // A superseded card collapses to a stub so a long chain doesn't swamp the canvas.
  if (superseded && collapsed) {
    return (
      <div className={`card stub${selected ? " selected" : ""}`} style={style}
           data-card-id={node.id} onPointerDown={() => props.onSelect(node.id)}>
        <div className="card-head" onPointerDown={(e) => props.onPointerDownHeader(e, node)}>
          <span className="card-title">{node.sp_title || node.id}</span>
          <span className="badge">rev {revisions}</span>
          <button className="icon" title="Show the superseded content"
                  onClick={() => props.onToggleCollapsed(node.id)}>▾</button>
        </div>
      </div>
    );
  }

  return (
    <div
      className={`card${selected ? " selected" : ""}${superseded ? " superseded" : ""}`}
      style={style}
      data-card-id={node.id}
      onPointerDown={() => props.onSelect(node.id)}
      onDoubleClick={(e) => {
        if (node.type === "text" && !superseded) {
          e.stopPropagation();
          props.onStartEdit(node.id);
        }
      }}
    >
      <div className="card-head" onPointerDown={(e) => props.onPointerDownHeader(e, node)}>
        <span className="card-title">{node.sp_title || node.id}</span>
        <span className="card-kind">{kind}</span>
        {openCount > 0 && <span className="badge comments" title={`${openCount} open comment(s)`}>{openCount}</span>}
        {superseded && (
          <button className="icon" title="Collapse" onClick={() => props.onToggleCollapsed(node.id)}>▴</button>
        )}
        {kind === "html" && (
          <button className="icon" title="Open full window" onClick={() => props.onPopOut(node)}>⤢</button>
        )}
        <button className="icon danger" title="Delete card" onClick={() => props.onDelete(node.id)}>×</button>
      </div>

      {superseded && <div className="superseded-note">superseded — read only</div>}

      {editing ? (
        <textarea
          className="card-body editor"
          defaultValue={node.text ?? ""}
          autoFocus
          onPointerDown={(e) => e.stopPropagation()}
          onBlur={(e) => props.onCommitEdit(node.id, e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") props.onCancelEdit();
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              props.onCommitEdit(node.id, e.currentTarget.value);
            }
          }}
        />
      ) : (
        <Body node={node} />
      )}

      {props.overlay}

      {!superseded && (
        <>
          <div className="handle resize" title="Resize"
               onPointerDown={(e) => props.onPointerDownResize(e, node)} />
          <div className="handle link" title="Drag to another card to link"
               onPointerDown={(e) => props.onPointerDownLink(e, node)} />
        </>
      )}
      <div className="card-foot">
        <span>{node.sp_created_by}</span>
        <span>rev {node.sp_rev ?? 1}</span>
      </div>
    </div>
  );
}

export const Card = memo(CardView);
