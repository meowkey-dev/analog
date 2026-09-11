// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import { findTextRanges } from "./card-search";

describe("findTextRanges", () => {
  it("finds case-insensitive matches split across rendered markup", () => {
    const root = document.createElement("div");
    root.innerHTML = "First <strong>search</strong> term and SEARCH term";

    const ranges = findTextRanges(root, "search term");

    expect(ranges.map((range) => range.toString())).toEqual(["search term", "SEARCH term"]);
  });

  it("does not search scripts, styles, or accessibility duplicates", () => {
    const root = document.createElement("div");
    root.innerHTML = [
      "<span>visible</span>",
      "<script>visible</script>",
      "<style>.visible { color: red }</style>",
      '<span aria-hidden="true">visible</span>',
    ].join("");

    expect(findTextRanges(root, "visible")).toHaveLength(1);
  });

  it("treats search punctuation literally", () => {
    const root = document.createElement("div");
    root.textContent = "Use a+b, not ab.";

    expect(findTextRanges(root, "a+b").map((range) => range.toString())).toEqual(["a+b"]);
  });
});
