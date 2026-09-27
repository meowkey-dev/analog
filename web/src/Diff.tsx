import { useMemo } from "react";

/**
 * Unified line diff between two revisions of a card (#6). A small LCS over
 * lines — card content is prose and code, so line granularity is what a human
 * wants to scan; nothing here needs to be a real diff algorithm.
 */

type Row = { kind: "same" | "add" | "del"; text: string };

export function diffLines(before: string, after: string): Row[] {
  const a = before.split("\n");
  const b = after.split("\n");
  const n = a.length;
  const m = b.length;
  const lcs: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i]![j] = a[i] === b[j] ? lcs[i + 1]![j + 1]! + 1 : Math.max(lcs[i + 1]![j]!, lcs[i]![j + 1]!);
    }
  }
  const rows: Row[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      rows.push({ kind: "same", text: a[i]! });
      i += 1;
      j += 1;
    } else if (lcs[i + 1]![j]! >= lcs[i]![j + 1]!) {
      rows.push({ kind: "del", text: a[i]! });
      i += 1;
    } else {
      rows.push({ kind: "add", text: b[j]! });
      j += 1;
    }
  }
  while (i < n) rows.push({ kind: "del", text: a[i++]! });
  while (j < m) rows.push({ kind: "add", text: b[j++]! });
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
