package portable

// a snapshot of the board chrome so a saved HTML file still looks like analog
// without fetching styles.css. themes and katex ride along only in the UI
// export, which serialises the live document.
const exportCSS = `:root {
  --bg: #12141a;
  --grid: #1b1e26;
  --panel: #171a21;
  --line: #272b36;
  --card: #1d212a;
  --card-head: #232833;
  --ink: #dfe3ec;
  --dim: #8b93a7;
  --accent: #6ea8fe;
  --warn: #e0a35a;
  font-synthesis: none;
}
* { box-sizing: border-box; }
html, body { margin: 0; background: var(--bg); color: var(--ink);
  font: 13px/1.5 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif; }
body { min-height: 100vh; }
.export-head {
  display: flex; align-items: baseline; gap: 10px;
  padding: 10px 16px; border-bottom: 1px solid var(--line); background: var(--panel);
  height: 44px;
}
.export-head .brand { font-weight: 650; letter-spacing: .04em; color: var(--accent); }
.export-head .title { font-weight: 550; }
.export-head .slug { color: var(--dim); }
.empty { color: var(--dim); padding: 48px 16px; }
.board {
  position: relative;
  background-color: var(--bg);
  background-image:
    linear-gradient(var(--grid) 1px, transparent 1px),
    linear-gradient(90deg, var(--grid) 1px, transparent 1px);
  background-size: 40px 40px;
}
.world { position: absolute; left: 0; top: 0; }
.card {
  position: absolute; display: flex; flex-direction: column;
  background: var(--card); border: 1px solid var(--line); border-radius: 10px;
  overflow: hidden; box-shadow: 0 6px 18px rgba(0,0,0,.3);
}
.card.superseded { opacity: .82; border-style: dashed; }
.card.deleted { opacity: .45; }
.card-head {
  display: flex; align-items: center; gap: 6px;
  padding: 5px 10px; background: var(--card-head);
  border-bottom: 1px solid var(--line); flex: none;
}
.card-title { font-weight: 600; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.card-kind { font-size: 10px; color: var(--dim); text-transform: uppercase; letter-spacing: .06em; }
.card-body { flex: 1; overflow: auto; padding: 10px 12px; min-height: 0; }
.card-body.plain { margin: 0; white-space: pre-wrap; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.card-body.svg { display: grid; place-items: center; }
.card-body.svg svg { max-width: 100%; max-height: 100%; }
.card-body.file { display: grid; place-items: center; padding: 6px; }
.card-body.file img, .card-body.file embed { max-width: 100%; max-height: 100%; object-fit: contain; }
.card-body.file.muted { color: var(--dim); }
.card-body.html { border: none; width: 100%; background: #fff; padding: 0; }
.card-body.md h1, .card-body.md h2, .card-body.md h3 { margin: .2em 0 .4em; font-size: 1.15em; }
.card-body.md p { margin: .4em 0; }
.card-body.md ul, .card-body.md ol { margin: .4em 0; padding-left: 1.2em; }
.card-body.md code { background: #12151c; padding: 1px 4px; border-radius: 4px; }
.card-body.md pre { background: #12151c; padding: 8px; border-radius: 6px; overflow: auto; }
.card-foot {
  display: flex; justify-content: space-between; flex: none;
  font-size: 10px; color: var(--dim);
  padding: 3px 10px; border-top: 1px solid var(--line);
}
.links { position: absolute; overflow: visible; pointer-events: none; }
.links .edge-line { fill: none; stroke: var(--edge-color, #4c566a); stroke-width: 1.75; }
.links marker path { fill: #4c566a; }
.links .edge.dangling .edge-line { stroke: var(--edge-color, #3a4152); stroke-dasharray: 4 4; opacity: .6; }
.links .edge-label {
  fill: var(--edge-color, var(--dim)); font-size: 11px; paint-order: stroke;
  stroke: var(--bg); stroke-width: 4px; stroke-linejoin: round;
  font-family: ui-sans-serif, system-ui, sans-serif;
}
* { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
`
