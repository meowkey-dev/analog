import { memo } from "react";
import type { Edge, Node, Side } from "./api";

/** One <svg> layer beneath the cards; each edge is a bezier between anchors. */

function anchor(node: Node, side: Side): [number, number] {
  switch (side) {
    case "top": return [node.x + node.width / 2, node.y];
    case "bottom": return [node.x + node.width / 2, node.y + node.height];
    case "left": return [node.x, node.y + node.height / 2];
    case "right": return [node.x + node.width, node.y + node.height / 2];
  }
}

/** When an edge omits fromSide/toSide, pick the pair facing each other. */
export function autoSides(from: Node, to: Node): [Side, Side] {
  const dx = (to.x + to.width / 2) - (from.x + from.width / 2);
  const dy = (to.y + to.height / 2) - (from.y + from.height / 2);
  if (Math.abs(dx) >= Math.abs(dy)) {
    return dx >= 0 ? ["right", "left"] : ["left", "right"];
  }
  return dy >= 0 ? ["bottom", "top"] : ["top", "bottom"];
}

function curve(a: [number, number], b: [number, number], aSide: Side, bSide: Side): string {
  const pull = Math.max(40, Math.hypot(b[0] - a[0], b[1] - a[1]) / 2.5);
  const out = (side: Side): [number, number] =>
    side === "left" ? [-pull, 0] : side === "right" ? [pull, 0] : side === "top" ? [0, -pull] : [0, pull];
  const [ax, ay] = out(aSide);
  const [bx, by] = out(bSide);
  return `M ${a[0]} ${a[1]} C ${a[0] + ax} ${a[1] + ay}, ${b[0] + bx} ${b[1] + by}, ${b[0]} ${b[1]}`;
}

/** Arrowheads carry the edge's color (#3), so one marker per distinct color. */
function markerId(color: string | undefined): string {
  return `arrow-${(color ?? "default").replace(/[^a-zA-Z0-9]/g, "-")}`;
}

export interface LinksProps {
  edges: Edge[];
  nodes: Map<string, Node>;
  bounds: { minX: number; minY: number; width: number; height: number };
  selectedId: string | null;
  onSelect: (id: string) => void;
  onDelete: (id: string) => void;
  draft?: { from: Node; to: [number, number] } | null;
}

function LinksView({ edges, nodes, bounds, selectedId, onSelect, onDelete, draft }: LinksProps) {
  return (
    <svg
      className="links"
      style={{ left: bounds.minX, top: bounds.minY, width: bounds.width, height: bounds.height }}
      viewBox={`${bounds.minX} ${bounds.minY} ${bounds.width} ${bounds.height}`}
    >
      <defs>
        {Array.from(new Set(edges.map((e) => e.color ?? "default"))).map((color) => (
          <marker key={color} id={markerId(color === "default" ? undefined : color)}
                  viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7"
                  markerHeight="7" orient="auto-start-reverse">
            <path d="M 0 0 L 10 5 L 0 10 z"
                  style={color === "default" ? undefined : { fill: color }} />
          </marker>
        ))}
      </defs>

      {edges.map((edge) => {
        const from = nodes.get(edge.fromNode);
        const to = nodes.get(edge.toNode);
        if (!from || !to) return null;
        // A soft-deleted endpoint keeps its last position, so the edge still
        // renders there — dashed, unattached-looking, and deletable (#15).
        const dangling = Boolean(from.sp_deleted_at || to.sp_deleted_at);
        const [autoFrom, autoTo] = autoSides(from, to);
        const a = anchor(from, edge.fromSide ?? autoFrom);
        const b = anchor(to, edge.toSide ?? autoTo);
        const d = curve(a, b, edge.fromSide ?? autoFrom, edge.toSide ?? autoTo);
        const mid: [number, number] = [(a[0] + b[0]) / 2, (a[1] + b[1]) / 2];
        const selected = selectedId === edge.id;
        return (
          <g key={edge.id} className={`edge${dangling ? " dangling" : ""}${selected ? " selected" : ""}`}
             style={edge.color ? { "--edge-color": edge.color } as React.CSSProperties : undefined}>
            <path className="edge-hit" d={d}
                  onPointerDown={(e) => { e.stopPropagation(); onSelect(edge.id); }} />
            <path className="edge-line" markerEnd={dangling ? undefined : `url(#${markerId(edge.color)})`} d={d} />
            {edge.label && (
              <g transform={`translate(${mid[0]}, ${mid[1]})`}>
                <text className="edge-label" textAnchor="middle" dy="0.32em">{edge.label}</text>
              </g>
            )}
            {selected && (
              <g transform={`translate(${mid[0]}, ${mid[1] - 18})`}
                 onPointerDown={(e) => { e.stopPropagation(); onDelete(edge.id); }}>
                <circle className="edge-delete" r="9" />
                <text className="edge-delete-x" textAnchor="middle" dy="0.32em">×</text>
              </g>
            )}
          </g>
        );
      })}

      {draft && (() => {
        const [side] = autoSides(draft.from, {
          ...draft.from, x: draft.to[0], y: draft.to[1], width: 0, height: 0,
        });
        const a = anchor(draft.from, side);
        return <path className="edge-draft" d={`M ${a[0]} ${a[1]} L ${draft.to[0]} ${draft.to[1]}`} />;
      })()}
    </svg>
  );
}

export const Links = memo(LinksView);
