import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Card } from "./Card";
import { Links } from "./Links";
import { AnnotationOverlay, type DraftAnnotation } from "./Annotations";
import type { Annotation, Edge, Node } from "./api";

/** Pan/zoom via a CSS transform; cards absolutely positioned; raw pointer events. */

export interface Viewport {
  x: number;
  y: number;
  scale: number;
}

const MIN_SCALE = 0.15;
const MAX_SCALE = 3;
const MIN_W = 140;
const MIN_H = 90;

interface DragState {
  kind: "card" | "resize" | "pan" | "link";
  id?: string;
  startPointer: [number, number];
  startValue: [number, number];
  node?: Node;
}

export interface CanvasProps {
  nodes: Node[];
  /** Every node including soft-deleted ones, so edges into deleted cards can render (#15). */
  allNodes: Node[];
  edges: Edge[];
  annotations: Annotation[];
  annotateMode: boolean;
  draft: DraftAnnotation | null;
  selectedCard: string | null;
  selectedEdge: string | null;
  selectedAnnotation: string | null;
  focus: { id: string; nonce: number } | null;
  onSelectCard: (id: string | null) => void;
  onSelectEdge: (id: string | null) => void;
  onSelectAnnotation: (id: string | null) => void;
  onDraft: (draft: DraftAnnotation | null) => void;
  onMoveCard: (id: string, x: number, y: number) => void;
  onResizeCard: (id: string, width: number, height: number) => void;
  onEditCard: (id: string, text: string) => void;
  onDeleteCard: (id: string) => void;
  onCreateLink: (from: string, to: string, label: string) => void;
  onDeleteLink: (id: string) => void;
  onPopOut: (node: Node) => void;
  onCreateCardAt: (x: number, y: number) => void;
}

export function Canvas(props: CanvasProps) {
  const container = useRef<HTMLDivElement>(null);
  const [viewport, setViewport] = useState<Viewport>({ x: 80, y: 80, scale: 1 });
  const [drag, setDrag] = useState<DragState | null>(null);
  const [ghost, setGhost] = useState<Record<string, Partial<Node>>>({});
  const [linkTo, setLinkTo] = useState<[number, number] | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  const [pendingLink, setPendingLink] = useState<{ from: string; to: string } | null>(null);
  const [linkLabel, setLinkLabel] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const nodes = useMemo(
    () => props.nodes.map((node) => ({ ...node, ...(ghost[node.id] ?? {}) })),
    [props.nodes, ghost],
  );
  const nodeMap = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);
  const linkNodes = useMemo(
    () => new Map(props.allNodes.map((n) => [n.id, n])),
    [props.allNodes],
  );

  const openByCard = useMemo(() => {
    const counts = new Map<string, number>();
    for (const a of props.annotations) {
      if (!a.resolved) counts.set(a.card_id, (counts.get(a.card_id) ?? 0) + 1);
    }
    return counts;
  }, [props.annotations]);

  const revisionCount = useMemo(() => {
    // How many cards a superseded card has been revised into, following the chain.
    const counts = new Map<string, number>();
    for (const node of props.nodes) {
      let depth = 1;
      let current: Node | undefined = node;
      const seen = new Set<string>();
      while (current?.sp_superseded_by && !seen.has(current.id)) {
        seen.add(current.id);
        current = props.nodes.find((n) => n.id === current!.sp_superseded_by);
        depth += 1;
      }
      counts.set(node.id, depth);
    }
    return counts;
  }, [props.nodes]);

  // Superseded cards start collapsed (SPEC §2.4).
  useEffect(() => {
    setCollapsed((previous) => {
      const next = { ...previous };
      let changed = false;
      for (const node of props.nodes) {
        if (node.sp_superseded_by && next[node.id] === undefined) {
          next[node.id] = true;
          changed = true;
        }
      }
      return changed ? next : previous;
    });
  }, [props.nodes]);

  const toWorld = useCallback(
    (clientX: number, clientY: number): [number, number] => {
      const element = container.current!;
      const box = element.getBoundingClientRect();
      // overflow: clip keeps the canvas from ever scrolling (see styles.css), so
      // client coords map onto the transform directly.
      return [
        (clientX - box.left - viewport.x) / viewport.scale,
        (clientY - box.top - viewport.y) / viewport.scale,
      ];
    },
    [viewport],
  );

  const bounds = useMemo(() => {
    if (nodes.length === 0) return { minX: -500, minY: -500, width: 2000, height: 1500 };
    const pad = 600;
    const minX = Math.min(...nodes.map((n) => n.x)) - pad;
    const minY = Math.min(...nodes.map((n) => n.y)) - pad;
    const maxX = Math.max(...nodes.map((n) => n.x + n.width)) + pad;
    const maxY = Math.max(...nodes.map((n) => n.y + n.height)) + pad;
    return { minX, minY, width: maxX - minX, height: maxY - minY };
  }, [nodes]);

  // Clicking an activity row pans the canvas to its subject.
  useEffect(() => {
    if (!props.focus || !container.current) return;
    const node = nodeMap.get(props.focus.id);
    if (!node) return;
    const box = container.current.getBoundingClientRect();
    setViewport((v) => ({
      ...v,
      x: box.width / 2 - (node.x + node.width / 2) * v.scale,
      y: box.height / 2 - (node.y + node.height / 2) * v.scale,
    }));
    props.onSelectCard(node.id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.focus?.nonce]);

  // --- pointer handling ------------------------------------------------------

  const startPan = (event: React.PointerEvent) => {
    if (event.button !== 0 && event.button !== 1) return;
    props.onSelectCard(null);
    props.onSelectEdge(null);
    props.onSelectAnnotation(null);
    setDrag({
      kind: "pan",
      startPointer: [event.clientX, event.clientY],
      startValue: [viewport.x, viewport.y],
    });
  };

  const startCardDrag = (event: React.PointerEvent, node: Node) => {
    if (event.button !== 0 || props.annotateMode) return;
    event.stopPropagation();
    setDrag({
      kind: "card", id: node.id, node,
      startPointer: [event.clientX, event.clientY],
      startValue: [node.x, node.y],
    });
  };

  const startResize = (event: React.PointerEvent, node: Node) => {
    event.stopPropagation();
    setDrag({
      kind: "resize", id: node.id, node,
      startPointer: [event.clientX, event.clientY],
      startValue: [node.width, node.height],
    });
  };

  const startLink = (event: React.PointerEvent, node: Node) => {
    // Canceling the pointerdown stops the drag from starting a text selection (#18).
    event.preventDefault();
    event.stopPropagation();
    setLinkTo(toWorld(event.clientX, event.clientY));
    setDrag({
      kind: "link", id: node.id, node,
      startPointer: [event.clientX, event.clientY],
      startValue: [node.x, node.y],
    });
  };

  useEffect(() => {
    if (!drag) return;

    const move = (event: PointerEvent) => {
      const dx = event.clientX - drag.startPointer[0];
      const dy = event.clientY - drag.startPointer[1];
      if (drag.kind === "pan") {
        setViewport((v) => ({ ...v, x: drag.startValue[0] + dx, y: drag.startValue[1] + dy }));
      } else if (drag.kind === "card") {
        setGhost((g) => ({
          ...g,
          [drag.id!]: {
            x: Math.round(drag.startValue[0] + dx / viewport.scale),
            y: Math.round(drag.startValue[1] + dy / viewport.scale),
          },
        }));
      } else if (drag.kind === "resize") {
        setGhost((g) => ({
          ...g,
          [drag.id!]: {
            width: Math.max(MIN_W, Math.round(drag.startValue[0] + dx / viewport.scale)),
            height: Math.max(MIN_H, Math.round(drag.startValue[1] + dy / viewport.scale)),
          },
        }));
      } else if (drag.kind === "link") {
        setLinkTo(toWorld(event.clientX, event.clientY));
      }
    };

    const up = (event: PointerEvent) => {
      if (drag.kind === "card") {
        const moved = ghost[drag.id!];
        if (moved && (moved.x !== drag.startValue[0] || moved.y !== drag.startValue[1])) {
          props.onMoveCard(drag.id!, moved.x!, moved.y!);
        }
        setGhost((g) => {
          const { [drag.id!]: _, ...rest } = g;
          return rest;
        });
      } else if (drag.kind === "resize") {
        const sized = ghost[drag.id!];
        if (sized) props.onResizeCard(drag.id!, sized.width!, sized.height!);
        setGhost((g) => {
          const { [drag.id!]: _, ...rest } = g;
          return rest;
        });
      } else if (drag.kind === "link") {
        const element = document.elementFromPoint(event.clientX, event.clientY);
        const target = element?.closest<HTMLElement>("[data-card-id]")?.dataset.cardId;
        if (target && target !== drag.id) {
          // SPEC §4.3: always label. An unlabelled edge is noise, so the edge is
          // not created until a label exists.
          setLinkLabel("");
          setPendingLink({ from: drag.id!, to: target });
        }
        setLinkTo(null);
      }
      setDrag(null);
    };

    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
  }, [drag, ghost, viewport.scale, toWorld, props]);

  /**
   * Whether some element between `start` and the canvas can absorb this wheel
   * delta itself. A card body with overflow:auto scrolls natively AND bubbles
   * the wheel up here, so without this the board pans at the same time. A
   * scrollable card owns the wheel outright — even scrolled to its end the
   * delta must not spill into a board pan (#8); pan by dragging instead.
   */
  const consumedByScrollable = (start: Element | null, dx: number, dy: number): boolean => {
    for (let el = start; el && el !== container.current; el = el.parentElement) {
      if (!(el instanceof HTMLElement)) continue;
      const style = getComputedStyle(el);
      const scrollableY = /auto|scroll/.test(style.overflowY) && el.scrollHeight > el.clientHeight;
      const scrollableX = /auto|scroll/.test(style.overflowX) && el.scrollWidth > el.clientWidth;
      if ((scrollableY && dy !== 0) || (scrollableX && dx !== 0)) return true;
    }
    return false;
  };

  const onWheel = (event: React.WheelEvent) => {
    if (event.ctrlKey || event.metaKey) {
      const box = container.current!.getBoundingClientRect();
      const px = event.clientX - box.left;
      const py = event.clientY - box.top;
      setViewport((v) => {
        const scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, v.scale * Math.exp(-event.deltaY / 300)));
        const ratio = scale / v.scale;
        return { scale, x: px - (px - v.x) * ratio, y: py - (py - v.y) * ratio };
      });
    } else {
      if (consumedByScrollable(event.target as Element, event.deltaX, event.deltaY)) return;
      setViewport((v) => ({ ...v, x: v.x - event.deltaX, y: v.y - event.deltaY }));
    }
  };

  const zoomBy = (factor: number) =>
    setViewport((v) => {
      const box = container.current!.getBoundingClientRect();
      const px = box.width / 2;
      const py = box.height / 2;
      const scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, v.scale * factor));
      const ratio = scale / v.scale;
      return { scale, x: px - (px - v.x) * ratio, y: py - (py - v.y) * ratio };
    });

  const fit = () => {
    if (!container.current || nodes.length === 0) return;
    const box = container.current.getBoundingClientRect();
    const minX = Math.min(...nodes.map((n) => n.x));
    const minY = Math.min(...nodes.map((n) => n.y));
    const maxX = Math.max(...nodes.map((n) => n.x + n.width));
    const maxY = Math.max(...nodes.map((n) => n.y + n.height));
    const scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE,
      Math.min((box.width - 120) / (maxX - minX), (box.height - 120) / (maxY - minY))));
    setViewport({
      scale,
      x: box.width / 2 - ((minX + maxX) / 2) * scale,
      y: box.height / 2 - ((minY + maxY) / 2) * scale,
    });
  };

  return (
    <div
      ref={container}
      className={`canvas${props.annotateMode ? " annotating" : ""}${drag?.kind === "pan" ? " panning" : ""}${drag ? " dragging" : ""}`}
      onPointerDown={startPan}
      onWheel={onWheel}
      onDoubleClick={(event) => {
        if (event.target !== event.currentTarget) return;
        const [x, y] = toWorld(event.clientX, event.clientY);
        props.onCreateCardAt(Math.round(x), Math.round(y));
      }}
    >
      <div className="viewport"
           style={{ transform: `translate(${viewport.x}px, ${viewport.y}px) scale(${viewport.scale})` }}>
        <Links
          edges={props.edges}
          nodes={linkNodes}
          bounds={bounds}
          selectedId={props.selectedEdge}
          onSelect={props.onSelectEdge}
          onDelete={props.onDeleteLink}
          draft={drag?.kind === "link" && linkTo ? { from: drag.node!, to: linkTo } : null}
        />
        {nodes.map((node) => (
          <Card
            key={node.id}
            node={node}
            selected={props.selectedCard === node.id}
            editing={editing === node.id}
            openCount={openByCard.get(node.id) ?? 0}
            revisions={revisionCount.get(node.id) ?? 1}
            collapsed={collapsed[node.id] ?? false}
            onToggleCollapsed={(id) => setCollapsed((c) => ({ ...c, [id]: !c[id] }))}
            onPointerDownHeader={startCardDrag}
            onPointerDownResize={startResize}
            onPointerDownLink={startLink}
            onSelect={props.onSelectCard}
            onStartEdit={setEditing}
            onCommitEdit={(id, text) => {
              setEditing(null);
              if (text !== (nodeMap.get(id)?.text ?? "")) props.onEditCard(id, text);
            }}
            onCancelEdit={() => setEditing(null)}
            onDelete={props.onDeleteCard}
            onPopOut={props.onPopOut}
            overlay={
              <AnnotationOverlay
                node={node}
                annotations={props.annotations.filter((a) => a.card_id === node.id && !a.resolved)}
                active={props.annotateMode}
                draft={props.draft}
                selectedId={props.selectedAnnotation}
                onSelect={props.onSelectAnnotation}
                onDraft={props.onDraft}
              />
            }
          />
        ))}
      </div>

      {pendingLink && (
        <form
          className="composer link-composer"
          onSubmit={(event) => {
            event.preventDefault();
            if (!linkLabel.trim()) return;
            props.onCreateLink(pendingLink.from, pendingLink.to, linkLabel.trim());
            setPendingLink(null);
          }}
          onPointerDown={(event) => event.stopPropagation()}
        >
          <div className="composer-head">
            Link <strong>{nodeMap.get(pendingLink.from)?.sp_title ?? pendingLink.from}</strong>
            {" → "}
            <strong>{nodeMap.get(pendingLink.to)?.sp_title ?? pendingLink.to}</strong>
          </div>
          <input
            autoFocus
            value={linkLabel}
            placeholder="How do they relate? e.g. depends on, contradicts"
            onChange={(event) => setLinkLabel(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape") setPendingLink(null);
              if (event.key === "Enter" && linkLabel.trim()) {
                event.preventDefault();
                props.onCreateLink(pendingLink.from, pendingLink.to, linkLabel.trim());
                setPendingLink(null);
              }
            }}
          />
          <div className="composer-foot">
            <span className="hint">Unlabelled edges are noise, so a label is required.</span>
            <button type="button" className="ghost" onClick={() => setPendingLink(null)}>Cancel</button>
            <button type="submit" disabled={!linkLabel.trim()}>Link</button>
          </div>
        </form>
      )}

      <div className="zoom">
        <button onClick={() => zoomBy(1.2)} title="Zoom in">+</button>
        <button onClick={() => zoomBy(1 / 1.2)} title="Zoom out">−</button>
        <button onClick={fit} title="Fit to content">⤢</button>
        <span>{Math.round(viewport.scale * 100)}%</span>
      </div>
    </div>
  );
}
