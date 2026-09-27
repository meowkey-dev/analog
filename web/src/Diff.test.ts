import { describe, expect, it } from "vitest";
import { diffLines, MAX_DIFF_CELLS } from "./Diff";

const kinds = (rows: { kind: string; text: string }[]) => rows.map((r) => `${r.kind[0]}${r.text}`);

describe("diffLines", () => {
  it("diffs by line, keeping the shared head and tail", () => {
    expect(kinds(diffLines("a\nb\nc\nd", "a\nB\nc\nd\ne"))).toEqual(["sa", "db", "aB", "sc", "sd", "ae"]);
  });

  it("finds moved-around lines in the differing middle", () => {
    expect(kinds(diffLines("x\n1\n2\n3\ny", "x\n2\n3\n4\ny"))).toEqual(["sx", "d1", "s2", "s3", "a4", "sy"]);
  });

  it("reports identical text as all same", () => {
    expect(diffLines("a\nb", "a\nb").every((r) => r.kind === "same")).toBe(true);
  });

  it("stays bounded on a huge edit instead of building the full table", () => {
    // 20k short lines each side, all different: 400M cells unbounded.
    const before = Array.from({ length: 20_000 }, (_, i) => `a${i}`).join("\n");
    const after = Array.from({ length: 20_000 }, (_, i) => `b${i}`).join("\n");
    expect(20_001 * 20_001).toBeGreaterThan(MAX_DIFF_CELLS);
    const started = performance.now();
    const rows = diffLines(`head\n${before}\ntail`, `head\n${after}\ntail`);
    expect(performance.now() - started).toBeLessThan(1000);
    expect(rows).toHaveLength(40_002);
    expect(rows[0]).toEqual({ kind: "same", text: "head" });
    expect(rows[1]).toEqual({ kind: "del", text: "a0" });
    expect(rows[20_001]).toEqual({ kind: "add", text: "b0" });
    expect(rows[40_001]).toEqual({ kind: "same", text: "tail" });
  });

  it("still diffs precisely when a small edit sits in a huge card", () => {
    const lines = Array.from({ length: 20_000 }, (_, i) => `line ${i}`);
    const edited = [...lines];
    edited[10_000] = "changed";
    const rows = diffLines(lines.join("\n"), edited.join("\n"));
    expect(rows.filter((r) => r.kind !== "same")).toEqual([
      { kind: "del", text: "line 10000" },
      { kind: "add", text: "changed" },
    ]);
  });
});
