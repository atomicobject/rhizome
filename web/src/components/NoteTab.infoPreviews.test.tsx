import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { NodePreview, NodeWorkspace } from "../api/types";
import { withFakeFetch } from "../test/fakeFetch";
import { NotesSidebarInfo } from "./NotesSidebarInfo";

const preview: NodePreview = {
  ref: "docs/playground/pizza-party-2026.md#item-1030",
  path: "docs/playground/pizza-party-2026.md",
  title: "Pizza party",
  typeLabel: "Event",
  format: "markdown",
  fragmentResolved: true,
  fields: [],
  hasIssues: false,
};

function setHover(enabled: boolean) {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: vi.fn().mockReturnValue({ matches: enabled }),
  });
}

function renderSidebar(workspace: NodeWorkspace, onOpen = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  function Wrapper({ children }: PropsWithChildren) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }

  return {
    onOpen,
    ...render(<NotesSidebarInfo noteReader workspace={workspace} onOpen={onOpen} />, {
      wrapper: Wrapper,
    }),
  };
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

import {
  workspaceWithEmbeddedRelation,
  workspaceWithSectionsAndNearby,
} from "./body/noteInfoFixtures.test-support";

function groupSection(title: string): HTMLElement {
  const section = screen
    .getByRole("heading", {
      name: (name) => name.replace(/\d+\s*(materials?)?$/, "").trim() === title,
    })
    .closest<HTMLElement>("section, details");

  if (!section) throw new Error(`no sidebar section rendered for ${title}`);

  return section;
}

describe("NotesSidebarInfo note previews", () => {
  const http = withFakeFetch();

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    setHover(true);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("opens a related-note preview to the left and preserves click modes", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
      function (this: HTMLElement) {
        if (this.classList.contains("note-preview")) return new DOMRect(0, 0, 240, 180);

        if (this.classList.contains("ontology-sidebar-info")) {
          return new DOMRect(880, 0, 320, 700);
        }

        if (this instanceof HTMLButtonElement) return new DOMRect(900, 120, 100, 24);

        return new DOMRect();
      },
    );

    const view = renderSidebar(workspaceWithEmbeddedRelation());

    const button = within(groupSection("Relations")).getByRole("button", {
      name: "Decide rain-date contingency by end of May",
    });

    fireEvent.mouseEnter(button, { clientY: 125 });
    await advance(100);
    await advance(200);

    const card = screen.getByRole("region", { name: "Preview of Pizza party" });
    expect(card).toHaveStyle({ left: "634px", top: "120px" });
    expect(button).toHaveAttribute("aria-describedby", card.id);
    const request = http.requests("GET", "/api/v1/nodes/preview")[0];
    expect(request?.query.get("ref")).toBe("docs/playground/pizza-party-2026.md#item-1030");
    expect(request?.query.get("from")).toBe("docs/people/drew.md");

    fireEvent.keyDown(window, { key: "Escape" });
    expect(
      screen.queryByRole("region", { name: "Preview of Pizza party" }),
    ).not.toBeInTheDocument();

    fireEvent.click(button);
    expect(view.onOpen).toHaveBeenLastCalledWith(
      "docs/playground/pizza-party-2026.md#item-1030",
      "stack",
    );
    fireEvent.click(button, { metaKey: true });
    expect(view.onOpen).toHaveBeenLastCalledWith(
      "docs/playground/pizza-party-2026.md#item-1030",
      "beside",
    );
  });

  it("does not duplicate an anchor already present in a relation path", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    const workspace = workspaceWithEmbeddedRelation();
    workspace.relations![0].items![0].path = "docs/playground/pizza-party-2026.md#item-1030";
    const view = renderSidebar(workspace);

    const button = within(groupSection("Relations")).getByRole("button", {
      name: "Decide rain-date contingency by end of May",
    });

    fireEvent.mouseEnter(button);
    await advance(300);

    expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("ref")).toBe(
      "docs/playground/pizza-party-2026.md#item-1030",
    );
    fireEvent.click(button);
    expect(view.onOpen).toHaveBeenCalledWith(
      "docs/playground/pizza-party-2026.md#item-1030",
      "stack",
    );
  });

  it("previews and opens section and nearby refs by their stable targets", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    const view = renderSidebar(workspaceWithSectionsAndNearby());

    const section = within(groupSection("Relations")).getByRole("button", {
      name: "Old section heading",
    });

    fireEvent.mouseEnter(section);
    await advance(300);

    expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("ref")).toBe(
      "docs/sections.md#struct:section-stable",
    );
    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.click(section, { ctrlKey: true });
    expect(view.onOpen).toHaveBeenLastCalledWith(
      "docs/sections.md#struct:section-stable",
      "beside",
    );

    const nearby = within(groupSection("Nearby nodes")).getByRole("button", {
      name: "Nearby section",
    });

    fireEvent.mouseEnter(nearby);
    await advance(300);

    expect(http.requests("GET", "/api/v1/nodes/preview")[1]?.query.get("ref")).toBe(
      "docs/people/drew.md#struct:nearby-stable",
    );
    fireEvent.click(nearby);
    expect(view.onOpen).toHaveBeenLastCalledWith(
      "docs/people/drew.md#struct:nearby-stable",
      "stack",
    );
  });

  it("shrinks a left preview to remain outside a narrow rail", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
      function (this: HTMLElement) {
        if (this.classList.contains("note-preview")) return new DOMRect(0, 0, 440, 180);

        if (this.classList.contains("ontology-sidebar-info")) {
          return new DOMRect(400, 0, 300, 700);
        }

        if (this instanceof HTMLButtonElement) return new DOMRect(420, 120, 100, 24);

        return new DOMRect();
      },
    );

    renderSidebar(workspaceWithEmbeddedRelation());

    const button = within(groupSection("Relations")).getByRole("button", {
      name: "Decide rain-date contingency by end of May",
    });

    fireEvent.mouseEnter(button);
    await advance(100);
    await advance(200);

    const card = screen.getByRole("region", { name: "Preview of Pizza party" });
    expect(card).toHaveStyle({ left: "8px", maxWidth: "386px" });
    expect(
      Number.parseFloat(card.style.left) + Number.parseFloat(card.style.maxWidth),
    ).toBeLessThan(400);
  });

  it("opens from focus, supports Tab transfer, and closes on blur", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    const view = renderSidebar(workspaceWithEmbeddedRelation());

    const button = within(groupSection("Relations")).getByRole("button", {
      name: "Decide rain-date contingency by end of May",
    });

    act(() => button.focus());
    await advance(100);
    await advance(200);

    const card = screen.getByRole("region", { name: "Preview of Pizza party" });
    fireEvent.keyDown(button, { key: "Tab" });
    expect(card).toHaveFocus();
    fireEvent.keyDown(card, { key: "Tab", shiftKey: true });
    expect(button).toHaveFocus();

    fireEvent.blur(button);
    await advance(150);
    expect(
      screen.queryByRole("region", { name: "Preview of Pizza party" }),
    ).not.toBeInTheDocument();
    expect(view.onOpen).not.toHaveBeenCalled();
  });
});
