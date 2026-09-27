// @vitest-environment jsdom

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ExportMenu } from "./ExportMenu";
import {
  buildExportHTML,
  escapeHtml,
  fileStem,
  rewriteCssUrls,
  svgFileText,
  waitForExportResources,
  wrapExportDocument,
} from "./export";

describe("escapeHtml", () => {
  it("escapes markup in titles", () => {
    expect(escapeHtml(`A & B <C>"`)).toBe("A &amp; B &lt;C&gt;&quot;");
  });
});

describe("rewriteCssUrls", () => {
  it("rewrites http urls and leaves data uris", () => {
    const css = `src: url("/fonts/a.woff2"), url('https://x/b.woff2') format("woff2"), url(data:font/woff2,abc)`;
    const out = rewriteCssUrls(css, (url) => `DATA(${url})`);
    expect(out).toContain('url("DATA(/fonts/a.woff2)")');
    expect(out).toContain('url("DATA(https://x/b.woff2)")');
    expect(out).toContain("url(data:font/woff2,abc)");
  });
});

describe("wrapExportDocument", () => {
  it("is a portable page sized to the board", () => {
    const html = wrapExportDocument({
      title: "Nav <redesign>",
      slug: "redesign",
      css: ".card{color:red}",
      body: "<div class=\"export-board\">x</div>",
      width: 800,
      height: 600,
      mdScale: "1.25",
    });
    expect(html.startsWith("<!DOCTYPE html>")).toBe(true);
    expect(html).toContain("Nav &lt;redesign&gt;");
    expect(html).toContain("/redesign");
    expect(html).toContain("--md-scale: 1.25");
    expect(html).toContain("@page { size: 800px 644px");
    expect(html).toContain(".card{color:red}");
    expect(html).toContain("analogExportReady");
    expect(html).toContain("const deadline = Date.now() + 4000");
    expect(html).toContain("await Promise.all([fontReady, mediaReadyGroup, frameReadyGroup])");
  });

  it("does not add capabilities to an exported html card", () => {
    const html = wrapExportDocument({
      title: "Demo", slug: "demo", css: "", width: 100, height: 80,
      body: '<iframe sandbox="allow-scripts" srcdoc="&lt;p&gt;demo&lt;/p&gt;"></iframe>',
    });
    expect(html).toContain('sandbox="allow-scripts"');
    expect(html).not.toContain("allow-same-origin");
    expect(html).not.toContain("allow-forms");
  });
});

describe("buildExportHTML", () => {
  afterEach(() => {
    document.body.replaceChildren();
    vi.restoreAllMocks();
  });

  it("serializes the live DOM, strips editor chrome, and embeds image/PDF file resources", async () => {
    document.body.innerHTML = `
      <main class="canvas"><div class="viewport">
        <svg class="links"><path class="edge-hit"/><path class="edge-line"/></svg>
        <article class="card selected" data-card-id="c_1"
                 style="left:10px;top:20px;width:200px;height:100px">
            <header class="card-head"><span class="card-title">Report</span>
            <button class="icon">delete</button></header>
          <div class="body-zone">
            <div class="card-body plain">rendered body</div>
            <textarea class="card-body editor">in-progress body</textarea>
            <img class="uploaded" src="blob:image" alt="shot">
            <img class="external" src="https://cdn.example/image.png" alt="external">
            <a data-file-card-link="true" href="blob:pdf">report.pdf</a>
            <div class="annotation-layer"><button>pin</button></div>
            <div class="card-thread">comment</div>
            <div class="draw-tools"><button>done</button></div>
          </div>
          <div class="handle resize se"></div>
          <footer class="card-foot">agent <span>rev 1</span></footer>
        </article>
      </div></main>`;
    const card = document.querySelector<HTMLElement>("[data-card-id]")!;
    Object.defineProperty(card, "offsetWidth", { configurable: true, value: 200 });
    Object.defineProperty(card, "offsetHeight", { configurable: true, value: 100 });
    const image = document.querySelector<HTMLImageElement>("img")!;
    Object.defineProperty(image, "complete", { configurable: true, value: true });
    const external = document.querySelector<HTMLImageElement>("img.external")!;
    Object.defineProperty(external, "complete", { configurable: true, value: true });

    const fetchRequests: RequestInit[] = [];
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (init) fetchRequests.push(init);
      if (String(input) === "https://cdn.example/image.png") return { ok: false } as Response;
      const type = String(input) === "blob:pdf" ? "application/pdf" : "image/png";
      return {
        ok: true,
        blob: async () => new Blob([String(input)], { type }),
      } as Response;
    });
    vi.stubGlobal("fetch", fetchMock);

    const html = await buildExportHTML({ title: "Nav", slug: "redesign" });
    const parsed = new DOMParser().parseFromString(html, "text/html");

    expect(parsed.querySelector("[data-card-id]")).not.toBeNull();
    expect(parsed.querySelector(".selected")).toBeNull();
    expect(parsed.querySelector(".icon, .handle, .annotation-layer, .card-thread, .draw-tools")).toBeNull();
    expect(parsed.querySelector(".edge-hit")).toBeNull();
    expect(parsed.querySelector(".card-body.plain")?.textContent).toContain("rendered body");
    expect(parsed.querySelector("textarea.editor")).toBeNull();
    expect(parsed.body.textContent).toContain("in-progress body");
    expect(parsed.querySelector("img")?.getAttribute("src")).toMatch(/^data:image\/png;base64,/);
    expect(parsed.querySelector("img.external")?.getAttribute("src"))
      .toBe("https://cdn.example/image.png");
    expect(parsed.querySelector("a[data-file-card-link]")?.getAttribute("href"))
      .toMatch(/^data:application\/pdf;base64,/);
    expect(fetchMock).toHaveBeenCalledTimes(3);
    for (const init of fetchRequests) expect(init.credentials).toBe("omit");
  });

  it("waits for pending images without blocking a live sandboxed iframe", async () => {
    const image = document.createElement("img");
    Object.defineProperty(image, "complete", { configurable: true, value: false });
    const frame = document.createElement("iframe");
    Object.defineProperty(frame, "contentDocument", { configurable: true, value: null });
    document.body.append(image, frame);

    const pending = waitForExportResources(document);
    image.dispatchEvent(new Event("load"));
    await expect(Promise.race([
      pending.then(() => "done"),
      new Promise<string>((resolve) => setTimeout(() => resolve("timeout"), 250)),
    ])).resolves.toBe("done");
  });

  it("embeds a vendor script once for multiple sandboxed chart cards", async () => {
    document.body.innerHTML = `<main class="canvas"><div class="viewport">
      <article data-card-id="a" style="left:0px;top:0px;width:200px;height:100px">
        <iframe sandbox="allow-scripts"></iframe></article>
      <article data-card-id="b" style="left:210px;top:0px;width:200px;height:100px">
        <iframe sandbox="allow-scripts"></iframe></article>
    </div></main>`;
    const source = `<script src="/vendor/plotly-basic-2.35.2.min.js"></script><div>chart</div>`;
    document.querySelectorAll("iframe").forEach((frame) => frame.setAttribute("srcdoc", source));
    document.querySelectorAll<HTMLElement>("[data-card-id]").forEach((card) => {
      Object.defineProperty(card, "offsetWidth", { value: 200 });
      Object.defineProperty(card, "offsetHeight", { value: 100 });
    });
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => ({
      ok: true,
      arrayBuffer: async () => new TextEncoder().encode("window.Plotly = {};").buffer,
    } as Response));
    vi.stubGlobal("fetch", fetchMock);

    const html = await buildExportHTML({ title: "Charts", slug: "charts" });
    const parsed = new DOMParser().parseFromString(html, "text/html");
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock.mock.calls[0]?.[0]).toContain("/vendor/plotly-basic-2.35.2.min.js");
    expect(parsed.querySelectorAll("iframe[data-analog-srcdoc]")).toHaveLength(2);
    expect(parsed.querySelector("iframe[srcdoc]")).toBeNull();
    expect(html.match(/const encoded =/g)).toHaveLength(1);
    expect(html).toContain(btoa("window.Plotly = {};"));
    expect(html).toContain('sandbox="allow-scripts"');
  });
});

describe("ExportMenu", () => {
  it("renders the topbar control", () => {
    const html = renderToStaticMarkup(
      createElement(ExportMenu, {
        title: "Nav", slug: "redesign",
        onError: () => {}, onBusy: () => {},
      }),
    );
    expect(html).toContain("export");
    expect(html).toContain("Save the board as HTML or PDF");
  });
});

describe("svgFileText", () => {
  function body(markup: string, style = ""): HTMLElement {
    const el = document.createElement("div");
    el.setAttribute("style", style);
    el.innerHTML = markup;
    document.body.append(el);
    return el;
  }

  afterEach(() => document.body.replaceChildren());

  it("is a standalone XML file with the SVG namespace (#105)", () => {
    const text = svgFileText(body('<svg viewBox="0 0 10 10"><circle cx="5" cy="5" r="4"/><text>a&nbsp;b</text></svg>'));
    expect(text).toMatch(/^<\?xml /);
    expect(text).toContain('xmlns="http://www.w3.org/2000/svg"');
    expect(text).not.toContain("&nbsp;");
    const doc = new DOMParser().parseFromString(text!, "image/svg+xml");
    expect(doc.querySelector("parsererror")).toBeNull();
    expect(doc.documentElement.localName).toBe("svg");
  });

  it("carries the colour the card gave currentColor, keeping its own style last", () => {
    const text = svgFileText(body(
      '<svg style="opacity: 0.5"><path fill="currentColor" d="M0 0h1v1z"/></svg>',
      "color: rgb(1, 2, 3)",
    ))!;
    const svg = new DOMParser().parseFromString(text, "image/svg+xml").documentElement;
    const style = svg.getAttribute("style")!;
    expect(style).toContain("color: rgb(1, 2, 3)");
    expect(style.endsWith("opacity: 0.5")).toBe(true);
  });

  it("keeps every svg root, not just the first", () => {
    const text = svgFileText(body('<svg id="a"><rect width="1" height="1"/></svg><svg id="b"><circle r="1"/></svg>'))!;
    const outer = new DOMParser().parseFromString(text, "image/svg+xml").documentElement;
    expect(outer.localName).toBe("svg");
    expect(Array.from(outer.children).map((c) => c.id)).toEqual(["a", "b"]);
    for (const child of Array.from(outer.children)) {
      expect(child.hasAttribute("x")).toBe(true);
      expect(child.hasAttribute("width")).toBe(true);
    }
  });

  it("is null without an svg element", () => {
    expect(svgFileText(body("<p>nope</p>"))).toBeNull();
  });
});

describe("fileStem", () => {
  it("strips characters a filesystem refuses", () => {
    expect(fileStem(' a/b: "c" ', "c_x")).toBe("a-b- -c");
    expect(fileStem("  ", "c_x")).toBe("c_x");
    expect(fileStem(undefined, "c_x")).toBe("c_x");
  });
});
