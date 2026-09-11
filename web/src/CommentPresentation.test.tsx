// @vitest-environment jsdom

import { act, type ComponentProps, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { AnnotationPanel } from "./Annotations";
import type { Annotation } from "./api";
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
  height: 240,
  text: "card text",
  sp_kind: "plain" as const,
};

const humanOpen: Annotation = {
  id: "a_human",
  card_id: node.id,
  card_title: "One",
  card_rev: 1,
  selector: { type: "point", x: 0.2, y: 0.3 },
  body: "human note",
  motivation: "editing",
  creator: "kai",
  creator_kind: "human",
  resolved: false,
  resolved_reply: null,
  stale: false,
  created_at: "2026-09-10T12:00:00.000Z",
};

const agentResolved: Annotation = {
  ...humanOpen,
  id: "a_agent",
  selector: { type: "point", x: 0.7, y: 0.8 },
  body: "agent note",
  creator: "codex",
  creator_kind: "agent",
  resolved: true,
  resolved_reply: "addressed in the latest revision",
  created_at: "2026-09-10T12:01:00.000Z",
};

const canvasProps: ComponentProps<typeof Canvas> = {
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

function render(element: ReactNode): HTMLDivElement {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root!.render(element));
  return host;
}

function openThread(container: Element): void {
  const button = container.querySelector<HTMLButtonElement>(
    'button[title="Show the comment thread on this card"]',
  );
  expect(button).not.toBeNull();
  act(() => button!.click());
}

describe("card comment history", () => {
  it("keeps resolved comments in the thread but out of the open badge and overlay", () => {
    const container = render(
      <Canvas {...canvasProps} annotations={[humanOpen, agentResolved]} />,
    );

    expect(container.querySelector(".badge.comments")?.textContent).toBe("1");
    expect(container.querySelectorAll(".annotation-layer .pin")).toHaveLength(1);

    openThread(container);

    expect(container.querySelectorAll(".thread-item")).toHaveLength(2);
    expect(container.querySelector(".thread-item .who.human")?.textContent).toBe("kai");
    expect(container.querySelector(".thread-item .who.agent")?.textContent).toBe("codex");
    expect(container.querySelector(".thread-item.resolved .resolved-marker")?.textContent).toContain("resolved");
    expect(container.querySelector(".thread-item.resolved .reply-bubble")?.textContent)
      .toContain("addressed in the latest revision");
  });

  it("offers history without an open badge or pin when every comment is resolved", () => {
    const container = render(
      <Canvas {...canvasProps} annotations={[agentResolved]} />,
    );

    expect(container.querySelector(".badge.comments")).toBeNull();
    expect(container.querySelector(".annotation-layer .pin")).toBeNull();

    openThread(container);
    expect(container.querySelector(".thread-item.resolved")).not.toBeNull();
  });
});

describe("comment panel authors", () => {
  it("classifies author labels by creator kind", () => {
    const container = render(
      <AnnotationPanel
        annotations={[humanOpen, agentResolved]}
        showResolved
        selectedId={null}
        onToggleResolved={vi.fn()}
        onSelect={vi.fn()}
        onResolve={vi.fn()}
        onReopen={vi.fn()}
      />,
    );

    expect(container.querySelector(".comment-author.human")?.textContent).toBe("kai");
    expect(container.querySelector(".comment-author.agent")?.textContent).toBe("codex");
  });
});
