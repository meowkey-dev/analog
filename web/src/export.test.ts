// @vitest-environment jsdom

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ExportMenu } from "./ExportMenu";
import {
  buildExportHTML,
  escapeHtml,
  rewriteCssUrls,
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
