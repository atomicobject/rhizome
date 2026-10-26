import { fireEvent, render, screen } from "@testing-library/react";
import { useRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { PanelResizeHandle } from "./PanelResizeHandle";
import { PANE_RANGE, SIDEBAR_RANGE, useShellPanes } from "./shellPanes";

const STORAGE_KEY = "rhizome:notes:panes:v1";

function Shell() {
  const ref = useRef<HTMLDivElement>(null);
  const panes = useShellPanes();

  return (
    <div ref={ref} data-testid="shell" data-pane-collapsed={panes.paneCollapsed}>
      <PanelResizeHandle
        label="Resize sidebar"
        range={SIDEBAR_RANGE}
        value={panes.sidebarWidth}
        edge="end"
        cssVar="--sidebar-width"
        target={ref}
        onResize={panes.setSidebarWidth}
      />
      <PanelResizeHandle
        label="Resize right pane"
        range={PANE_RANGE}
        value={panes.paneWidth}
        edge="start"
        cssVar="--pane-width"
        target={ref}
        onResize={panes.setPaneWidth}
      />
      <button type="button" onClick={panes.togglePane}>
        Toggle pane
      </button>
    </div>
  );
}

const sidebar = () => screen.getByRole("separator", { name: "Resize sidebar" });

const pane = () => screen.getByRole("separator", { name: "Resize right pane" });

function mockViewport(narrow: boolean) {
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      matches: narrow,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
}

beforeEach(() => window.localStorage.clear());

afterEach(() => vi.unstubAllGlobals());

describe("panel resizing", () => {
  it("exposes each handle as a vertical separator with its range", () => {
    render(<Shell />);

    expect(sidebar()).toHaveAttribute("aria-orientation", "vertical");
    expect(sidebar()).toHaveAttribute("aria-valuenow", "264");
    expect(sidebar()).toHaveAttribute("aria-valuemin", "200");
    expect(sidebar()).toHaveAttribute("aria-valuemax", "400");
    expect(sidebar()).toHaveAttribute("tabindex", "0");
    expect(pane()).toHaveAttribute("aria-valuenow", "340");
    expect(pane()).toHaveAttribute("aria-valuemin", "280");
    expect(pane()).toHaveAttribute("aria-valuemax", "560");
  });

  it("moves 16px per arrow toward the panel's inner edge, and Home and End jump to the limits", () => {
    render(<Shell />);

    fireEvent.keyDown(sidebar(), { key: "ArrowRight" });
    expect(sidebar()).toHaveAttribute("aria-valuenow", "280");
    fireEvent.keyDown(sidebar(), { key: "ArrowLeft" });
    fireEvent.keyDown(sidebar(), { key: "ArrowLeft" });
    expect(sidebar()).toHaveAttribute("aria-valuenow", "248");
    fireEvent.keyDown(sidebar(), { key: "Home" });
    expect(sidebar()).toHaveAttribute("aria-valuenow", "200");
    fireEvent.keyDown(sidebar(), { key: "ArrowLeft" });
    expect(sidebar()).toHaveAttribute("aria-valuenow", "200");

    // The right pane's handle sits on its left edge, so Left widens it.
    fireEvent.keyDown(pane(), { key: "ArrowLeft" });
    expect(pane()).toHaveAttribute("aria-valuenow", "356");
    fireEvent.keyDown(pane(), { key: "End" });
    expect(pane()).toHaveAttribute("aria-valuenow", "560");
  });

  it("drags without re-rendering, clamps to the range, and commits on release", () => {
    render(<Shell />);
    const shell = screen.getByTestId("shell");

    fireEvent.pointerDown(sidebar(), { button: 0, clientX: 264, pointerId: 1 });
    fireEvent.pointerMove(sidebar(), { clientX: 300, pointerId: 1 });
    expect(shell.style.getPropertyValue("--sidebar-width")).toBe("300px");
    expect(sidebar()).toHaveAttribute("aria-valuenow", "264");
    fireEvent.pointerMove(sidebar(), { clientX: 900, pointerId: 1 });
    expect(shell.style.getPropertyValue("--sidebar-width")).toBe("400px");
    fireEvent.pointerUp(sidebar(), { pointerId: 1 });
    expect(sidebar()).toHaveAttribute("aria-valuenow", "400");

    fireEvent.pointerDown(pane(), { button: 0, clientX: 1000, pointerId: 2 });
    fireEvent.pointerMove(pane(), { clientX: 1100, pointerId: 2 });
    fireEvent.pointerUp(pane(), { pointerId: 2 });
    expect(pane()).toHaveAttribute("aria-valuenow", "280");
  });

  it("persists widths and the pane's collapsed state across reloads", () => {
    const first = render(<Shell />);
    fireEvent.keyDown(sidebar(), { key: "End" });
    fireEvent.click(screen.getByRole("button", { name: "Toggle pane" }));
    first.unmount();

    render(<Shell />);
    expect(sidebar()).toHaveAttribute("aria-valuenow", "400");
    expect(screen.getByTestId("shell")).toHaveAttribute("data-pane-collapsed", "true");
  });

  it("ignores stored widths outside the range", () => {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ sidebar: 9000, pane: 10 }));
    render(<Shell />);

    expect(sidebar()).toHaveAttribute("aria-valuenow", "400");
    expect(pane()).toHaveAttribute("aria-valuenow", "280");
  });

  it("starts the pane collapsed below 1200px whatever was stored, and opens it on toggle", () => {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ paneCollapsed: false }));
    mockViewport(true);
    render(<Shell />);

    const shell = screen.getByTestId("shell");
    expect(shell).toHaveAttribute("data-pane-collapsed", "true");
    fireEvent.click(screen.getByRole("button", { name: "Toggle pane" }));
    expect(shell).toHaveAttribute("data-pane-collapsed", "false");
    // Opening the overlay leaves the stored wide-window choice alone.
    expect(JSON.parse(window.localStorage.getItem(STORAGE_KEY) ?? "{}").paneCollapsed).toBe(false);
  });
});
