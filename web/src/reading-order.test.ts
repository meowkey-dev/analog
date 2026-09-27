import { describe, expect, it } from "vitest";
import type { AnalogEvent, Annotation, Edge, Node } from "./api";
import { boardOrder, endOfBoard, neighbors, openOrder, readingOrder, recentOrder } from "./reading-order";

const at = (id: string, x: number, y: number, height = 200): Node => ({
  id, type: "text", x, y, width: 300, height, text: id,
});

const ids = (nodes: Node[]) => nodes.map((n) => n.id);

describe("reading order", () => {
  it("reads a board in rows, tolerating ragged tops", () => {
    const nodes = [at("c", 720, 10), at("a", 0, 0), at("d", 0, 260), at("b", 360, 40), at("e", 400, 250)];
    expect(ids(boardOrder(nodes))).toEqual(["a", "b", "c", "d", "e"]);
  });

  it("starts a new row once a card is below the first card's middle", () => {
    const nodes = [at("a", 0, 0, 100), at("b", 400, 60, 100)];
    expect(ids(boardOrder(nodes))).toEqual(["a", "b"]);
    expect(ids(boardOrder([at("a", 400, 0, 100), at("b", 0, 60, 100)]))).toEqual(["a", "b"]);
  });

  it("puts the most recently touched cards first, ignoring moves", () => {
    const nodes = [at("a", 0, 0), at("b", 360, 0), at("c", 720, 0)];
    const events = [
      { seq: 1, type: "card.updated", subject_id: "a" },
      { seq: 2, type: "annotation.created", subject_id: "a_1", payload: { card_id: "c" } },
      { seq: 3, type: "card.moved", subject_id: "b" },
    ] as AnalogEvent[];
    expect(ids(recentOrder(nodes, events))).toEqual(["c", "a", "b"]);
  });

  it("puts cards with the most open comments first", () => {
    const nodes = [at("a", 0, 0), at("b", 360, 0), at("c", 720, 0)];
    const annotations = [
      { card_id: "c", resolved: false }, { card_id: "c", resolved: false },
      { card_id: "b", resolved: false }, { card_id: "a", resolved: true },
    ] as Annotation[];
    expect(ids(openOrder(nodes, annotations))).toEqual(["c", "b", "a"]);
  });

  it("walks only changed cards for what's new", () => {
    const nodes = [at("a", 0, 0), at("b", 360, 0), at("c", 720, 0)];
    expect(ids(readingOrder("changes", nodes, [], [], new Set(["c", "a"])))).toEqual(["a", "c"]);
  });

  it("lists linked cards, outgoing first", () => {
    const nodes = [at("a", 0, 0), at("b", 360, 0), at("c", 720, 0)];
    const byId = new Map(nodes.map((n) => [n.id, n]));
    const edges = [
      { id: "l_1", fromNode: "c", toNode: "a", label: "cites" },
      { id: "l_2", fromNode: "a", toNode: "b", label: "depends on" },
      { id: "l_3", fromNode: "a", toNode: "gone", label: "dangling" },
    ] as Edge[];
    expect(neighbors("a", edges, byId).map((n) => [n.direction, n.node.id]))
      .toEqual([["out", "b"], ["in", "c"]]);
  });

  it("places a new card under everything, at the left", () => {
    expect(endOfBoard([at("a", 40, 0), at("b", 400, 100, 300)])).toEqual({ x: 40, y: 440 });
    expect(endOfBoard([])).toEqual({ x: 0, y: 0 });
  });
});
