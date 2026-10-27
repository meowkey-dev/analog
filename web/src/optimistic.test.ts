import { describe, expect, it } from "vitest";
import type { Annotation } from "./api";
import { PENDING_PREFIX, confirmed, isPending, withPending } from "./optimistic";

const open: Annotation = {
  id: "a_1", card_id: "c_1", card_rev: 1, selector: null, body: "tighten this",
  motivation: "editing", creator: "kai", creator_kind: "human",
  resolved: false, resolved_reply: null, stale: false, created_at: "2026-10-27T00:00:00.000Z",
};

describe("optimistic comment writes (#127)", () => {
  it("shows a resolve with its reply before the server answers", () => {
    const [shown] = withPending([open], [{ op: "resolve", id: "a_1", reply: "done" }]);
    expect(shown).toMatchObject({ resolved: true, resolved_reply: "done" });
  });

  it("clears the reply on reopen, as the server does", () => {
    const resolved = { ...open, resolved: true, resolved_reply: "done" };
    const [shown] = withPending([resolved], [{ op: "reopen", id: "a_1" }]);
    expect(shown).toMatchObject({ resolved: false, resolved_reply: null });
  });

  it("appends a new comment, marked pending until the server names it", () => {
    const draft = { ...open, id: `${PENDING_PREFIX}1` };
    const shown = withPending([open], [{ op: "create", annotation: draft }]);
    expect(shown.map((a) => a.id)).toEqual(["a_1", `${PENDING_PREFIX}1`]);
    expect(isPending(shown[1]!)).toBe(true);
    expect(isPending(shown[0]!)).toBe(false);
  });

  it("is the server list itself with nothing pending, and rolls back by dropping the write", () => {
    const list = [open];
    expect(withPending(list, [])).toBe(list);
  });

  it("is idempotent over a list that already reflects the write", () => {
    const resolved = { ...open, resolved: true, resolved_reply: "done" };
    expect(withPending([resolved], [{ op: "resolve", id: "a_1", reply: "done" }])).toEqual([resolved]);
  });

  it("merges the server's copy in place, or adds it", () => {
    const server = { ...open, resolved: true, resolved_reply: "ok" };
    expect(confirmed([open], server)).toEqual([server]);
    const created = { ...open, id: "a_2" };
    expect(confirmed([open], created).map((a) => a.id)).toEqual(["a_1", "a_2"]);
  });
});
