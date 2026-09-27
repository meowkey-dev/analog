import type { AnalogEvent, Annotation, Edge, Node } from "./api";

/**
 * The order the reader walks a board in (one card at a time, prev/next).
 *
 * A canvas has no order of its own, so each of these is a guess at what a human
 * wants next: the board as it reads (rows, top to bottom, left to right), what
 * the agents touched last, or what still has open comments.
 */

export type ReadingOrder = "board" | "recent" | "open" | "changes";

export const ORDERS: { id: ReadingOrder; label: string }[] = [
  { id: "board", label: "board order" },
  { id: "recent", label: "recent first" },
  { id: "open", label: "open comments" },
  { id: "changes", label: "what's new" },
];

/**
 * Rows, then columns. A card joins the current row while its top edge sits in
 * the upper half of the row's first card; that tolerates the ragged tops of a
 * hand-arranged board without merging cards that are plainly stacked.
 */
export function boardOrder(nodes: Node[]): Node[] {
  const byTop = [...nodes].sort((a, b) => a.y - b.y || a.x - b.x);
  const rows: Node[][] = [];
  let row: Node[] = [];
  let limit = -Infinity;
  for (const node of byTop) {
    if (row.length > 0 && node.y < limit) {
      row.push(node);
      continue;
    }
    if (row.length > 0) rows.push(row);
    row = [node];
    limit = node.y + Math.max(1, node.height) / 2;
  }
  if (row.length > 0) rows.push(row);
  return rows.flatMap((r) => r.sort((a, b) => a.x - b.x));
}

/** The card an event is about, if any. */
export function eventCard(event: AnalogEvent): string | undefined {
  if (event.type.startsWith("card.")) return event.subject_id;
  if (event.type.startsWith("annotation.")) return event.payload?.card_id;
  return undefined;
}

/** Most recently created, edited or commented first; moves don't count. */
export function recentOrder(nodes: Node[], events: AnalogEvent[]): Node[] {
  const last = new Map<string, number>();
  for (const event of events) {
    if (event.type === "card.moved") continue;
    const id = eventCard(event);
    if (id) last.set(id, Math.max(last.get(id) ?? 0, event.seq));
  }
  const board = boardOrder(nodes);
  const rank = new Map(board.map((n, i) => [n.id, i]));
  return [...nodes].sort((a, b) =>
    (last.get(b.id) ?? 0) - (last.get(a.id) ?? 0) || rank.get(a.id)! - rank.get(b.id)!);
}

/** Cards with open comments, most first; the rest follow in board order. */
export function openOrder(nodes: Node[], annotations: Annotation[]): Node[] {
  const open = new Map<string, number>();
  for (const a of annotations) {
    if (!a.resolved) open.set(a.card_id, (open.get(a.card_id) ?? 0) + 1);
  }
  const board = boardOrder(nodes);
  const rank = new Map(board.map((n, i) => [n.id, i]));
  return [...nodes].sort((a, b) =>
    (open.get(b.id) ?? 0) - (open.get(a.id) ?? 0) || rank.get(a.id)! - rank.get(b.id)!);
}

export function readingOrder(
  order: ReadingOrder,
  nodes: Node[],
  events: AnalogEvent[],
  annotations: Annotation[],
  changed: ReadonlySet<string>,
): Node[] {
  switch (order) {
    case "recent":
      return recentOrder(nodes, events);
    case "open":
      return openOrder(nodes, annotations);
    case "changes":
      return boardOrder(nodes.filter((n) => changed.has(n.id)));
    default:
      return boardOrder(nodes);
  }
}

export interface Neighbor {
  edge: Edge;
  node: Node;
  direction: "out" | "in";
}

/** The cards a link reaches from this one, outgoing first. */
export function neighbors(id: string, edges: Edge[], byId: Map<string, Node>): Neighbor[] {
  const out: Neighbor[] = [];
  const inbound: Neighbor[] = [];
  for (const edge of edges) {
    if (edge.fromNode === id) {
      const node = byId.get(edge.toNode);
      if (node) out.push({ edge, node, direction: "out" });
    } else if (edge.toNode === id) {
      const node = byId.get(edge.fromNode);
      if (node) inbound.push({ edge, node, direction: "in" });
    }
  }
  return [...out, ...inbound];
}

/** Where a card goes so it lands at the end of board order: under everything, at the left. */
export function endOfBoard(nodes: Node[]): { x: number; y: number } {
  if (nodes.length === 0) return { x: 0, y: 0 };
  const x = Math.min(...nodes.map((n) => n.x));
  const y = Math.max(...nodes.map((n) => n.y + n.height)) + 40;
  return { x: Math.round(x), y: Math.round(y) };
}
