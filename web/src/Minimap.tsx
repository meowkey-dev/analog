import { useRef } from "react";
import type { Node } from "./api";
import type { Viewport } from "./Canvas";

/**
 * The whole board in a corner, with the visible part outlined. Dragging on it
 * moves the view there — the fast way across a board too large to pan, and on a
 * phone the only way that doesn't cost a dozen swipes. Changed cards are tinted
 * so "where did the agent work" reads at a glance.
 */

const WIDTH = 168;
const HEIGHT = 112;
const PAD = 40;

export function Minimap(props: {
  nodes: Node[];
  viewport: Viewport;
  /** The canvas element's size, px. */
  size: { width: number; height: number };
  changed: ReadonlySet<string>;
  selected: string | null;
  /** Centre the view on this world point. */
  onCenter: (x: number, y: number) => void;
}) {
  const { nodes, viewport, size } = props;
  const box = useRef<SVGSVGElement>(null);

  // What the viewport shows, in world coordinates.
  const view = {
    x: -viewport.x / viewport.scale,
    y: -viewport.y / viewport.scale,
    w: size.width / viewport.scale,
    h: size.height / viewport.scale,
  };
  // The map covers the cards and the view, so the outline never leaves it.
  let minX = view.x, minY = view.y, maxX = view.x + view.w, maxY = view.y + view.h;
  for (const n of nodes) {
    minX = Math.min(minX, n.x);
    minY = Math.min(minY, n.y);
    maxX = Math.max(maxX, n.x + n.width);
    maxY = Math.max(maxY, n.y + n.height);
  }
  minX -= PAD; minY -= PAD; maxX += PAD; maxY += PAD;
  const scale = Math.min(WIDTH / (maxX - minX), HEIGHT / (maxY - minY));
  const offX = (WIDTH - (maxX - minX) * scale) / 2;
  const offY = (HEIGHT - (maxY - minY) * scale) / 2;
  const mx = (x: number) => offX + (x - minX) * scale;
  const my = (y: number) => offY + (y - minY) * scale;

  const centerAt = (clientX: number, clientY: number) => {
    const rect = box.current?.getBoundingClientRect();
    if (!rect) return;
    const x = (clientX - rect.left) * (WIDTH / rect.width);
    const y = (clientY - rect.top) * (HEIGHT / rect.height);
    props.onCenter((x - offX) / scale + minX, (y - offY) / scale + minY);
  };

  const onPointerDown = (event: React.PointerEvent<SVGSVGElement>) => {
    event.preventDefault();
    event.stopPropagation();
    const target = event.currentTarget;
    target.setPointerCapture(event.pointerId);
    centerAt(event.clientX, event.clientY);
    const move = (e: PointerEvent) => centerAt(e.clientX, e.clientY);
    const up = () => {
      target.removeEventListener("pointermove", move);
      target.removeEventListener("pointerup", up);
      target.removeEventListener("pointercancel", up);
    };
    target.addEventListener("pointermove", move);
    target.addEventListener("pointerup", up);
    target.addEventListener("pointercancel", up);
  };

  return (
    <svg ref={box} className="minimap" viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
         role="img" aria-label="Board overview; drag to move the view"
         onPointerDown={onPointerDown}>
      {nodes.map((n) => (
        <rect key={n.id}
              className={`mini-card${props.changed.has(n.id) ? " changed" : ""}${props.selected === n.id ? " selected" : ""}${n.sp_superseded_by ? " superseded" : ""}`}
              x={mx(n.x)} y={my(n.y)}
              width={Math.max(1.5, n.width * scale)} height={Math.max(1.5, n.height * scale)}
              rx={1.5} />
      ))}
      <rect className="mini-view" x={mx(view.x)} y={my(view.y)}
            width={view.w * scale} height={view.h * scale} rx={2} />
    </svg>
  );
}
