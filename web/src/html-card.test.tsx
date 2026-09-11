// @vitest-environment jsdom

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { HTMLCardFrame } from "./html-card";
import { withScrollReporter } from "./Card";

describe("HTMLCardFrame", () => {
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
