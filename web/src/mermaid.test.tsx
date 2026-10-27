// @vitest-environment jsdom

import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import Markdown from "react-markdown";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { mdRehypePlugins, mdRemarkPlugins } from "./markdown";
import { mdComponents } from "./mermaid";

beforeAll(() => {
  // jsdom has no 2D context; Pele then estimates text widths, as it does in Node.
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean })
    .IS_REACT_ACT_ENVIRONMENT = true;
});

let root: Root | undefined;
let host: HTMLElement | undefined;

afterEach(() => {
  act(() => root?.unmount());
  host?.remove();
  root = host = undefined;
});

async function mount(text: string): Promise<HTMLElement> {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  await act(async () => {
    root!.render(
      <Markdown remarkPlugins={mdRemarkPlugins} rehypePlugins={mdRehypePlugins} components={mdComponents}>
        {text}
      </Markdown>,
    );
  });
  return host;
}

async function settled(el: HTMLElement): Promise<void> {
  await vi.waitFor(() => expect(el.querySelector("[data-mermaid-pending]")).toBeNull());
}

const flowchart = "```mermaid\nflowchart LR\n  A[Draft] --> B[Review]\n```";

describe("mermaid in md cards", () => {
  it("renders a mermaid fence as a sanitized svg", async () => {
    const el = await mount(`Before\n\n${flowchart}\n\nAfter`);
    await settled(el);
    const svg = el.querySelector(".mermaid svg.pele");
    expect(svg).not.toBeNull();
    expect(svg!.textContent).toContain("Draft");
    expect(el.querySelector("code.language-mermaid")).toBeNull();
    expect(el.textContent).toContain("Before");
    expect(el.textContent).toContain("After");
  });

  it("keeps other code blocks as code", async () => {
    const el = await mount("```js\nconst a = 1;\n```");
    expect(el.querySelector("pre > code.language-js")?.textContent).toBe("const a = 1;\n");
    expect(el.querySelector(".mermaid")).toBeNull();
  });

  it("shows the source and the error for a broken diagram", async () => {
    const el = await mount("```mermaid\nflowchart TD\n  A -->\n```");
    await settled(el);
    expect(el.querySelector(".mermaid svg")).toBeNull();
    expect(el.querySelector("code.language-mermaid")?.textContent).toContain("A -->");
    expect(el.querySelector(".mermaid-error")?.textContent).toMatch(/^line 3: Expecting/);
  });

  it("does not let a diagram link navigate the board", async () => {
    const el = await mount("```mermaid\nflowchart LR\n  A --> B\n  click A \"https://example.com\"\n```");
    await settled(el);
    expect(el.querySelector(".mermaid svg")).not.toBeNull();
    expect(el.querySelector(".mermaid a")).toBeNull();
  });

  it("gives each diagram its own id prefix", async () => {
    // sankey paints its links with gradients referenced by id.
    const sankey = "```mermaid\nsankey\n  a,b,3\n```";
    const el = await mount(`${sankey}\n\n${sankey}`);
    await settled(el);
    const ids = Array.from(el.querySelectorAll(".mermaid [id]"), (n) => n.id);
    expect(ids.length).toBeGreaterThanOrEqual(2);
    expect(new Set(ids).size).toBe(ids.length);
  });
});
