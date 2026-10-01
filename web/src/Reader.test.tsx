// @vitest-environment jsdom

import { act, type ComponentProps } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { Reader, topbarAfterScroll } from "./Reader";
import type { Node } from "./api";

beforeAll(() => {
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean })
    .IS_REACT_ACT_ENVIRONMENT = true;
});

const card = (id: string, x: number, text: string, extra: Partial<Node> = {}): Node => ({
  id, type: "text", x, y: 0, width: 300, height: 200, text, sp_title: id.toUpperCase(),
  sp_kind: "plain", ...extra,
});

const nodes = [card("c_a", 0, "first"), card("c_b", 400, "second\nnew line"), card("c_c", 800, "third")];

function props(overrides: Partial<ComponentProps<typeof Reader>> = {}): ComponentProps<typeof Reader> {
  return {
    nodes,
    edges: [{ id: "l_1", fromNode: "c_a", toNode: "c_c", label: "depends on" }],
    annotations: [],
    events: [],
    changes: new Map(),
    lastChanges: new Map(),
    currentId: "c_a",
    order: "board",
    editRequest: 0,
    onOrder: vi.fn(),
    onNavigate: vi.fn(),
    onMarkSeen: vi.fn(),
    onMarkAllSeen: vi.fn(),
    onEdit: vi.fn(),
    onDraft: vi.fn(),
    onResolve: vi.fn(),
    onReopen: vi.fn(),
    onShowOnCanvas: vi.fn(),
    onFind: vi.fn(),
    onPopOut: vi.fn(),
    onQuickAdd: vi.fn(),
    notify: vi.fn(),
    ...overrides,
  };
}

let host: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

afterEach(() => {
  if (root) act(() => root!.unmount());
  host?.remove();
  root = null;
  host = null;
});

function render(p: ComponentProps<typeof Reader>): HTMLDivElement {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root!.render(<Reader {...p} />));
  return host;
}

const click = (el: Element | null | undefined) => act(() => (el as HTMLElement).click());

describe("Reader", () => {
  it("shows one card with its position, and steps to the next", () => {
    const p = props();
    const el = render(p);
    expect(el.querySelector(".reader-title h1")?.textContent).toBe("C_A");
    expect(el.querySelector(".reader-pos")?.textContent).toBe("1 / 3");
    click(el.querySelector('[aria-label="Next card"]'));
    expect(p.onNavigate).toHaveBeenCalledWith("c_b");
    expect(el.querySelector<HTMLButtonElement>('[aria-label="Previous card"]')!.disabled).toBe(true);
  });

  it("follows a link chip", () => {
    const p = props();
    const el = render(p);
    const chip = el.querySelector(".reader-links .chip");
    expect(chip?.textContent).toContain("depends on");
    click(chip);
    expect(p.onNavigate).toHaveBeenCalledWith("c_c");
  });

  it("offers the diff of an edit since last seen, and acknowledges it", () => {
    const p = props({
      currentId: "c_b",
      changes: new Map([["c_b", { kind: "edited" as const, before: "second", newComments: 0, actor: "claude-code" }]]),
    });
    const el = render(p);
    expect(el.querySelector(".change-note .what")?.textContent).toBe("edited by claude-code since you last looked");
    const diffButton = [...el.querySelectorAll(".change-note .diff-toggle button")].find((b) => b.textContent === "diff");
    click(diffButton);
    expect(el.querySelector(".diff-row.add")?.textContent).toContain("new line");
    click(el.querySelector(".change-note .seen"));
    expect(p.onMarkSeen).toHaveBeenCalledWith("c_b");
  });

  it("says so when there is nothing new", () => {
    const el = render(props({ order: "changes", currentId: null }));
    expect(el.textContent).toContain("You're all caught up.");
  });

  it("switching to an empty what's-new queue shows caught up, not the last card", () => {
    const p = props();
    const el = render(p);
    expect(el.querySelector(".reader-title h1")?.textContent).toBe("C_A");
    act(() => root!.render(<Reader {...p} order="changes" />));
    expect(el.textContent).toContain("You're all caught up.");
    expect(el.querySelector(".reader-title")).toBeNull();
    // and back again, the board opens on its first card
    act(() => root!.render(<Reader {...p} order="board" />));
    expect(p.onNavigate).toHaveBeenLastCalledWith("c_a");
  });

  it("edits title and text", () => {
    const p = props();
    const el = render(p);
    click(el.querySelector('[title="Edit this card"]'));
    const textarea = el.querySelector<HTMLTextAreaElement>(".reader-editor textarea")!;
    expect(textarea.value).toBe("first");
    const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!;
    act(() => {
      setter.call(textarea, "first, revised");
      textarea.dispatchEvent(new Event("input", { bubbles: true }));
    });
    act(() => {
      el.querySelector<HTMLFormElement>(".reader-editor")!.requestSubmit();
    });
    expect(p.onEdit).toHaveBeenCalledWith("c_a", "first, revised", "C_A");
  });
});

describe("topbarAfterScroll", () => {
  const fresh = () => ({ anchor: 0, last: 0, settleUntil: 0 });

  it("hides on a sustained scroll down and shows on the way back up", () => {
    const s = fresh();
    const seen = [10, 20, 40].map((y) => topbarAfterScroll(s, y, 1000, 0));
    expect(seen).toEqual([false, false, true]);
    expect(topbarAfterScroll(s, 300, 1000, 0)).toBe(true);
    // turning back measures from the turn, not from the top
    expect(topbarAfterScroll(s, 290, 1000, 0)).toBeNull();
    expect(topbarAfterScroll(s, 270, 1000, 0)).toBe(false);
  });

  it("brings the bar back at the top even while settling", () => {
    const s = { anchor: 200, last: 200, settleUntil: 100 };
    expect(topbarAfterScroll(s, 0, 1000, 50)).toBe(false);
  });

  it("keeps the bar for a card that barely overflows", () => {
    const s = fresh();
    expect(topbarAfterScroll(s, 60, 70, 0)).toBeNull();
  });

  it("ignores the scroll the bar itself causes while it moves", () => {
    const s = { anchor: 200, last: 200, settleUntil: 100 };
    expect(topbarAfterScroll(s, 150, 1000, 50)).toBeNull();
    // after settling, direction is measured from where the body came to rest
    expect(topbarAfterScroll(s, 140, 1000, 200)).toBeNull();
  });
});

describe("Reader top bar", () => {
  it("resets the scroll for a new card, not for a new callback", () => {
    const scrollTo = vi.fn();
    const original = HTMLElement.prototype.scrollTo;
    HTMLElement.prototype.scrollTo = scrollTo as typeof original;
    try {
      const p = props({ onTopbar: vi.fn() });
      render(p);
      scrollTo.mockClear();
      // crossing the narrow breakpoint swaps the callback
      act(() => root!.render(<Reader {...p} onTopbar={undefined} />));
      expect(scrollTo).not.toHaveBeenCalled();
      act(() => root!.render(<Reader {...p} onTopbar={undefined} currentId="c_b" />));
      expect(scrollTo).toHaveBeenCalledWith(0, 0);
    } finally {
      HTMLElement.prototype.scrollTo = original;
    }
  });

  it("follows an html card scrolling inside its frame", () => {
    const onTopbar = vi.fn();
    const el = render(props({
      nodes: [card("c_h", 0, "<p>hi</p>", { sp_kind: "html" })],
      edges: [],
      currentId: "c_h",
      onTopbar,
    }));
    const frame = el.querySelector("iframe")!;
    const report = (sy: number) => act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        data: { type: "analog-scroll", sy, ch: 2000, vh: 600 },
        source: frame.contentWindow,
      }));
    });
    report(300);
    expect(onTopbar).toHaveBeenLastCalledWith(true);
  });

  it("syncs the bar when the narrow callback returns after a card change", () => {
    const onTopbar = vi.fn();
    const p = props({
      nodes: [card("c_h", 0, "<p>hi</p>", { sp_kind: "html" }), card("c_b", 400, "second")],
      edges: [],
      currentId: "c_h",
      onTopbar,
    });
    const el = render(p);
    const frame = el.querySelector("iframe")!;
    act(() => {
      window.dispatchEvent(new MessageEvent("message", {
        data: { type: "analog-scroll", sy: 300, ch: 2000, vh: 600 },
        source: frame.contentWindow,
      }));
    });
    expect(onTopbar).toHaveBeenLastCalledWith(true);
    // widen, move on to the next card, narrow again
    act(() => root!.render(<Reader {...p} onTopbar={undefined} />));
    act(() => root!.render(<Reader {...p} onTopbar={undefined} currentId="c_b" />));
    act(() => root!.render(<Reader {...p} onTopbar={onTopbar} currentId="c_b" />));
    expect(onTopbar).toHaveBeenLastCalledWith(false);
  });
});
