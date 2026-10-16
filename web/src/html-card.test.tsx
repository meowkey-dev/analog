// @vitest-environment jsdom

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { HTMLCardFrame, serverVendorSource } from "./html-card";
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
