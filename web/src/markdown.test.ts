import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import Markdown from "react-markdown";
import { describe, expect, it } from "vitest";
import { mdRemarkPlugins, mdRehypePlugins } from "./markdown";

function render(text: string): string {
  return renderToStaticMarkup(
    createElement(Markdown, { remarkPlugins: mdRemarkPlugins, rehypePlugins: mdRehypePlugins }, text),
  );
}

describe("markdown math", () => {
  it("renders inline TeX as KaTeX", () => {
    const html = render("The identity $e^{i\\pi}+1=0$ holds.");
    expect(html).toContain("katex");
    expect(html).not.toContain("katex-display");
    expect(html).toContain("e");
  });

  it("renders display TeX as a block", () => {
    const html = render("$$\n\\int_0^1 x^2\\,dx\n$$");
    expect(html).toContain("katex-display");
  });

  it("renders malformed TeX instead of throwing", () => {
    expect(() => render("$\\notARealTeXCommand$")).not.toThrow();
    const html = render("$\\notARealTeXCommand$");
    expect(html).toContain("katex");
    expect(html).toContain("notARealTeXCommand");
  });

  it("still renders GFM tables", () => {
    const html = render("| a | b |\n| --- | --- |\n| 1 | 2 |");
    expect(html).toContain("<table>");
    expect(html).toContain("<td>1</td>");
  });
});
