/**
 * Snapshot the live board into a portable HTML file (#72). The renderer — markdown,
 * KaTeX, html iframes, drawings — already ran; we serialise that DOM so a saved
 * file looks like the space, with Analog styles and uploaded media embedded.
 * Sandboxed html-card dependencies remain external when the parent cannot safely
 * capture them.
 *
 * PDF is the browser's print of the same document. analog-server stays CGO-free.
 */

const STRIP =
  ".handle, .icon, .theme-wrap, .annotation-layer, .card-thread, " +
  ".edge-hit, .edge-delete, .edge-delete-x, .badge.comments, .draw-tools";

const PAD = 48;
const RESOURCE_TIMEOUT = 4000;

// The generated page waits once more after it is opened. Cloning an iframe does
// not clone its browsing context, so the print window must wait for those fresh
// documents as well as the parent page's fonts and media.
const READY_SCRIPT = `<script>
(() => {
  const waitUntil = (promise, deadline) => new Promise(resolve => {
    let done = false;
    let timer;
    const finish = () => {
      if (done) return;
      done = true;
      if (timer !== undefined) clearTimeout(timer);
      resolve();
    };
    timer = setTimeout(finish, Math.max(0, deadline - Date.now()));
    Promise.resolve(promise).then(finish, finish);
  });
  const loaded = (element, deadline) => new Promise(resolve => {
    let done = false;
    let timer;
    const finish = () => {
      if (done) return;
      done = true;
      if (timer !== undefined) clearTimeout(timer);
      element.removeEventListener('load', finish);
      element.removeEventListener('error', finish);
      resolve();
    };
    element.addEventListener('load', finish, { once: true });
    element.addEventListener('error', finish, { once: true });
    timer = setTimeout(finish, Math.max(0, deadline - Date.now()));
  });
  const mediaReady = (element, deadline) => {
    if (element instanceof HTMLImageElement && element.complete) {
      if (!element.decode) return Promise.resolve();
      try { return waitUntil(element.decode(), deadline); } catch { return Promise.resolve(); }
    }
    return loaded(element, deadline);
  };
  const frameReady = (frame, deadline) => {
    if (frame.contentDocument && frame.contentDocument.readyState === 'complete') return Promise.resolve();
    return loaded(frame, deadline);
  };
  const ready = async () => {
    const deadline = Date.now() + ${RESOURCE_TIMEOUT};
    const fontReady = document.fonts && document.fonts.ready
      ? waitUntil(document.fonts.ready, deadline) : Promise.resolve();
    const media = [...document.querySelectorAll('img, embed, object, source, image')];
    const mediaReadyGroup = Promise.all(media.map(element => mediaReady(element, deadline)));
    const frames = [...document.querySelectorAll('iframe')];
    const frameReadyGroup = Promise.all(frames.map(frame => frameReady(frame, deadline)));
    await Promise.all([fontReady, mediaReadyGroup, frameReadyGroup]);
    document.documentElement.dataset.analogExportReady = 'true';
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', ready, { once: true });
  else void ready();
})();
</script>`;

export function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

/** Rewrite url(...) in CSS through `resolve`, leaving data: and about: alone. */
export function rewriteCssUrls(css: string, resolve: (url: string) => string): string {
  return css.replace(/url\(\s*(['"]?)([^'")]+)\1\s*\)/g, (full, _q: string, url: string) => {
    const trimmed = url.trim();
    if (/^(data:|about:|blob:)/i.test(trimmed)) return full;
    return `url("${resolve(trimmed)}")`;
  });
}

export function wrapExportDocument(opts: {
  title: string;
  slug: string;
  css: string;
  body: string;
  width: number;
  height: number;
  mdScale?: string;
}): string {
  const mdScale = opts.mdScale?.trim() || "1";
  const pageH = opts.height + 44;
  return `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(opts.title)} — analog</title>
<style>
:root { --md-scale: ${escapeHtml(mdScale)}; }
${opts.css}
html, body { margin: 0; min-height: 100%; overflow: auto; height: auto; }
.app, .topbar, .hints, .panel, .zoom, .toast, .composer { display: none !important; }
.export-head {
  display: flex; align-items: baseline; gap: 10px;
  padding: 10px 16px; border-bottom: 1px solid var(--line); background: var(--panel);
  height: 44px;
}
.export-head .brand { font-weight: 650; letter-spacing: .04em; color: var(--accent); }
.export-head .title { font-weight: 550; }
.export-head .slug { color: var(--dim); }
.export-board { position: relative; overflow: hidden; }
.export-world { position: absolute; left: 0; top: 0; }
.card.selected { border-color: var(--line); box-shadow: 0 6px 18px rgba(0,0,0,.3); }
* { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
@page { size: ${opts.width}px ${pageH}px; margin: 0; }
</style>${READY_SCRIPT}</head>
<body>
<header class="export-head"><span class="brand">analog</span>
<span class="title">${escapeHtml(opts.title)}</span>
<span class="slug">/${escapeHtml(opts.slug)}</span></header>
${opts.body}
</body></html>
`;
}

export async function buildExportHTML(opts: { title: string; slug: string }): Promise<string> {
  const viewport = document.querySelector<HTMLElement>(".canvas .viewport");
  const cards = Array.from(document.querySelectorAll<HTMLElement>("[data-card-id]"));
  await waitForExportResources(viewport ?? document);
  const css = await inlineCssUrls(collectCss());
  const mdScale = getComputedStyle(document.documentElement).getPropertyValue("--md-scale") || "1";

  if (!viewport || cards.length === 0) {
    return wrapExportDocument({
      title: opts.title, slug: opts.slug, css,
      body: `<p class="empty" style="color:var(--dim);padding:48px 16px">this space is empty</p>`,
      width: 640, height: 360, mdScale,
    });
  }

  const clone = viewport.cloneNode(true) as HTMLElement;
  replaceEditors(clone);
  clone.querySelectorAll(STRIP).forEach((el) => el.remove());
  clone.querySelectorAll(".selected").forEach((el) => el.classList.remove("selected"));
  clone.style.transform = "";
  clone.style.position = "relative";
  await inlineMedia(clone);

  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const card of cards) {
    const x = Number.parseFloat(card.style.left) || 0;
    const y = Number.parseFloat(card.style.top) || 0;
    const w = card.offsetWidth;
    const h = card.offsetHeight;
    minX = Math.min(minX, x);
    minY = Math.min(minY, y);
    maxX = Math.max(maxX, x + w);
    maxY = Math.max(maxY, y + h);
  }
  // the live .links layer is padded 600px past the cards for pan room; shrink it
  // to the card bbox so the snapshot does not clip arrows or grow by a metre.
  const links = clone.querySelector<SVGElement>("svg.links");
  if (links && Number.isFinite(minX)) {
    const w = Math.max(1, maxX - minX);
    const h = Math.max(1, maxY - minY);
    links.style.left = `${minX}px`;
    links.style.top = `${minY}px`;
    links.style.width = `${w}px`;
    links.style.height = `${h}px`;
    links.setAttribute("viewBox", `${minX} ${minY} ${w} ${h}`);
  }
  const width = Math.max(1, maxX - minX + PAD * 2);
  const height = Math.max(1, maxY - minY + PAD * 2);
  clone.style.transform = `translate(${PAD - minX}px, ${PAD - minY}px)`;
  clone.className = "export-world";

  const board = document.createElement("div");
  board.className = "export-board canvas";
  board.style.width = `${width}px`;
  board.style.height = `${height}px`;
  board.append(clone);

  return wrapExportDocument({
    title: opts.title, slug: opts.slug, css,
    body: board.outerHTML, width, height, mdScale,
  });
}

function replaceEditors(root: ParentNode): void {
  root.querySelectorAll<HTMLTextAreaElement>("textarea.editor").forEach((editor) => {
    const text = editor.ownerDocument.createElement("pre");
    text.className = "card-body plain";
    text.textContent = editor.value;
    editor.replaceWith(text);
  });
}

export function downloadText(filename: string, text: string, type: string): void {
  const blob = new Blob([text], { type });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

function collectCss(): string {
  const parts: string[] = [];
  for (const sheet of Array.from(document.styleSheets)) {
    let rules: CSSRuleList;
    try {
      rules = sheet.cssRules;
    } catch {
      continue;
    }
    for (const rule of Array.from(rules)) parts.push(rule.cssText);
  }
  return parts.join("\n");
}

async function inlineCssUrls(css: string): Promise<string> {
  const cache = new Map<string, string>();
  const matches = [...css.matchAll(/url\(\s*(['"]?)([^'")]+)\1\s*\)/g)];
  const unique = [...new Set(
    matches.map((m) => m[2]?.trim()).filter((url): url is string => Boolean(url)),
  )].filter((url) => !/^(data:|about:|blob:)/i.test(url));
  await Promise.all(unique.map(async (url) => {
    cache.set(url, await asDataURI(url).catch(() => url));
  }));
  return rewriteCssUrls(css, (url) => cache.get(url) ?? url);
}

async function inlineMedia(root: ParentNode): Promise<void> {
  const nodes = root.querySelectorAll<HTMLElement>(
    "img, embed, object, source, image, a[data-file-card-link]",
  );
  await Promise.all(Array.from(nodes).map(async (el) => {
    const attrs = el.matches("a[data-file-card-link]") ? ["href"] : ["src", "href", "data"];
    for (const attr of attrs) {
      const value = el.getAttribute(attr);
      if (!value || value.startsWith("data:")) continue;
      try {
        el.setAttribute(attr, await asDataURI(value));
      } catch {
        /* leave the original; a broken image is worse as a throw */
      }
    }
  }));
}

async function asDataURI(src: string): Promise<string> {
  if (src.startsWith("data:")) return src;
  // Export may encounter a user-authored external dependency. Fetch without
  // ambient credentials; if CORS or the server refuses it, preserve the URL and
  // document that dependency as an intentional portability boundary.
  const response = await fetch(src, { credentials: "omit" });
  if (!response.ok) throw new Error(String(response.status));
  const blob = await response.blob();
  return await new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}

/** Wait for the live board's resources before measuring and cloning its DOM. */
export async function waitForExportResources(root: ParentNode): Promise<void> {
  const owner = root instanceof Document ? root : root instanceof Element ? root.ownerDocument : null;
  const waits: Promise<unknown>[] = [];
  const fonts = owner?.fonts;
  if (fonts) waits.push(withTimeout(fonts.ready));
  // A live sandboxed srcdoc iframe may have loaded before its contentDocument is
  // inspectable. Its browsing context is preserved in the clone; the fresh print
  // document's readiness script is the relevant wait for that content.
  const elements = root.querySelectorAll<HTMLElement>("img, embed, object, source, image");
  elements.forEach((element) => {
    if (element instanceof HTMLImageElement && element.complete) {
      try {
        if (element.decode) waits.push(withTimeout(element.decode()));
      } catch {
        /* a broken image is already settled; keep its URL as the fallback */
      }
      return;
    }
    waits.push(withTimeout(waitForLoad(element)));
  });
  await Promise.all(waits);
}

/** Wait for the readiness marker in a freshly opened print window. */
export async function waitForPrintReady(target: Window): Promise<void> {
  const deadline = Date.now() + RESOURCE_TIMEOUT + 1000;
  while (Date.now() < deadline) {
    if (target.closed) throw new Error("the print window was closed");
    if (target.document.documentElement?.dataset.analogExportReady === "true") return;
    await new Promise<void>((resolve) => setTimeout(resolve, 50));
  }
  throw new Error("export resources did not finish loading");
}

function withTimeout<T>(promise: Promise<T>): Promise<T | undefined> {
  return Promise.race([
    promise,
    new Promise<undefined>((resolve) => setTimeout(() => resolve(undefined), RESOURCE_TIMEOUT)),
  ]).catch(() => undefined);
}

function waitForLoad(element: Element): Promise<void> {
  return new Promise((resolve) => {
    let done = false;
    let timer: number | undefined;
    const finish = () => {
      if (done) return;
      done = true;
      if (timer !== undefined) window.clearTimeout(timer);
      element.removeEventListener("load", finish);
      element.removeEventListener("error", finish);
      resolve();
    };
    element.addEventListener("load", finish, { once: true });
    element.addEventListener("error", finish, { once: true });
    timer = window.setTimeout(finish, RESOURCE_TIMEOUT);
  });
}
