// @vitest-environment jsdom

import { act, type ComponentProps, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { AnnotationComposer } from "./Annotations";
import type { Annotation, Node } from "./api";
import { Canvas } from "./Canvas";

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const box = (left: number, top: number, width: number, height: number) =>
  ({ left, top, width, height, right: left + width, bottom: top + height, x: left, y: top, toJSON() {} }) as DOMRect;

beforeAll(() => {
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean })
    .IS_REACT_ACT_ENVIRONMENT = true;
  // jsdom lays nothing out: every element is a 320×200 box, every range a
  // line of text inside it.
  Element.prototype.getBoundingClientRect = () => box(0, 0, 320, 200);
  Range.prototype.getBoundingClientRect = () => box(32, 40, 96, 20);
});

const plain: Node = {
  id: "c_plain", type: "text", x: 0, y: 0, width: 320, height: 240,
  text: "the axis is misleading", sp_kind: "plain",
};

const image: Node = {
  id: "c_image", type: "file", x: 400, y: 0, width: 320, height: 240, file: "chart.png",
};

const region = (cardId: string, id: string): Annotation => ({
  id, card_id: cardId, card_title: "", card_rev: 1,
  selector: { type: "rect", x: 0.1, y: 0.2, w: 0.3, h: 0.1 },
  body: "> the axis\n\nrebase at zero", motivation: "editing",
  creator: "kai", creator_kind: "human", resolved: false, resolved_reply: null,
  stale: false, created_at: "2026-10-27T12:00:00.000Z",
});

const canvasProps: ComponentProps<typeof Canvas> = {
  nodes: [plain], allNodes: [plain], edges: [], annotations: [],
  annotateMode: false, draft: null, selectedCard: null, selectedEdge: null,
  selectedAnnotation: null, focus: null,
  onSelectCard: vi.fn(), onSelectEdge: vi.fn(), onSelectAnnotation: vi.fn(), onDraft: vi.fn(),
  onMoveCard: vi.fn(), onResizeCard: vi.fn(), onEditCard: vi.fn(), onDeleteCard: vi.fn(),
  onCreateLink: vi.fn(), onDeleteLink: vi.fn(), onPopOut: vi.fn(),
  onCreateCardAt: vi.fn(), onCreateFileCards: vi.fn(),
};

let host: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

afterEach(() => {
  if (root) act(() => root!.unmount());
  host?.remove();
  root = null;
  host = null;
  window.getSelection()?.removeAllRanges();
});

function render(element: ReactNode): HTMLDivElement {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root!.render(element));
  return host;
}

/** Select an element's text the way a drag would, ending the gesture. */
async function selectText(el: Element) {
  const range = document.createRange();
  range.selectNodeContents(el);
  const sel = window.getSelection()!;
  sel.removeAllRanges();
  sel.addRange(range);
  el.dispatchEvent(new Event("pointerdown", { bubbles: true }));
  document.dispatchEvent(new Event("pointerup", { bubbles: true }));
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
}

describe("comment by selecting (#125)", () => {
  it("offers Comment for selected card text, with no comment mode", async () => {
    const onDraft = vi.fn();
    const container = render(<Canvas {...canvasProps} onDraft={onDraft} />);
    const body = container.querySelector(".card-body.plain")!;

    expect(container.querySelector(".selection-comment")).toBeNull();
    await selectText(body);

    const button = container.querySelector<HTMLButtonElement>(".selection-comment");
    expect(button?.textContent).toBe("Comment");
    act(() => button!.click());

    expect(onDraft).toHaveBeenCalledTimes(1);
    const draft = onDraft.mock.calls[0]![0];
    expect(draft).toMatchObject({
      cardId: plain.id, quote: "the axis is misleading", fromSelection: true,
    });
    // The range's box, as fractions of the card body: an ordinary rect selector.
    expect(draft.selector.type).toBe("rect");
    for (const [k, v] of Object.entries({ x: 0.1, y: 0.2, w: 0.3, h: 0.1 })) {
      expect(draft.selector[k]).toBeCloseTo(v);
    }
    expect(window.getSelection()?.isCollapsed).toBe(true);
    expect(container.querySelector(".selection-comment")).toBeNull();
  });

  it("offers nothing for a selection made outside the card's body", async () => {
    const container = render(<Canvas {...canvasProps} />);

    await selectText(container.querySelector(".card-title")!);

    expect(container.querySelector(".selection-comment")).toBeNull();
  });

  it("withdraws the offer when the selection collapses", async () => {
    const container = render(<Canvas {...canvasProps} />);
    await selectText(container.querySelector(".card-body.plain")!);
    expect(container.querySelector(".selection-comment")).not.toBeNull();

    act(() => {
      window.getSelection()!.removeAllRanges();
      document.dispatchEvent(new Event("selectionchange"));
    });

    expect(container.querySelector(".selection-comment")).toBeNull();
  });
});

describe("highlights", () => {
  it("draws a region on a text card as a highlight that leaves its text selectable", () => {
    const onSelect = vi.fn();
    const container = render(
      <Canvas {...canvasProps} nodes={[plain, image]} allNodes={[plain, image]}
              annotations={[region(plain.id, "a_text"), region(image.id, "a_image")]}
              onSelectAnnotation={onSelect} />,
    );

    const text = container.querySelector('[data-card-id="c_plain"] .pin.rect')!;
    const picture = container.querySelector('[data-card-id="c_image"] .pin.rect')!;
    expect(text.classList).toContain("highlight");
    expect(picture.classList).not.toContain("highlight");

    // Only the marker takes the pointer outside comment mode (see styles.css).
    const marker = text.querySelector(".pin-marker")!;
    act(() => { marker.dispatchEvent(new Event("pointerdown", { bubbles: true })); });
    expect(onSelect).toHaveBeenCalledWith("a_text");
  });
});

describe("the composer", () => {
  const setValue = (el: HTMLTextAreaElement, value: string) => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  };
  const key = (el: Element, init: KeyboardEventInit) =>
    el.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, ...init }));

  it("sends on Enter with the chosen kind, and breaks lines on Shift-Enter", () => {
    const onSubmit = vi.fn();
    const container = render(
      <AnnotationComposer
        draft={{
          cardId: plain.id, selector: { type: "rect", x: 0, y: 0, w: 1, h: 0.1 },
          quote: "the axis", fromSelection: true,
        }}
        cardTitle="Option B" onCancel={vi.fn()} onSubmit={onSubmit} />,
    );
    const area = container.querySelector("textarea")!;
    expect(container.querySelector(".composer-head")?.textContent).toContain("selected text");

    act(() => setValue(area, "rebase at zero"));
    act(() => { key(area, { key: "Enter", shiftKey: true }); });
    expect(onSubmit).not.toHaveBeenCalled();

    const editing = Array.from(container.querySelectorAll<HTMLButtonElement>(".chip"))
      .find((b) => b.textContent === "editing")!;
    act(() => editing.click());
    expect(editing.getAttribute("aria-checked")).toBe("true");

    act(() => { key(area, { key: "Enter" }); });
    expect(onSubmit).toHaveBeenCalledWith("> the axis\n\nrebase at zero", "editing");
  });

  it("does not send an empty comment", () => {
    const onSubmit = vi.fn();
    const container = render(
      <AnnotationComposer draft={{ cardId: plain.id, selector: null }}
                          cardTitle="Option B" onCancel={vi.fn()} onSubmit={onSubmit} />,
    );

    act(() => { key(container.querySelector("textarea")!, { key: "Enter" }); });

    expect(onSubmit).not.toHaveBeenCalled();
  });
});
