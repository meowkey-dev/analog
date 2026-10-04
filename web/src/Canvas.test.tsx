// @vitest-environment jsdom

import { act, type ComponentProps } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { Canvas } from "./Canvas";

// Counts markdown parses: the cost a needless card re-render pays (#110).
const markdown = vi.hoisted(() => ({ renders: 0 }));
vi.mock("react-markdown", () => ({
  default: ({ children }: { children: string }) => {
    markdown.renders += 1;
    return <div>{children}</div>;
  },
}));

let resized: (() => void) | null = null;

class ResizeObserverStub {
  constructor(callback: () => void) {
    resized = callback;
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}

beforeAll(() => {
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean })
    .IS_REACT_ACT_ENVIRONMENT = true;
});

const node = {
  id: "c_one",
  type: "text" as const,
  x: 0,
  y: 0,
  width: 320,
  height: 200,
  text: "selectable card text",
  sp_kind: "plain" as const,
};

const props: ComponentProps<typeof Canvas> = {
  nodes: [node],
  allNodes: [node],
  edges: [],
  annotations: [],
  annotateMode: false,
  draft: null,
  selectedCard: null,
  selectedEdge: null,
  selectedAnnotation: null,
  focus: null,
  onSelectCard: vi.fn(),
  onSelectEdge: vi.fn(),
  onSelectAnnotation: vi.fn(),
  onDraft: vi.fn(),
  onMoveCard: vi.fn(),
  onResizeCard: vi.fn(),
  onEditCard: vi.fn(),
  onDeleteCard: vi.fn(),
  onCreateLink: vi.fn(),
  onDeleteLink: vi.fn(),
  onPopOut: vi.fn(),
  onCreateCardAt: vi.fn(),
  onCreateFileCards: vi.fn(),
};

let host: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

afterEach(() => {
  if (root) act(() => root!.unmount());
  host?.remove();
  root = null;
  host = null;
});

function renderCanvas(overrides: Partial<ComponentProps<typeof Canvas>> = {}): HTMLDivElement {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root!.render(<Canvas {...props} {...overrides} />));
  return host;
}

function pointerDown(target: Element): MouseEvent {
  const event = new MouseEvent("pointerdown", {
    bubbles: true,
    cancelable: true,
    button: 0,
    clientX: 10,
    clientY: 10,
  });
  act(() => target.dispatchEvent(event));
  return event;
}

describe("Canvas drag gestures", () => {
  it.each([
    ["board pan", ".canvas"],
    ["card move", ".card-head"],
    ["card resize", ".handle.resize.se"],
  ])("cancels native selection before a %s", (_name, selector) => {
    const target = renderCanvas().querySelector(selector);

    expect(target).not.toBeNull();
    expect(pointerDown(target!).defaultPrevented).toBe(true);
  });

  it.each([
    ["card content", ".card-body"],
    ["canvas controls", ".zoom button"],
    ["card header controls", ".card-head button"],
  ])("preserves native pointer behavior on %s", (_name, selector) => {
    const target = renderCanvas().querySelector(selector);

    expect(target).not.toBeNull();
    expect(pointerDown(target!).defaultPrevented).toBe(false);
  });
});

describe("in-card search", () => {
  it("opens from the card header and navigates rendered matches", () => {
    const searchable = { ...node, text: "first card and second card" };
    const container = renderCanvas({
      nodes: [searchable],
      allNodes: [searchable],
      selectedCard: node.id,
    });
    const button = container.querySelector<HTMLButtonElement>(
      'button[title="Search this card (⌘/Ctrl-F)"]',
    );
    expect(button).not.toBeNull();
    act(() => button!.click());

    const input = container.querySelector<HTMLInputElement>('.card-search input');
    expect(input).not.toBeNull();
    act(() => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
      setter?.call(input, "card");
      input!.dispatchEvent(new Event("input", { bubbles: true }));
    });

    expect(container.querySelector(".card-search-count")?.textContent).toBe("1/2");
    expect(document.getSelection()?.toString()).toBe("card");

    const next = container.querySelector<HTMLButtonElement>('button[title="Next match"]');
    act(() => next!.click());
    expect(container.querySelector(".card-search-count")?.textContent).toBe("2/2");
    expect(document.getSelection()?.toString()).toBe("card");
  });

  it("opens on the selected card with the native find shortcut", () => {
    const container = renderCanvas({ selectedCard: node.id });
    const event = new KeyboardEvent("keydown", {
      bubbles: true,
      cancelable: true,
      ctrlKey: true,
      key: "f",
    });

    act(() => window.dispatchEvent(event));

    expect(event.defaultPrevented).toBe(true);
    expect(container.querySelector('.card-search input')).not.toBeNull();
  });
});

describe("canvas resize", () => {
  function setSize(element: HTMLElement, width: number, height: number) {
    Object.defineProperty(element, "clientWidth", { configurable: true, value: width });
    Object.defineProperty(element, "clientHeight", { configurable: true, value: height });
  }

  it("ignores the side panel but follows a page resize", () => {
    const container = renderCanvas();
    const canvas = container.querySelector<HTMLElement>(".canvas")!;
    const world = container.querySelector<HTMLElement>(".viewport")!;
    setSize(container, 1000, 600);
    const initial = world.style.transform;

    setSize(canvas, 1000, 600);
    act(() => resized!());
    expect(world.style.transform).toBe(initial);

    setSize(canvas, 660, 600);
    act(() => resized!());
    expect(world.style.transform).toBe(initial);

    setSize(canvas, 1000, 600);
    act(() => resized!());
    expect(world.style.transform).toBe(initial);

    setSize(container, 1200, 600);
    setSize(canvas, 1200, 600);
    act(() => resized!());
    expect(world.style.transform).toBe("translate(180px, 80px) scale(1)");
  });

  it("centres a focused card in the page while the panel is open", () => {
    const container = renderCanvas();
    const canvas = container.querySelector<HTMLElement>(".canvas")!;
    setSize(container, 1000, 600);
    setSize(canvas, 660, 600);
    Object.defineProperty(canvas, "getBoundingClientRect", {
      value: () => ({ width: 660, height: 600 }),
    });

    act(() => root!.render(<Canvas {...props} focus={{ id: node.id, nonce: 1 }} />));

    expect(container.querySelector<HTMLElement>(".viewport")!.style.transform)
      .toBe("translate(340px, 200px) scale(1)");
  });

  it("fits content against the full page width", () => {
    const container = renderCanvas();
    const canvas = container.querySelector<HTMLElement>(".canvas")!;
    setSize(container, 1000, 600);
    setSize(canvas, 660, 600);
    Object.defineProperty(canvas, "getBoundingClientRect", {
      value: () => ({ width: 660, height: 600 }),
    });

    act(() => container.querySelector<HTMLButtonElement>('button[title="Fit to content"]')!.click());

    expect(container.querySelector<HTMLElement>(".viewport")!.style.transform)
      .toBe("translate(116px, 60px) scale(2.4)");
  });
});

describe("re-render cost (#110)", () => {
  const cards = Array.from({ length: 20 }, (_, i) => ({
    ...node, id: `c_${i}`, x: i * 400, sp_kind: "md" as const, text: `card ${i}`,
  }));

  it("does not re-render cards while the board pans", () => {
    const container = renderCanvas({ nodes: cards, allNodes: cards });
    const canvas = container.querySelector<HTMLElement>(".canvas")!;
    const before = markdown.renders;

    for (let i = 0; i < 5; i++) {
      act(() => canvas.dispatchEvent(new WheelEvent("wheel", { bubbles: true, deltaX: 10, deltaY: 10 })));
    }

    expect(container.querySelector<HTMLElement>(".viewport")!.style.transform)
      .toBe("translate(30px, 30px) scale(1)");
    expect(markdown.renders).toBe(before);
  });

  it("re-renders only the cards a selection touches", () => {
    const container = renderCanvas({ nodes: cards, allNodes: cards });
    const before = markdown.renders;

    act(() => root!.render(<Canvas {...props} nodes={cards} allNodes={cards} selectedCard="c_3" />));
    // Selecting changes the card chrome, not its body: nothing is re-parsed.
    expect(markdown.renders).toBe(before);
    expect(container.querySelector('[data-card-id="c_3"]')!.classList).toContain("selected");
  });

  it("re-parses only the card whose text changed", () => {
    renderCanvas({ nodes: cards, allNodes: cards });
    const before = markdown.renders;
    const edited = cards.map((c) => (c.id === "c_5" ? { ...c, text: "edited" } : c));

    act(() => root!.render(<Canvas {...props} nodes={edited} allNodes={edited} />));
    expect(markdown.renders).toBe(before + 1);
  });
});

describe("revision depth", () => {
  const rev = (id: string, next?: string) => ({
    ...node, id, sp_superseded_by: next, x: 0,
  });

  function badges(nodes: ReturnType<typeof rev>[]) {
    const container = renderCanvas({ nodes, allNodes: nodes });
    return Object.fromEntries(nodes.map((n) => [
      n.id,
      container.querySelector(`[data-card-id="${n.id}"] .badge`)?.textContent ?? null,
    ]));
  }

  it("counts the revisions a superseded card has been through, whatever order they load in", () => {
    // Out of order, so a later card's depth is reused from one computed earlier.
    const chain = [rev("c_b", "c_c"), rev("c_a", "c_b"), rev("c_c", "c_d"), rev("c_d")];
    expect(badges(chain)).toEqual({ c_a: "rev 4", c_b: "rev 3", c_c: "rev 2", c_d: null });
  });

  it("counts a successor that is off the board, and ends on a cycle", () => {
    expect(badges([rev("c_a", "c_gone")])).toEqual({ c_a: "rev 2" });
    const cycle = badges([rev("c_a", "c_b"), rev("c_b", "c_a")]);
    expect(cycle.c_a).toMatch(/^rev \d+$/);
    expect(cycle.c_b).toMatch(/^rev \d+$/);
  });

  // Rendering the chain in jsdom is slow; the assertion counts lookups, not time.
  it("is linear in the length of a chain", { timeout: 30_000 }, () => {
    const length = 1000;
    const long = Array.from({ length }, (_, i) => rev(`c_${i}`, i < length - 1 ? `c_${i + 1}` : undefined));
    const byId = new Map(long.map((n) => [n.id, n]));
    let lookups = 0;
    const get = Map.prototype.get;
    const spy = vi.spyOn(Map.prototype, "get").mockImplementation(function (this: Map<unknown, unknown>, key) {
      if (this.size === byId.size && typeof key === "string" && key.startsWith("c_")) lookups += 1;
      return get.call(this, key);
    });
    try {
      renderCanvas({ nodes: long, allNodes: long });
    } finally {
      spy.mockRestore();
    }
    // Quadratic would be ~500k successor lookups; a few per card is linear.
    expect(lookups).toBeLessThan(long.length * 20);
  });
});

describe("the text editor", () => {
  // Blur used to be the only way to save; the bar makes it explicit.
  function openEditor() {
    const onEditCard = vi.fn();
    const container = renderCanvas({ onEditCard });
    const card = container.querySelector(`[data-card-id="${node.id}"]`)!;
    act(() => card.dispatchEvent(new MouseEvent("dblclick", { bubbles: true })));
    container.querySelector<HTMLTextAreaElement>("textarea.editor")!.value = "edited by hand";
    const button = (label: string) =>
      [...container.querySelectorAll<HTMLButtonElement>(".text-editor-bar button")]
        .find((b) => b.textContent === label)!;
    return { container, onEditCard, button };
  }

  it("saves from the save button", () => {
    const { container, onEditCard, button } = openEditor();
    act(() => button("save").click());
    expect(onEditCard).toHaveBeenCalledWith(node.id, "edited by hand");
    expect(container.querySelector("textarea.editor")).toBeNull();
  });

  it("discards from the cancel button without blurring the textarea into a save", () => {
    const { container, onEditCard, button } = openEditor();
    const down = new MouseEvent("mousedown", { bubbles: true, cancelable: true });
    act(() => button("cancel").dispatchEvent(down));
    expect(down.defaultPrevented).toBe(true);
    act(() => button("cancel").click());
    expect(onEditCard).not.toHaveBeenCalled();
    expect(container.querySelector("textarea.editor")).toBeNull();
  });
});
