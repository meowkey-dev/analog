import { describe, expect, it } from "vitest";
import type { AnalogEvent, Node } from "./api";
import {
  baseline, changesSince, describeChange, fingerprint, lastChange, markAllSeen, markSeen, MAX_KEPT,
} from "./seen";

const card = (id: string, text: string, title = id): Node => ({
  id, type: "text", x: 0, y: 0, width: 100, height: 100, text, sp_title: title, sp_kind: "md",
});

const event = (seq: number, type: AnalogEvent["type"], subject: string, actor = "claude-code",
  payload?: Record<string, unknown>): AnalogEvent => ({
  seq, ts: "2026-09-27T00:00:00Z", type, subject_id: subject, actor, actor_kind: "agent", payload,
});

describe("seen", () => {
  it("treats the first visit as all seen", () => {
    const nodes = [card("c_a", "one"), card("c_b", "two")];
    const record = baseline(nodes, 5);
    expect(changesSince(record, nodes, [], "kai").size).toBe(0);
  });

  it("reports an edit with the text last seen, and a new card", () => {
    const record = baseline([card("c_a", "one\ntwo")], 5);
    const now = [card("c_a", "one\nthree"), card("c_new", "hi")];
    const events = [event(6, "card.updated", "c_a"), event(7, "card.created", "c_new")];
    const changes = changesSince(record, now, events, "kai");
    expect(changes.get("c_a")).toEqual({ kind: "edited", before: "one\ntwo", newComments: 0, actor: "claude-code" });
    expect(changes.get("c_new")?.kind).toBe("new");
  });

  it("counts a title change as an edit", () => {
    const record = baseline([card("c_a", "same", "Old")], 1);
    expect(changesSince(record, [card("c_a", "same", "New")], [], "kai").get("c_a")?.kind).toBe("edited");
  });

  it("counts comments from others after the card was seen, not your own", () => {
    const nodes = [card("c_a", "x")];
    const record = baseline(nodes, 5);
    const events = [
      event(4, "annotation.created", "a_old", "claude-code", { card_id: "c_a" }),
      event(6, "annotation.created", "a_1", "claude-code", { card_id: "c_a" }),
      event(7, "annotation.created", "a_2", "kai", { card_id: "c_a" }),
    ];
    const change = changesSince(record, nodes, events, "kai").get("c_a");
    expect(change).toEqual({ kind: "comments", newComments: 1 });
    expect(describeChange(change!)).toBe("1 new comment");
  });

  it("acknowledging keeps the previous text as the last change", () => {
    let record = baseline([card("c_a", "v1")], 1);
    const edited = card("c_a", "v2");
    record = markSeen(record, [edited], 2);
    expect(changesSince(record, [edited], [], "kai").size).toBe(0);
    expect(lastChange(record, edited)).toBe("v1");
    // a later edit is news again, and the diff is against what was acknowledged
    const again = card("c_a", "v3");
    expect(changesSince(record, [again], [], "kai").get("c_a")?.before).toBe("v2");
    expect(lastChange(record, again)).toBeUndefined();
  });

  it("re-acknowledging an unchanged card keeps its last change", () => {
    let record = baseline([card("c_a", "v1")], 1);
    record = markSeen(record, [card("c_a", "v2")], 2);
    record = markSeen(record, [card("c_a", "v2")], 3);
    expect(lastChange(record, card("c_a", "v2"))).toBe("v1");
  });

  it("marking all seen drops cards that are gone", () => {
    const record = markAllSeen(baseline([card("c_a", "x"), card("c_gone", "y")], 1), [card("c_a", "x")], 9);
    expect(Object.keys(record.cards)).toEqual(["c_a"]);
    expect(record.seq).toBe(9);
  });

  it("hashes texts too large to keep, so the badge survives without a diff", () => {
    const big = "x".repeat(MAX_KEPT + 1);
    const record = baseline([card("c_a", big)], 1);
    expect(record.cards.c_a!.t).toBeUndefined();
    const change = changesSince(record, [card("c_a", big + "y")], [], "kai").get("c_a");
    expect(change?.kind).toBe("edited");
    expect(change?.before).toBeUndefined();
  });

  it("fingerprints differ on text, title and file", () => {
    const base = card("c_a", "t", "T");
    expect(fingerprint(base)).toBe(fingerprint({ ...base }));
    expect(fingerprint(base)).not.toBe(fingerprint({ ...base, text: "u" }));
    expect(fingerprint(base)).not.toBe(fingerprint({ ...base, sp_title: "U" }));
    expect(fingerprint(base)).not.toBe(fingerprint({ ...base, file: "/media/x.png" }));
    // geometry is not content
    expect(fingerprint(base)).toBe(fingerprint({ ...base, x: 500, width: 10 }));
  });
});
