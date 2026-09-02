/**
 * drawing cards are svg text nodes authored with a pen (#61). strokes append as
 * tagged <path>s so an agent chart stays intact when the human marks it up; the
 * tag is how a later edit finds them again without a new sp_kind.
 */

export type Point = [number, number];

export interface ViewBox {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface Stroke {
  d: string;
  color: string;
  width: number;
}

export const DRAW_WIDTH = 480;
export const DRAW_HEIGHT = 360;
/** viewBox height: card height minus the chrome the body does not own. */
export const DRAW_VIEW_H = 300;

export const DRAW_COLORS = [
  "#dfe3ec", "#6ea8fe", "#e06c75", "#e0a35a", "#63c187", "#12141a", "#ffffff",
] as const;

export const DRAW_WIDTHS = [2, 4, 8] as const;

const STROKE_TAG = 'data-analog-stroke="1"';
const STROKE_PATTERN = String.raw`<path\b[^>]*data-analog-stroke="1"[^>]*\/?>`;

export function emptyDrawing(width = DRAW_WIDTH, height = DRAW_VIEW_H): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}"></svg>`;
}

export function parseViewBox(svg: string, fallback: ViewBox = { x: 0, y: 0, w: DRAW_WIDTH, h: DRAW_VIEW_H }): ViewBox {
  const vb = /viewBox\s*=\s*"([^"]+)"/i.exec(svg);
  if (vb) {
    const parts = vb[1]!.trim().split(/[\s,]+/).map(Number);
    if (parts.length === 4 && parts.every(Number.isFinite) && parts[2]! > 0 && parts[3]! > 0) {
      return { x: parts[0]!, y: parts[1]!, w: parts[2]!, h: parts[3]! };
    }
  }
  const width = Number(/<svg\b[^>]*\bwidth="([\d.]+)"/i.exec(svg)?.[1]);
  const height = Number(/<svg\b[^>]*\bheight="([\d.]+)"/i.exec(svg)?.[1]);
  if (width > 0 && height > 0) return { x: 0, y: 0, w: width, h: height };
  return fallback;
}

function attr(tag: string, name: string): string | undefined {
  // `(?:^|\\s)` so `stroke` does not match the tail of `data-analog-stroke`.
  return new RegExp(`(?:^|[\\s])${name}="([^"]*)"`, "i").exec(tag)?.[1];
}

export function extractStrokes(svg: string): Stroke[] {
  const strokes: Stroke[] = [];
  for (const match of svg.matchAll(new RegExp(STROKE_PATTERN, "gi"))) {
    const tag = match[0];
    const d = attr(tag, "d");
    if (!d) continue;
    strokes.push({
      d,
      color: attr(tag, "stroke") || DRAW_COLORS[0] || "#dfe3ec",
      width: Number(attr(tag, "stroke-width") || DRAW_WIDTHS[0]) || DRAW_WIDTHS[0] || 2,
    });
  }
  return strokes;
}

export function stripStrokes(svg: string): string {
  return svg.replace(new RegExp(String.raw`\s*` + STROKE_PATTERN, "gi"), "");
}

/** inner markup of an svg, analog strokes removed, so the editor can layer them. */
export function svgBackground(svg: string): string {
  const stripped = stripStrokes(svg);
  const open = /<svg\b[^>]*>/i.exec(stripped);
  if (!open || open.index === undefined) return "";
  const close = stripped.lastIndexOf("</svg>");
  if (close < 0) return "";
  return stripped.slice(open.index + open[0].length, close).trim();
}

function escapeAttr(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
}

export function strokeTag(stroke: Stroke): string {
  return `<path ${STROKE_TAG} class="analog-stroke" fill="none" stroke="${escapeAttr(stroke.color)}" stroke-width="${stroke.width}" stroke-linecap="round" stroke-linejoin="round" d="${escapeAttr(stroke.d)}"/>`;
}

/**
 * append (or replace) analog strokes just before </svg>. a document that is not
 * already an svg becomes a blank drawing; agent markup is otherwise preserved.
 */
export function serializeDrawing(svg: string, strokes: Stroke[], fallback: ViewBox = { x: 0, y: 0, w: DRAW_WIDTH, h: DRAW_VIEW_H }): string {
  const base = /<svg[\s>]/i.test(svg) ? stripStrokes(svg) : emptyDrawing(fallback.w, fallback.h);
  const paths = strokes.map(strokeTag).join("");
  const close = base.lastIndexOf("</svg>");
  if (close < 0) return emptyDrawing(fallback.w, fallback.h).replace("</svg>", paths + "</svg>");
  return base.slice(0, close) + paths + base.slice(close);
}

export function fmt(n: number): string {
  return (Math.round(n * 10) / 10).toString();
}

export function pointsToPath(points: Point[]): string {
  if (points.length === 0) return "";
  if (points.length === 1) {
    const p = points[0]!;
    return `M${fmt(p[0])} ${fmt(p[1])}l.01 0`;
  }
  return points.map((p, i) => `${i === 0 ? "M" : "L"}${fmt(p[0])} ${fmt(p[1])}`).join("");
}

/** drop points that would not move the pen by a viewBox unit, so a stroke stays small. */
export function extendStroke(points: Point[], next: Point, minDist = 1.2): Point[] {
  const last = points[points.length - 1];
  if (!last) return [next];
  const dx = next[0] - last[0];
  const dy = next[1] - last[1];
  if (dx * dx + dy * dy < minDist * minDist) return points;
  return [...points, next];
}

export function parsePathPoints(d: string): Point[] {
  const points: Point[] = [];
  const re = /[ML]\s*(-?[\d.]+)\s*,?\s*(-?[\d.]+)/gi;
  let match: RegExpExecArray | null;
  while ((match = re.exec(d))) {
    points.push([Number(match[1]), Number(match[2])]);
  }
  return points;
}

function distToSegment(px: number, py: number, ax: number, ay: number, bx: number, by: number): number {
  const abx = bx - ax, aby = by - ay;
  const len2 = abx * abx + aby * aby;
  if (len2 === 0) return Math.hypot(px - ax, py - ay);
  let t = ((px - ax) * abx + (py - ay) * aby) / len2;
  t = Math.max(0, Math.min(1, t));
  return Math.hypot(px - (ax + t * abx), py - (ay + t * aby));
}

/** index of the stroke under the pointer, or -1. slop grows with stroke width. */
export function hitStroke(strokes: Stroke[], x: number, y: number): number {
  let best = -1;
  let bestDist = Infinity;
  for (let i = 0; i < strokes.length; i++) {
    const stroke = strokes[i]!;
    const pts = parsePathPoints(stroke.d);
    if (pts.length === 0) continue;
    const slop = stroke.width * 2 + 4;
    let d = Infinity;
    if (pts.length === 1) {
      const p = pts[0]!;
      d = Math.hypot(x - p[0], y - p[1]);
    } else {
      for (let j = 1; j < pts.length; j++) {
        const a = pts[j - 1]!, b = pts[j]!;
        d = Math.min(d, distToSegment(x, y, a[0], a[1], b[0], b[1]));
      }
    }
    if (d <= slop && d < bestDist) {
      best = i;
      bestDist = d;
    }
  }
  return best;
}

/**
 * pointer → viewBox, assuming the default `xMidYMid meet` letterboxing. the
 * editor SVG fills its box; the drawing itself may not.
 */
export function clientToViewBox(
  clientX: number,
  clientY: number,
  rect: { left: number; top: number; width: number; height: number },
  vb: ViewBox,
): Point {
  if (rect.width <= 0 || rect.height <= 0 || vb.w <= 0 || vb.h <= 0) return [vb.x, vb.y];
  const scale = Math.min(rect.width / vb.w, rect.height / vb.h);
  const ox = rect.left + (rect.width - vb.w * scale) / 2;
  const oy = rect.top + (rect.height - vb.h * scale) / 2;
  return [vb.x + (clientX - ox) / scale, vb.y + (clientY - oy) / scale];
}
