// @vitest-environment jsdom

import { act, type ComponentProps } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { Canvas } from "./Canvas";

class ResizeObserverStub {
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

function renderCanvas(): HTMLDivElement {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root!.render(<Canvas {...props} />));
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
