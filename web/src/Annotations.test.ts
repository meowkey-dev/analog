import { describe, expect, it } from "vitest";
import {
  composerPosition, pinStyle, selectionButtonStyle, selectionSelector, type ScrollMetrics,
} from "./Annotations";

const measured: ScrollMetrics = {
  cw: 800, ch: 1200,
  vw: 400, vh: 300,
  sx: 40, sy: 100,
};

describe("pinStyle", () => {
  it("leaves point dimensions to the circular CSS marker", () => {
    const style = pinStyle({ x: 0.25, y: 0.5 }, measured);

    expect(style).toEqual({ left: 160, top: 500 });
  });

  it("maps region dimensions into the measured content", () => {
    const style = pinStyle({ x: 0.25, y: 0.5, w: 0.25, h: 0.5 }, measured);

    expect(style).toEqual({ left: 160, top: 500, width: 200, height: 600 });
  });

  it("does not inline zero dimensions before content is measured", () => {
    expect(pinStyle({ x: 0.25, y: 0.5 }, null)).toEqual({ left: "25%", top: "50%" });
  });
});

describe("selectionSelector", () => {
  it("turns a selection in the visible box into a rect over the content", () => {
    // The visible box is the top-left quarter of the content, scrolled by (40, 100).
    const selector = selectionSelector({ x: 0.5, y: 0.5, w: 0.25, h: 0.1 }, measured);

    expect(selector.type).toBe("rect");
    expect(selector.x).toBeCloseTo((0.5 * 400 + 40) / 800);
    expect(selector.y).toBeCloseTo((0.5 * 300 + 100) / 1200);
    expect(selector.w).toBeCloseTo((0.25 * 400) / 800);
    expect(selector.h).toBeCloseTo((0.1 * 300) / 1200);
  });

  it("clamps a selection that runs past the content", () => {
    const selector = selectionSelector({ x: 0.9, y: 0.9, w: 0.5, h: 0.5 }, null);

    expect(selector.x + selector.w).toBe(1);
    expect(selector.y + selector.h).toBe(1);
  });
});

describe("selectionButtonStyle", () => {
  it("sits under the selection's end", () => {
    const style = selectionButtonStyle({ x: 0.25, y: 0.25, w: 0.1, h: 0.05 }, measured);

    expect(style).toEqual({ left: 0.35 * 800 - 40, top: 0.3 * 1200 - 100 + 4 });
  });

  it("stays inside the visible box when the selection runs off it", () => {
    const style = selectionButtonStyle({ x: 0.5, y: 0.5, w: 0.5, h: 0.5 }, measured);

    expect(style).toEqual({ left: 400 - 4, top: 300 - 30 });
  });
});

describe("composerPosition", () => {
  const desktop = { w: 1400, h: 900 };

  it("opens below the gesture", () => {
    expect(composerPosition({ x: 500, y: 200 }, desktop)).toEqual({ left: 476, top: 214 });
  });

  it("opens above when there is no room below", () => {
    expect(composerPosition({ x: 500, y: 850 }, desktop)?.top).toBeLessThan(850);
  });

  it("stays on screen at the right edge", () => {
    const place = composerPosition({ x: 1395, y: 200 }, desktop)!;
    expect(place.left + 440).toBeLessThanOrEqual(desktop.w - 8);
  });

  it("clamps against the measured height, not the usual one", () => {
    // 230px fits below a gesture at 600; a 300px composer does not, so it opens above.
    expect(composerPosition({ x: 500, y: 600 }, desktop)).toEqual({ left: 476, top: 614 });
    const tall = composerPosition({ x: 500, y: 600 }, desktop, 300)!;
    expect(tall.top).toBe(600 - 300 - 14);
    expect(tall.top + 300).toBeLessThanOrEqual(desktop.h - 8);
  });

  it("stays on screen when it fits neither above nor below", () => {
    const place = composerPosition({ x: 500, y: 300 }, { w: 1400, h: 500 }, 420)!;
    expect(place.top).toBeGreaterThanOrEqual(8);
    expect(place.top + 420).toBeLessThanOrEqual(500 - 8);
  });

  it("stays docked without a gesture or on a phone", () => {
    expect(composerPosition(undefined, desktop)).toBeNull();
    expect(composerPosition({ x: 100, y: 100 }, { w: 390, h: 800 })).toBeNull();
  });
});
