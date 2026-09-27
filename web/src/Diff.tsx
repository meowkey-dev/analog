import { useMemo } from "react";

/**
 * Unified line diff between two revisions of a card (#6). A small LCS over
 * lines — card content is prose and code, so line granularity is what a human
 * wants to scan; nothing here needs to be a real diff algorithm.
 */

type Row = { kind: "same" | "add" | "del"; text: string };

/**
 * The LCS table is (old lines × new lines). A card may keep 60 KB of text for a
 * diff (seen.ts), which can be tens of thousands of short lines: past this many
 * cells the middle is shown as replaced wholesale rather than freezing the tab.
 * 4M cells is 16 MB of Uint32Array.
 */
export const MAX_DIFF_CELLS = 4_000_000;

export function diffLines(before: string, after: string): Row[] {
  const a = before.split("\n");
  const b = after.split("\n");
  // Edits are usually local: peel off the shared head and tail first, so the
  // table only spans the part that actually differs.
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) head += 1;
  let tail = 0;
  while (tail < a.length - head && tail < b.length - head &&
         a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail += 1;

  const rows: Row[] = a.slice(0, head).map((text) => ({ kind: "same" as const, text }));
  const x = a.slice(head, a.length - tail);
  const y = b.slice(head, b.length - tail);
  const n = x.length;
  const m = y.length;

  if ((n + 1) * (m + 1) > MAX_DIFF_CELLS) {
    for (const text of x) rows.push({ kind: "del", text });
    for (const text of y) rows.push({ kind: "add", text });
  } else {
    const w = m + 1;
    const lcs = new Uint32Array((n + 1) * w);
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        lcs[i * w + j] = x[i] === y[j]
          ? lcs[(i + 1) * w + j + 1]! + 1
          : Math.max(lcs[(i + 1) * w + j]!, lcs[i * w + j + 1]!);
      }
    }
    let i = 0;
    let j = 0;
    while (i < n && j < m) {
      if (x[i] === y[j]) {
        rows.push({ kind: "same", text: x[i]! });
        i += 1;
        j += 1;
      } else if (lcs[(i + 1) * w + j]! >= lcs[i * w + j + 1]!) {
        rows.push({ kind: "del", text: x[i]! });
        i += 1;
      } else {
        rows.push({ kind: "add", text: y[j]! });
        j += 1;
      }
    }
    while (i < n) rows.push({ kind: "del", text: x[i++]! });
    while (j < m) rows.push({ kind: "add", text: y[j++]! });
  }
  for (const text of a.slice(a.length - tail)) rows.push({ kind: "same", text });
  return rows;
}

const CONTEXT = 2;

export interface DiffSource {
  before: string;
  after: string;
  /** What the two sides are, for the toggle's tooltip and the banner. */
  label: string;
  /** Shown when the text matches — the change was the title or file. */
  same: string;
}

/**
 * What a card can be diffed against, best first: the revision that replaced it
 * (branch mode, #6), the text this browser last saw (replace mode keeps no
 * history server-side, see seen.ts), or the last change this browser saw.
 */
export function diffFor(
  node: { text?: string },
  successor: { text?: string } | undefined,
  seenBefore: string | undefined,
  lastBefore: string | undefined,
): DiffSource | null {
  if (successor) {
    if (successor.text === undefined || node.text === undefined) return null;
    return { before: node.text, after: successor.text,
             label: "what the next revision changed", same: "Text is identical to the next revision." };
  }
  const after = node.text ?? "";
  if (seenBefore !== undefined) {
    return { before: seenBefore, after,
             label: "changes since you last looked", same: "The text is unchanged; the title or file changed." };
  }
  if (lastBefore !== undefined) {
    return { before: lastBefore, after,
             label: "the last change this browser saw", same: "The text is unchanged; the title or file changed." };
  }
  return null;
}

export function DiffView({ before, after, same }: { before: string; after: string; same?: string }) {
  const rows = useMemo(() => diffLines(before, after), [before, after]);
  const changed = rows.some((row) => row.kind !== "same");
  if (!changed) {
    return <div className="diff empty">{same ?? "Text is identical to the next revision."}</div>;
  }

  // Collapse long unchanged runs to a couple of lines of context.
  const show = new Array<boolean>(rows.length).fill(false);
  rows.forEach((row, index) => {
    if (row.kind === "same") return;
    for (let k = Math.max(0, index - CONTEXT); k <= Math.min(rows.length - 1, index + CONTEXT); k++) {
      show[k] = true;
    }
  });

  const rendered: React.ReactNode[] = [];
  let skipping = 0;
  rows.forEach((row, index) => {
    if (!show[index]) {
      skipping += 1;
      if (skipping === 1) {
        rendered.push(
          <div key={`skip-${index}`} className="diff-row skip">⋯</div>,
        );
      }
      return;
    }
    skipping = 0;
    rendered.push(
      <div key={index} className={`diff-row ${row.kind}`}>
        <span className="sign">{row.kind === "add" ? "+" : row.kind === "del" ? "−" : " "}</span>
        <span className="text">{row.text || " "}</span>
      </div>,
    );
  });

  return <div className="diff">{rendered}</div>;
}
