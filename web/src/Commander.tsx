import { useEffect, useMemo, useRef, useState } from "react";
import type { Node } from "./api";

/**
 * The commander (#12): ⌘K to locate a card without panning around for it.
 * Search is deliberately dumb — substring over title and text, newest last —
 * because the cards are few enough that ranking would be theater.
 */

export function Commander(props: {
  nodes: Node[];
  onClose: () => void;
  onFocusCard: (id: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    input.current?.focus();
  }, []);

  const results = useMemo(() => {
    const q = query.trim().toLowerCase();
    return props.nodes
      .map((node) => {
        const title = (node.sp_title || node.id).toLowerCase();
        const text = (node.text ?? "").toLowerCase();
        const score = q
          ? title.includes(q) ? 0 : text.includes(q) ? 1 : null
          : 0;
        return { node, score };
      })
      .filter((r) => r.score !== null)
      .sort((a, b) => a.score! - b.score!);
  }, [props.nodes, query]);

  useEffect(() => {
    setActive(0);
  }, [query]);

  const pick = (node: Node | undefined) => {
    if (!node) return;
    props.onFocusCard(node.id);
    props.onClose();
  };

  const snippet = (node: Node): string => {
    const text = node.text ?? "";
    const q = query.trim().toLowerCase();
    if (!q) return text.slice(0, 80);
    const at = text.toLowerCase().indexOf(q);
    if (at < 0) return text.slice(0, 80);
    const start = Math.max(0, at - 24);
    return (start > 0 ? "…" : "") + text.slice(start, start + 80) +
      (start + 80 < text.length ? "…" : "");
  };

  return (
    <div className="commander" onClick={props.onClose}>
      <div className="commander-box" onClick={(e) => e.stopPropagation()}>
        <input
          ref={input}
          value={query}
          placeholder="Locate a card…"
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") props.onClose();
            if (e.key === "ArrowDown") { e.preventDefault(); setActive((a) => Math.min(results.length - 1, a + 1)); }
            if (e.key === "ArrowUp") { e.preventDefault(); setActive((a) => Math.max(0, a - 1)); }
            if (e.key === "Enter") { e.preventDefault(); pick(results[active]?.node); }
          }}
        />
        <ul>
          {results.length === 0 && <li className="muted">No cards match.</li>}
          {results.map(({ node }, index) => (
            <li key={node.id}
                className={index === active ? "active" : ""}
                onMouseEnter={() => setActive(index)}
                onClick={() => pick(node)}>
              <span className="title">{node.sp_title || node.id}</span>
              {node.sp_superseded_by && <span className="badge">rev {node.sp_rev ?? 1}</span>}
              <span className="kind">{node.type === "file" ? "file" : node.sp_kind ?? "plain"}</span>
              <span className="snippet">{snippet(node)}</span>
            </li>
          ))}
        </ul>
        <footer>↑↓ to move · enter to jump · esc to close</footer>
      </div>
    </div>
  );
}
