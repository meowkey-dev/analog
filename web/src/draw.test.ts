import { describe, expect, it } from "vitest";
import {
  clientToViewBox,
  emptyDrawing,
  extendStroke,
  extractStrokes,
  hitStroke,
  inkOutline,
  parseViewBox,
  pointsToPath,
  serializeDrawing,
  svgBackground,
  type InkPoint,
  type Stroke,
} from "./draw";

const chart =
  '<svg viewBox="0 0 200 120" xmlns="http://www.w3.org/2000/svg"><rect x="10" y="30" width="30" height="70" fill="#4a7"/><line x1="5" y1="100" x2="195" y2="100" stroke="#333"/></svg>';

const ink: Stroke = { d: "M10 10L20 20", color: "#e06c75", width: 2 };

describe("emptyDrawing", () => {
  it("is a viewBox-sized svg with no strokes", () => {
    const svg = emptyDrawing(480, 300);
    expect(svg).toContain('viewBox="0 0 480 300"');
    expect(extractStrokes(svg)).toEqual([]);
  });
});

describe("parseViewBox", () => {
  it("reads viewBox when present", () => {
    expect(parseViewBox(chart)).toEqual({ x: 0, y: 0, w: 200, h: 120 });
  });

  it("falls back to width/height, then the sketch default", () => {
    expect(parseViewBox('<svg width="40" height="20"></svg>')).toEqual({ x: 0, y: 0, w: 40, h: 20 });
    expect(parseViewBox("not svg")).toEqual({ x: 0, y: 0, w: 480, h: 300 });
  });
});

describe("serializeDrawing", () => {
  it("appends tagged strokes and leaves agent markup in place", () => {
    const next = serializeDrawing(chart, [ink]);
    expect(next).toContain('<rect x="10" y="30"');
    expect(extractStrokes(next)).toEqual([ink]);
    expect(svgBackground(next)).toContain("<rect");
    expect(svgBackground(next)).not.toContain("data-analog-stroke");
  });

  it("replaces previous analog strokes rather than stacking them", () => {
    const once = serializeDrawing(chart, [ink]);
    const twice = serializeDrawing(once, [{ ...ink, d: "M1 1L2 2" }]);
    expect(extractStrokes(twice)).toHaveLength(1);
    expect(extractStrokes(twice)[0]?.d).toBe("M1 1L2 2");
    expect(twice.match(/<rect/g)).toHaveLength(1);
  });

  it("turns non-svg text into a blank drawing", () => {
    const svg = serializeDrawing("", [ink]);
    expect(svg.startsWith("<svg")).toBe(true);
    expect(extractStrokes(svg)).toEqual([ink]);
  });
});

describe("pointsToPath", () => {
  it("degenerates a click to a short segment so round caps still paint", () => {
    expect(pointsToPath([[3, 4]])).toBe("M3 4l.01 0");
  });

  it("joins later points with L", () => {
    expect(pointsToPath([[0, 0], [10, 0], [10, 10]])).toBe("M0 0L10 0L10 10");
  });
});

describe("extendStroke", () => {
  it("keeps the first point and drops jitter under the threshold", () => {
    const start = extendStroke([], [0, 0]);
    expect(extendStroke(start, [0.4, 0.3])).toEqual(start);
    expect(extendStroke(start, [2, 0])).toEqual([[0, 0], [2, 0]]);
  });
});

describe("hitStroke", () => {
  it("hits a segment within the stroke's slop and misses far away", () => {
    const strokes: Stroke[] = [
      { d: "M0 0L10 0", color: "#fff", width: 2 },
      { d: "M0 20L10 20", color: "#fff", width: 2 },
    ];
    expect(hitStroke(strokes, 5, 1)).toBe(0);
    expect(hitStroke(strokes, 5, 20)).toBe(1);
    expect(hitStroke(strokes, 5, 50)).toBe(-1);
  });
});

describe("clientToViewBox", () => {
  it("maps the letterboxed centre onto the viewBox centre", () => {
    // 400×200 box, 200×200 viewBox → 200px drawing, 100px gutters left/right.
    const rect = { left: 0, top: 0, width: 400, height: 200 };
    const vb = { x: 0, y: 0, w: 200, h: 200 };
    expect(clientToViewBox(200, 100, rect, vb)).toEqual([100, 100]);
    expect(clientToViewBox(100, 0, rect, vb)).toEqual([0, 0]);
  });
});

describe("ink strokes", () => {
  const line: InkPoint[] = [[0, 10, 0.5], [10, 10, 0.5], [20, 10, 0.5], [30, 10, 0.5], [40, 10, 0.5]];

  it("outlines a pen stroke as a closed M/L polygon", () => {
    const d = inkOutline(line, 4, { pressure: false, last: true });
    expect(d).toMatch(/^M[\d.-]+ [\d.-]+(L[\d.-]+ [\d.-]+)+Z$/);
    expect(inkOutline([], 4, { pressure: false, last: true })).toBe("");
  });

  it("paints a lone tap as a dot rather than nothing", () => {
    expect(inkOutline([[5, 5, 0.5]], 4, { pressure: false, last: true })).not.toBe("");
  });

  it("thickens with stylus pressure", () => {
    const span = (p: number) => {
      const d = inkOutline(line.map(([x, y]) => [x, y, p] as InkPoint), 8, { pressure: true, last: true });
      const ys = Array.from(d.matchAll(/[ML]([\d.-]+) ([\d.-]+)/g), (m) => Number(m[2]));
      return Math.max(...ys) - Math.min(...ys);
    };
    expect(span(1)).toBeGreaterThan(span(0.1));
  });

  it("round-trips as a filled path with its nominal width", () => {
    const stroke: Stroke = { d: inkOutline(line, 4, { pressure: false, last: true }), color: "#6ea8fe", width: 4, ink: true };
    const svg = serializeDrawing(chart, [stroke, ink]);
    expect(svg).toContain('fill="#6ea8fe" stroke="none"');
    expect(extractStrokes(svg)).toEqual([stroke, ink]);
  });

  it("is hit inside the ink and missed beside it", () => {
    const stroke: Stroke = { d: inkOutline(line, 4, { pressure: false, last: true }), color: "#fff", width: 4, ink: true };
    expect(hitStroke([stroke], 20, 10)).toBe(0);
    expect(hitStroke([stroke], 20, 40)).toBe(-1);
  });
});
