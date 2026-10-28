// @vitest-environment jsdom

import { act, createElement, createRef } from "react";
import { createRoot } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { FRAME_FADE_MS, HTMLCardFrame, offerSource, promoteFrame, serverVendorSource } from "./html-card";
import { setConnection } from "./api";
import { PopOut, withScrollReporter } from "./Card";
import type { Node } from "./api";

describe("HTMLCardFrame", () => {
  it("loads vendor scripts from the connected server in a remote shell", () => {
    setConnection({ baseUrl: "http://127.0.0.1:8787", token: null });
    const source = `<script src="/vendor/plotly-basic-2.35.2.min.js"></script>`;
    expect(serverVendorSource(source)).toContain(
      `src="http://127.0.0.1:8787/vendor/plotly-basic-2.35.2.min.js"`,
    );
    setConnection({ baseUrl: "", token: null });
    expect(serverVendorSource(source)).toBe(source);
  });

  it("keeps the parent and card in separate opaque origins", () => {
    const html = renderToStaticMarkup(createElement(HTMLCardFrame, {
      className: "card-body html",
      srcDoc: "<p>demo</p>",
      title: "demo",
    }));
    expect(html).toContain('sandbox="allow-scripts"');
    expect(html).not.toContain("allow-same-origin");
    expect(html).not.toContain("allow-forms");
  });

  it("injects a syntactically valid in-frame search bridge", () => {
    const document = new DOMParser().parseFromString(
      withScrollReporter("<!doctype html><p>searchable</p>"),
      "text/html",
    );
    const script = document.querySelector("script");

    expect(script?.textContent).toContain("analog-search-result");
    // The selection bridge rides the same script (#125).
    expect(script?.textContent).toContain("analog-selection");
    expect(script?.textContent).toContain("analog-scroll-to");
    expect(() => new Function(script?.textContent ?? "")).not.toThrow();
  });
});

describe("PopOut", () => {
  const node = { id: "c_1", type: "text", sp_kind: "html", text: "<p>hi</p>", x: 0, y: 0, width: 100, height: 100 } as Node;
  const render = (annotateMode: boolean) => renderToStaticMarkup(createElement(PopOut, {
    node, annotations: [], annotateMode, draft: null, selectedAnnotation: null,
    onToggleAnnotate: () => {}, onSelectAnnotation: () => {}, onDraft: () => {}, onClose: () => {},
  }));

  it("can be annotated at full window, like the card on the canvas", () => {
    const html = render(true);
    expect(html).toContain("annotation-layer active");
    // Pins track the frame's scroll only if the reporter is injected here too.
    expect(html).toContain("analog-scroll");
    expect(html).toContain('sandbox="allow-scripts"');
  });

  it("leaves the card interactive outside comment mode", () => {
    expect(render(false)).not.toContain("annotation-layer active");
  });
});

describe("frame swap (#127)", () => {
  const box = { left: 0, top: 0, width: 300, height: 200 };

  it("loads a new source beside the live frame and keeps only the newest", () => {
    const live = [{ key: 0, srcDoc: "a", role: "live" as const }];
    const one = offerSource(live, "b", 1, box);
    expect(one.map((f) => f.role)).toEqual(["live", "incoming"]);
    const two = offerSource(one, "c", 2, box);
    expect(two.map((f) => [f.key, f.role])).toEqual([[0, "live"], [2, "incoming"]]);
    // Back to what is already showing: nothing to load.
    expect(offerSource(two, "a", 3, box)).toEqual(live);
  });

  it("promotes the loaded frame and retires the old one over it", () => {
    const frames = offerSource([{ key: 0, srcDoc: "a", role: "live" }], "b", 1, box);
    expect(promoteFrame(frames, 1, box)).toEqual([
      { key: 0, srcDoc: "a", role: "retiring", box },
      { key: 1, srcDoc: "b", role: "live" },
    ]);
    // A stale promotion (a timeout after the acknowledgement) changes nothing.
    const promoted = promoteFrame(frames, 1, box);
    expect(promoteFrame(promoted, 1, box)).toBe(promoted);
  });
});

describe("HTMLCardFrame swapping", () => {
  let host: HTMLDivElement | null = null;
  let root: ReturnType<typeof createRoot> | null = null;

  beforeAll(() => {
    (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean })
      .IS_REACT_ACT_ENVIRONMENT = true;
  });
  afterEach(() => {
    if (root) act(() => root!.unmount());
    host?.remove();
    root = host = null;
    vi.useRealTimers();
  });

  function mount(srcDoc: string, ref = createRef<HTMLIFrameElement>(), onLoad = () => {}) {
    host ??= document.body.appendChild(document.createElement("div"));
    root ??= createRoot(host);
    act(() => root!.render(createElement(HTMLCardFrame, { srcDoc, title: "t", className: "card-body html", ref, onLoad })));
    return ref;
  }
  const frames = () => [...host!.querySelectorAll("iframe")];
  const load = (frame: HTMLIFrameElement) => act(() => { frame.dispatchEvent(new Event("load")); });
  const report = (frame: HTMLIFrameElement, data: Record<string, unknown>) => act(() => {
    window.dispatchEvent(new MessageEvent("message", { data: { type: "analog-scroll", ...data }, source: frame.contentWindow }));
  });

  it("keeps the live frame until the new one has loaded and taken its scroll", () => {
    vi.useFakeTimers();
    const onLoad = vi.fn();
    const ref = mount("<p>one</p>", undefined, onLoad);
    const first = frames()[0]!;
    expect(ref.current).toBe(first);
    report(first, { sx: 0, sy: 320 });

    mount("<p>two</p>", ref, onLoad);
    const [, incoming] = frames();
    expect(incoming!.className).toContain("html-frame-incoming");
    expect(incoming!.getAttribute("aria-hidden")).toBe("true");
    expect(ref.current).toBe(first);

    const asked = vi.spyOn(incoming!.contentWindow!, "postMessage");
    load(incoming!);
    expect(asked).toHaveBeenCalledWith({ type: "analog-scroll-to", sx: 0, sy: 320, ack: 1 }, "*");
    // A report without the acknowledgement is the document's own, from before the restore.
    report(incoming!, { sx: 0, sy: 0 });
    expect(ref.current).toBe(first);

    report(incoming!, { sx: 0, sy: 320, ack: 1 });
    expect(ref.current).toBe(incoming);
    expect(onLoad).toHaveBeenCalledTimes(1);
    expect(first.className).toContain("html-frame-retiring");
    expect(incoming!.className).toBe("card-body html");

    act(() => { vi.advanceTimersByTime(FRAME_FADE_MS); });
    expect(frames()).toEqual([incoming]);
  });

  it("shows the new frame anyway if the restore is never acknowledged", () => {
    vi.useFakeTimers();
    const ref = mount("<p>one</p>");
    report(frames()[0]!, { sx: 0, sy: 50 });
    mount("<p>two</p>", ref);
    const incoming = frames()[1]!;
    load(incoming);
    expect(ref.current).not.toBe(incoming);
    act(() => { vi.advanceTimersByTime(1000); });
    expect(ref.current).toBe(incoming);
    expect(frames()).toEqual([incoming]);
  });
});
