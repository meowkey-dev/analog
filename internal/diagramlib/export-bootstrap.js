(() => {
  // Each diagram becomes an <img> of its SVG. The exported file is opened
  // outside any sandbox and has no sanitizer; an image cannot run script or
  // reach the page whatever the card text said.
  const css = getComputedStyle(document.documentElement);
  const v = (name, fallback) => css.getPropertyValue(name).trim() || fallback;
  // An image cannot see the page's CSS variables, so the palette is baked in.
  const palette = {
    "--pele-bg": v("--card", "#1d212a"),
    "--pele-fg": v("--ink", "#dfe3ec"),
    "--pele-muted": v("--dim", "#8b93a7"),
    "--pele-line": v("--dim", "#8b93a7"),
    "--pele-surface": "#12151c",
    "--pele-surface-alt": v("--line", "#272b36"),
    "--pele-border": v("--dim", "#8b93a7"),
    "--pele-accent": v("--accent", "#6ea8fe"),
  };
  const vars = Object.entries(palette).map(([name, value]) => `${name}:${value};`).join("");
  const fontFamily = getComputedStyle(document.body).fontFamily || undefined;
  const describe = (err) => {
    const message = String(err && err.message || err);
    const lines = message.split("\n").map((l) => l.trim()).filter((l) => l && l !== "^");
    const reason = lines[lines.length - 1] || message;
    return err && err.line > 0 && !/\bline \d/i.test(reason) ? `line ${err.line}: ${reason}` : reason;
  };
  let n = 0;
  for (const code of document.querySelectorAll(".card-body.md pre > code.language-mermaid")) {
    const pre = code.parentElement;
    const source = code.textContent.replace(/\n$/, "");
    try {
      const out = pele.render(source, { idPrefix: `mmd${n++}-`, links: false, fontFamily });
      const svg = new DOMParser().parseFromString(out.svg, "image/svg+xml").documentElement;
      svg.setAttribute("style", vars + (svg.getAttribute("style") || ""));
      const img = document.createElement("img");
      img.className = "mermaid";
      img.alt = source;
      img.width = out.width;
      img.height = out.height;
      img.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(new XMLSerializer().serializeToString(svg));
      pre.replaceWith(img);
    } catch (err) {
      const note = document.createElement("div");
      note.className = "mermaid-error";
      note.textContent = describe(err);
      pre.after(note);
    }
  }
})();
