import { useEffect, useId, useMemo, useState, type ComponentProps } from "react";
import type { Components, ExtraProps } from "react-markdown";
import DOMPurify from "dompurify";
import type { RenderOptions } from "pele";

/**
 * ```mermaid fences in md cards render as diagrams through Pele. The card text
 * stays Mermaid source, the way math stays TeX (#68): diff, the editor, agents
 * and an Obsidian export all see text. Pele is loaded on first use and each
 * diagram type is its own chunk, so a board with no diagrams pays nothing.
 */
type Pele = typeof import("pele/lazy");

let pele: Pele | undefined;
let loading: Promise<Pele> | undefined;

function loadPele(): Promise<Pele> {
  loading ??= import("pele/lazy").then((mod) => (pele = mod));
  return loading;
}

function options(idPrefix: string): RenderOptions {
  return {
    idPrefix,
    // Measure with the font the card paints in; Pele's own default is sans-serif.
    fontFamily: getComputedStyle(document.body).fontFamily || undefined,
    // A click on the board selects or drags the card; an anchor inside the
    // diagram would navigate the whole board away instead.
    links: false,
  };
}

// Same profile as svg cards: Pele escapes labels already, but the parent
// document is the boundary and nothing reaches it unsanitized.
const sanitize = (svg: string) => DOMPurify.sanitize(svg, { USE_PROFILES: { svg: true, svgFilters: true } });

type Result = { svg: string } | { error: string };

/** Synchronous once Pele and the diagram's chunk are loaded, so a re-mount does not flash source. */
function renderNow(source: string, idPrefix: string): Result | null {
  if (!pele) return null;
  // detectType parses front matter and throws on a malformed block. This runs
  // during render, where an escaped throw would take the whole board down.
  try {
    const type = pele.detectType(source);
    if (type && !pele.registered(type)) return null;
    return { svg: sanitize(pele.render(source, options(idPrefix)).svg) };
  } catch (err) {
    return { error: describe(err) };
  }
}

// A PeleError message is a parser report: a header, the offending source line and
// a caret. The block already shows the source, so keep the line number and reason.
function describe(err: unknown): string {
  if (!(err instanceof Error)) return String(err);
  const reason = err.message.split("\n").map((l) => l.trim()).filter((l) => l && l !== "^").at(-1) ?? err.message;
  const line = (err as { line?: unknown }).line;
  return typeof line === "number" && line > 0 && !/\bline \d/i.test(reason) ? `line ${line}: ${reason}` : reason;
}

export function MermaidBlock({ source }: { source: string }) {
  // Two diagrams on one board must not share gradient or clip ids.
  const idPrefix = `mmd${useId().replace(/[^a-zA-Z0-9]/g, "")}-`;
  const now = useMemo(() => renderNow(source, idPrefix), [source, idPrefix]);
  const [later, setLater] = useState<{ source: string; result: Result } | null>(null);
  const state = now ?? (later?.source === source ? later.result : null);

  useEffect(() => {
    if (now) return;
    let cancelled = false;
    const settle = (result: Result) => { if (!cancelled) setLater({ source, result }); };
    loadPele()
      .then((mod) => mod.renderAsync(source, options(idPrefix)))
      .then((result) => settle({ svg: sanitize(result.svg) }))
      .catch((err) => settle({ error: describe(err) }));
    return () => { cancelled = true; };
  }, [now, source, idPrefix]);

  if (state && "svg" in state) {
    return <div className="mermaid" dangerouslySetInnerHTML={{ __html: state.svg }} />;
  }
  // Pending and broken diagrams show their source, so the card is never blank;
  // export waits on data-mermaid-pending before it snapshots the board.
  return (
    <div className="mermaid mermaid-source" data-mermaid-pending={state ? undefined : "true"}>
      <pre><code className="language-mermaid">{source}</code></pre>
      {state && <div className="mermaid-error">{state.error}</div>}
    </div>
  );
}

function Pre({ node, children, ...rest }: ComponentProps<"pre"> & ExtraProps) {
  const code = node?.children[0];
  if (code?.type === "element" && code.tagName === "code") {
    const classes = code.properties.className;
    if (Array.isArray(classes) && classes.includes("language-mermaid")) {
      const text = code.children.map((child) => (child.type === "text" ? child.value : "")).join("");
      return <MermaidBlock source={text.replace(/\n$/, "")} />;
    }
  }
  return <pre {...rest}>{children}</pre>;
}

export const mdComponents: Components = { pre: Pre };
