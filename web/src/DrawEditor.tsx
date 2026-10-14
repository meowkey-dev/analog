import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import DOMPurify from "dompurify";
import {
  DRAW_COLORS,
  DRAW_WIDTHS,
  clientToViewBox,
  extendStroke,
  extractStrokes,
  hitStroke,
  inkOutline,
  parseViewBox,
  serializeDrawing,
  stripStrokes,
  type InkPoint,
  type Point,
  type Stroke,
} from "./draw";

/**
 * in-card pen for svg nodes (#61). commits the whole svg as card text; escape
 * discards. Cmd+Z while mounted undoes a stroke rather than the analog edit,
 * because a mis-stroke is the thing you want to take back.
 */

export type DrawTool = "pen" | "eraser";

/** the stroke under the pen. `pressure` is whether the device reports a real one. */
interface LiveStroke {
  points: InkPoint[];
  pressure: boolean;
}

/** a mouse reports a flat 0.5 and a finger often 0; only a stylus is trusted. */
function sample(event: PointerEvent, at: Point, pressure: boolean): InkPoint {
  return [at[0], at[1], pressure && event.pressure > 0 ? event.pressure : 0.5];
}

function finish(live: LiveStroke, color: string, width: number): Stroke {
  return { d: inkOutline(live.points, width, { pressure: live.pressure, last: true }), color, width, ink: true };
}

export function DrawEditor(props: {
  text: string;
  onCommit: (text: string) => void;
  onCancel: () => void;
}) {
  const vb = useMemo(() => parseViewBox(props.text), [props.text]);
  const backdrop = useMemo(() => {
    // innerHTML of an <svg><g> fragment is parsed as HTML, not SVG, so agent
    // charts would neither paint nor pass pointer events. a whole <svg> inside
    // a div is HTML the parser understands; the overlay on top owns the pen.
    const stripped = stripStrokes(props.text);
    if (!/<svg[\s>]/i.test(stripped)) return "";
    return DOMPurify.sanitize(stripped, { USE_PROFILES: { svg: true, svgFilters: true }, ADD_ATTR: ["data-analog-stroke"] });
  }, [props.text]);
  const [strokes, setStrokes] = useState<Stroke[]>(() => extractStrokes(props.text));
  const [tool, setTool] = useState<DrawTool>("pen");
  const [color, setColor] = useState<string>(DRAW_COLORS[0] ?? "#dfe3ec");
  const [width, setWidth] = useState<number>(DRAW_WIDTHS[0] ?? 2);
  const [live, setLive] = useState<LiveStroke | null>(null);

  const svgRef = useRef<SVGSVGElement>(null);
  const strokesRef = useRef(strokes);
  strokesRef.current = strokes;
  const liveRef = useRef(live);
  liveRef.current = live;
  const toolRef = useRef(tool);
  toolRef.current = tool;
  const colorRef = useRef(color);
  colorRef.current = color;
  const widthRef = useRef(width);
  widthRef.current = width;

  const toLocal = useCallback((clientX: number, clientY: number): Point => {
    const el = svgRef.current;
    if (!el) return [vb.x, vb.y];
    return clientToViewBox(clientX, clientY, el.getBoundingClientRect(), vb);
  }, [vb]);

  const snapshot = useCallback(
    (next: Stroke[]) => serializeDrawing(props.text, next, vb),
    [props.text, vb],
  );

  const commitNow = useCallback(() => {
    let next = strokesRef.current;
    const pending = liveRef.current;
    if (pending && pending.points.length > 0) {
      next = [...next, finish(pending, colorRef.current, widthRef.current)];
    }
    props.onCommit(snapshot(next));
  }, [props, snapshot]);

  useEffect(() => {
    const down = (event: PointerEvent) => {
      const target = event.target as HTMLElement | null;
      if (target?.closest(".draw-editor")) return;
      commitNow();
    };
    window.addEventListener("pointerdown", down);
    return () => window.removeEventListener("pointerdown", down);
  }, [commitNow]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        props.onCancel();
        return;
      }
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "z") {
        event.preventDefault();
        event.stopPropagation();
        setLive(null);
        setStrokes((s) => s.slice(0, -1));
        return;
      }
      if (event.key === "Backspace" || event.key === "Delete") {
        // the board's delete-card binding must not fire while the pen is down.
        event.preventDefault();
        event.stopPropagation();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [props]);

  useEffect(() => {
    const move = (event: PointerEvent) => {
      const current = liveRef.current;
      if (!current) return;
      // a stylus samples faster than frames; the coalesced events are the
      // samples between them, and without them fast curves turn into polygons.
      const samples = event.getCoalescedEvents?.() ?? [];
      let points = current.points;
      for (const e of samples.length > 0 ? samples : [event]) {
        points = extendStroke(points, sample(e, toLocal(e.clientX, e.clientY), current.pressure));
      }
      if (points === current.points) return;
      const next = { ...current, points };
      liveRef.current = next;
      setLive(next);
    };
    const up = () => {
      const pending = liveRef.current;
      if (!pending) return;
      liveRef.current = null;
      setLive(null);
      if (pending.points.length === 0) return;
      setStrokes((s) => [...s, finish(pending, colorRef.current, widthRef.current)]);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
  }, [toLocal]);

  const onPointerDown = (event: React.PointerEvent<SVGSVGElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    const [x, y] = toLocal(event.clientX, event.clientY);
    if (toolRef.current === "eraser") {
      const hit = hitStroke(strokesRef.current, x, y);
      if (hit >= 0) setStrokes((s) => s.filter((_, i) => i !== hit));
      return;
    }
    const pressure = event.pointerType === "pen";
    const start: LiveStroke = { points: [sample(event.nativeEvent, [x, y], pressure)], pressure };
    liveRef.current = start;
    setLive(start);
  };

  return (
    <div className="draw-editor" onPointerDown={(e) => e.stopPropagation()}>
      <div className="draw-tools" onPointerDown={(e) => e.stopPropagation()}>
        <button type="button" className={tool === "pen" ? "on" : ""} title="Pen"
                onClick={() => setTool("pen")}>pen</button>
        <button type="button" className={tool === "eraser" ? "on" : ""} title="Erase a stroke"
                onClick={() => setTool("eraser")}>erase</button>
        <span className="draw-swatches" role="radiogroup" aria-label="Ink color">
          {DRAW_COLORS.map((c) => (
            <button key={c} type="button"
                    className={`swatch${color === c ? " on" : ""}`}
                    style={{ background: c }}
                    title={c}
                    onClick={() => { setColor(c); setTool("pen"); }} />
          ))}
        </span>
        <span className="draw-widths" role="radiogroup" aria-label="Stroke width">
          {DRAW_WIDTHS.map((w) => (
            <button key={w} type="button" className={width === w ? "on" : ""}
                    title={`${w}px`}
                    onClick={() => setWidth(w)}>{w}</button>
          ))}
        </span>
        <button type="button" className="ghost" disabled={strokes.length === 0}
                title="Undo last stroke (⌘Z)"
                onClick={() => setStrokes((s) => s.slice(0, -1))}>undo</button>
        <span className="spacer" />
        <button type="button" className="ghost" onClick={props.onCancel}>cancel</button>
        <button type="button" onClick={commitNow}>done</button>
      </div>
      <div className="draw-stack">
        {backdrop && (
          <div className="draw-backdrop" dangerouslySetInnerHTML={{ __html: backdrop }} />
        )}
        <svg
          ref={svgRef}
          className={`draw-surface tool-${tool}`}
          viewBox={`${vb.x} ${vb.y} ${vb.w} ${vb.h}`}
          onPointerDown={onPointerDown}
        >
          {strokes.map((stroke, i) => stroke.ink ? (
            <path key={i} fill={stroke.color} stroke="none" d={stroke.d} />
          ) : (
            <path key={i} fill="none" stroke={stroke.color} strokeWidth={stroke.width}
                  strokeLinecap="round" strokeLinejoin="round" d={stroke.d} />
          ))}
          {live && live.points.length > 0 && (
            <path fill={color} stroke="none"
                  d={inkOutline(live.points, width, { pressure: live.pressure, last: false })} />
          )}
        </svg>
      </div>
    </div>
  );
}
