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

  it("keeps the board centred when a side panel opens and closes (#104)", () => {
    const container = renderCanvas();
    const canvas = container.querySelector<HTMLElement>(".canvas")!;
    const world = container.querySelector<HTMLElement>(".viewport")!;
    const initial = world.style.transform;

    setSize(canvas, 1000, 600);
    act(() => resized!());
    expect(world.style.transform).toBe(initial);

    setSize(canvas, 660, 600);
    act(() => resized!());
    expect(world.style.transform).toBe("translate(-90px, 80px) scale(1)");

    setSize(canvas, 1000, 600);
    act(() => resized!());
    expect(world.style.transform).toBe(initial);
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
