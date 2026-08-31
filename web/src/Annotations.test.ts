import { describe, expect, it } from "vitest";
import { pinStyle, type ScrollMetrics } from "./Annotations";

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
