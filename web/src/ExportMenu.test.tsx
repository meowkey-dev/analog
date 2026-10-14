// @vitest-environment jsdom

import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { ExportMenu } from "./ExportMenu";

beforeAll(() => {
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean })
    .IS_REACT_ACT_ENVIRONMENT = true;
});

let root: Root | null = null;

afterEach(() => {
  act(() => root?.unmount());
  root = null;
  document.body.replaceChildren();
});

function render(card?: { id: string; title?: string }): HTMLElement {
  const host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root!.render(
    <ExportMenu title="Nav" slug="redesign" card={card} onError={vi.fn()} onBusy={vi.fn()} />,
  ));
  return host;
}

const items = (host: HTMLElement) =>
  [...host.querySelectorAll('[role="menuitem"]')].map((item) => item.firstChild?.textContent?.trim());

describe("ExportMenu", () => {
  it("exports the board when nothing is selected", () => {
    const host = render();
    const button = host.querySelector("button")!;
    expect(button.textContent).toBe("export");
    act(() => button.click());
    expect(items(host)).toEqual(["HTML", "PDF"]);
  });

  it("becomes export card with a selection, keeping the board one click away", () => {
    const host = render({ id: "c_1", title: "Option A" });
    const button = host.querySelector("button")!;
    expect(button.textContent).toBe("export card");
    act(() => button.click());
    expect(items(host)).toEqual(["HTML", "PDF", "board HTML", "board PDF"]);
  });
});
