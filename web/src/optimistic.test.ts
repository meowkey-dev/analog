import { describe, expect, it } from "vitest";
import type { Annotation } from "./api";
import { PENDING_PREFIX, confirmed, inOrder, isPending, withPending } from "./optimistic";

const open: Annotation = {
  id: "a_1", card_id: "c_1", card_rev: 1, selector: null, body: "tighten this",
  motivation: "editing", creator: "kai", creator_kind: "human",
  resolved: false, resolved_reply: null, stale: false, created_at: "2026-10-27T00:00:00.000Z",
};

describe("optimistic comment writes (#127)", () => {
  it("shows a resolve with its reply before the server answers", () => {
    const [shown] = withPending([open], [{ slug: "s", op: "resolve", id: "a_1", reply: "done" }], "s");
    expect(shown).toMatchObject({ resolved: true, resolved_reply: "done" });
  });

  it("clears the reply on reopen, as the server does", () => {
    const resolved = { ...open, resolved: true, resolved_reply: "done" };
    const [shown] = withPending([resolved], [{ slug: "s", op: "reopen", id: "a_1" }], "s");
    expect(shown).toMatchObject({ resolved: false, resolved_reply: null });
  });

  it("appends a new comment, marked pending until the server names it", () => {
    const draft = { ...open, id: `${PENDING_PREFIX}1` };
    const shown = withPending([open], [{ slug: "s", op: "create", annotation: draft, known: new Set(["a_1"]) }], "s");
    expect(shown.map((a) => a.id)).toEqual(["a_1", `${PENDING_PREFIX}1`]);
    expect(isPending(shown[1]!)).toBe(true);
    expect(isPending(shown[0]!)).toBe(false);
  });

  it("is the server list itself with nothing pending, and rolls back by dropping the write", () => {
    const list = [open];
    expect(withPending(list, [], "s")).toBe(list);
  });

  it("is idempotent over a list that already reflects the write", () => {
    const resolved = { ...open, resolved: true, resolved_reply: "done" };
    expect(withPending([resolved], [{ slug: "s", op: "resolve", id: "a_1", reply: "done" }], "s")).toEqual([resolved]);
  });

  it("merges the server's copy in place, or adds it", () => {
    const server = { ...open, resolved: true, resolved_reply: "ok" };
    expect(confirmed([open], server)).toEqual([server]);
    const created = { ...open, id: "a_2" };
    expect(confirmed([open], created).map((a) => a.id)).toEqual(["a_1", "a_2"]);
  });
});

describe("optimistic writes under races (#128 review)", () => {
  const draft = { ...open, id: `${PENDING_PREFIX}1`, body: "new words" };

  it("shows a write only in the space it was made in", () => {
    const pending = [{ slug: "redesign", op: "resolve" as const, id: "a_1", reply: null }];
    expect(withPending([open], pending, "other")).toEqual([open]);
    expect(withPending([open], pending, "redesign")[0]!.resolved).toBe(true);
  });

  it("does not list a new comment twice when a refresh sees it before its POST answers", () => {
    const write = { slug: "s", op: "create" as const, annotation: draft, known: new Set(["a_1"]) };
    const listed = { ...draft, id: "a_9" };
    expect(withPending([open, listed], [write], "s").map((a) => a.id)).toEqual(["a_1", "a_9"]);
    // One listed comment stands in for one pending copy, not for an identical second one.
    const again = { ...write, annotation: { ...draft, id: `${PENDING_PREFIX}2` } };
    expect(withPending([open, listed], [write, again], "s")).toHaveLength(3);
    // A comment that was already there before the write is not it.
    expect(withPending([open], [{ ...write, annotation: { ...draft, body: open.body } }], "s")).toHaveLength(2);
  });

  it("sends writes to one comment in order, so a later reopen is not overtaken", async () => {
    const queues = new Map<string, Promise<unknown>>();
    const log: string[] = [];
    let finishResolve!: () => void;
    const resolve = inOrder(queues, "a_1", () => {
      log.push("resolve sent");
      return new Promise<void>((done) => { finishResolve = done; }).then(() => log.push("resolve answered"));
    });
    const reopen = inOrder(queues, "a_1", async () => { log.push("reopen sent"); });
    await Promise.resolve();
    expect(log).toEqual(["resolve sent"]);
    finishResolve();
    await Promise.all([resolve, reopen]);
    expect(log).toEqual(["resolve sent", "resolve answered", "reopen sent"]);
    expect(queues.size).toBe(0);
  });

  it("still sends a later write when an earlier one fails", async () => {
    const queues = new Map<string, Promise<unknown>>();
    const failedWrite = inOrder(queues, "a_1", () => Promise.reject(new Error("boom")));
    const next = inOrder(queues, "a_1", async () => "sent");
    await expect(failedWrite).rejects.toThrow("boom");
    await expect(next).resolves.toBe("sent");
  });
});
