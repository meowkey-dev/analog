// @vitest-environment jsdom

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { HTMLCardFrame } from "./html-card";

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
});
