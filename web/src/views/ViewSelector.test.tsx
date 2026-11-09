import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ViewChoice } from "../api/types";
import { ViewSelector } from "./ViewSelector";

const standard: ViewChoice[] = [
  { id: "overview", name: "Overview", renderer: "overview" },
  { id: "table", name: "Table", renderer: "table" },
  { id: "cards", name: "Cards", renderer: "card" },
];

const dashboard: ViewChoice = {
  id: "dashboard",
  name: "Dashboard",
  renderer: "custom",
  custom: true,
};

const active: ViewChoice = {
  id: "active",
  name: "Active ideas · Table",
  renderer: "table",
  custom: true,
};

// A bundled standard view renders as custom but ranks as standard.
const briefing: ViewChoice = { id: "briefing", name: "Briefing", renderer: "custom" };

// jsdom lays nothing out: the switcher is as wide as `rootWidth`, trailing
// controls as `trailingWidth`, and a measured choice or trigger is 10px per
// character plus 20px, so "Dashboard" is 110 and "More" is 60.
let rootWidth = 1000;

let trailingWidth = 0;

const resizeCallbacks = new Set<() => void>();

function widthOf(element: Element) {
  if (element.classList.contains("view-selector")) return rootWidth;

  if (element.classList.contains("view-selector__trailing")) return trailingWidth;

  if (element.tagName === "BUTTON" && element.closest(".view-selector__ruler"))
    return 10 * (element.textContent?.length ?? 0) + 20;

  return 0;
}

class TestResizeObserver implements ResizeObserver {
  constructor(private readonly callback: ResizeObserverCallback) {}

  private readonly fire = () => this.callback([], this);

  observe() {
    resizeCallbacks.add(this.fire);
  }

  unobserve() {}

  disconnect() {
    resizeCallbacks.delete(this.fire);
  }
}

function resize(width: number) {
  rootWidth = width;
  act(() => resizeCallbacks.forEach((callback) => callback()));
}

beforeEach(() => {
  rootWidth = 1000;
  trailingWidth = 0;
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (this: Element) {
    return new DOMRect(0, 0, widthOf(this), 20);
  });
  vi.stubGlobal("ResizeObserver", TestResizeObserver);
});

afterEach(() => {
  resizeCallbacks.clear();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function setup(choices: ViewChoice[], selectedId: string | null = "table", defaultId = "table") {
  const onSelect = vi.fn();
  render(
    <ViewSelector
      target={{ choices, defaultChoiceId: defaultId }}
      selectedId={selectedId}
      onSelect={onSelect}
    />,
  );

  return { onSelect, toolbar: screen.queryByRole("toolbar", { name: "Workspace view" }) };
}

const labels = (toolbar: HTMLElement) =>
  within(toolbar)
    .getAllByRole("button")
    .map((button) => button.textContent);

describe("ViewSelector", () => {
  it("is hidden when a target has fewer than two choices", () => {
    expect(setup(standard.slice(0, 1), "overview", "overview").toolbar).toBeNull();
  });

  it("keeps the selected view's controls when there is no switcher", () => {
    render(
      <ViewSelector
        target={{ choices: standard.slice(0, 1), defaultChoiceId: "overview" }}
        selectedId="overview"
        onSelect={vi.fn()}
        trailing={<button type="button">Reset to shared</button>}
      />,
    );

    expect(screen.queryByRole("toolbar", { name: "Workspace view" })).toBeNull();
    expect(screen.getByRole("button", { name: "Reset to shared" })).toBeVisible();
  });

  it("shows standard choices as pressed-state buttons and selects one on click", () => {
    const { onSelect, toolbar } = setup(standard);

    expect(labels(toolbar!)).toEqual(["Overview", "Table", "Cards"]);
    expect(within(toolbar!).getByRole("button", { name: "Table" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    fireEvent.click(within(toolbar!).getByRole("button", { name: "Cards" }));
    expect(onSelect).toHaveBeenCalledWith("cards");
    fireEvent.click(within(toolbar!).getByRole("button", { name: "Table" }));
    // Re-picking the selected default reports it so the remembered choice clears.
    expect(onSelect).toHaveBeenLastCalledWith("table");
  });

  it("ignores a click on a selected choice that is not the default", () => {
    const { onSelect, toolbar } = setup(standard, "cards");

    fireEvent.click(within(toolbar!).getByRole("button", { name: "Cards" }));
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("marks only the default choice", () => {
    const { toolbar } = setup(standard, "overview");

    expect(within(toolbar!).getByRole("button", { name: "Table" })).toHaveAccessibleDescription(
      "Default view",
    );
    expect(within(toolbar!).getByRole("button", { name: "Cards" })).not.toHaveAccessibleDescription(
      "Default view",
    );
  });

  it("puts custom choices first, ranked by their custom flag, inline when they fit", () => {
    const { onSelect, toolbar } = setup([...standard, briefing, dashboard, active]);

    expect(labels(toolbar!)).toEqual([
      "Dashboard",
      "Active ideas · Table",
      "Overview",
      "Table",
      "Cards",
      "Briefing",
    ]);
    expect(within(toolbar!).queryByRole("button", { name: "More" })).toBeNull();

    const segment = within(toolbar!).getByRole("button", { name: "Dashboard" });

    expect(segment.querySelector(".view-selector__icon")).not.toBeNull();
    expect(
      within(toolbar!)
        .getByRole("button", { name: "Briefing" })
        .querySelector(".view-selector__icon"),
    ).toBeNull();
    fireEvent.click(segment);
    expect(onSelect).toHaveBeenCalledWith("dashboard");
  });

  it("moves the choices that do not fit into a More menu that marks the default", () => {
    rootWidth = 400;
    // Inline: Dashboard 110 + Active 220 + More 60 = 390 of 400.
    const { onSelect, toolbar } = setup([...standard, dashboard, active], "dashboard", "table");

    expect(labels(toolbar!)).toEqual(["Dashboard", "Active ideas · Table", "More"]);

    const trigger = within(toolbar!).getByRole("button", { name: "More" });

    expect(trigger).toHaveAttribute("aria-haspopup", "menu");
    expect(trigger).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(trigger);

    const menu = screen.getByRole("menu", { name: "More views" });
    const items = within(menu).getAllByRole("menuitemradio");

    expect(items.map((item) => item.textContent)).toEqual(["Overview", "TableDefault", "Cards"]);
    expect(items[1]).toHaveAccessibleDescription("Default view");
    fireEvent.click(items[2]);
    expect(onSelect).toHaveBeenCalledWith("cards");
    expect(screen.queryByRole("menu")).toBeNull();
    expect(trigger).toHaveFocus();
  });

  it("counts trailing controls against the width", () => {
    rootWidth = 500;
    const choices = [...standard, dashboard, active];

    const { unmount } = render(
      <ViewSelector
        target={{ choices, defaultChoiceId: "table" }}
        selectedId="dashboard"
        onSelect={vi.fn()}
      />,
    );

    // 110 + 220 + 100 + More 60 = 490 of 500.
    expect(labels(screen.getByRole("toolbar"))).toEqual([
      "Dashboard",
      "Active ideas · Table",
      "Overview",
      "More",
    ]);
    unmount();

    trailingWidth = 100;
    render(
      <ViewSelector
        target={{ choices, defaultChoiceId: "table" }}
        selectedId="dashboard"
        onSelect={vi.fn()}
        trailing={<button type="button">Reset to shared</button>}
      />,
    );
    expect(labels(screen.getByRole("toolbar"))).toEqual([
      "Dashboard",
      "Active ideas · Table",
      "More",
    ]);
  });

  it("names a selected choice in the menu on the trigger and marks it pressed", () => {
    rootWidth = 400;
    // The trigger is as wide as "Cards" (70): 110 + 220 + 70 = 400.
    const { toolbar } = setup([...standard, dashboard, active], "cards");
    const trigger = within(toolbar!).getByRole("button", { name: "Cards" });

    expect(labels(toolbar!)).toEqual(["Dashboard", "Active ideas · Table", "Cards"]);
    expect(trigger).toHaveAttribute("aria-haspopup", "menu");
    expect(trigger).toHaveAttribute("aria-pressed", "true");
    expect(trigger).toHaveAttribute("tabindex", "0");
    fireEvent.click(trigger);
    expect(screen.getByRole("menuitemradio", { name: "Cards" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
  });

  it("follows the width as it changes", () => {
    const { toolbar } = setup([...standard, dashboard, active], "dashboard");

    expect(labels(toolbar!)).toHaveLength(5);
    resize(250);
    expect(labels(toolbar!)).toEqual(["Dashboard", "More"]);
    resize(1000);
    expect(labels(toolbar!)).toHaveLength(5);
  });

  it("moves focus to the trigger when a resize moves the focused choice into the menu", () => {
    const { toolbar } = setup([...standard, dashboard, active], "dashboard");
    const cards = within(toolbar!).getByRole("button", { name: "Cards" });

    cards.focus();
    resize(400);

    const trigger = within(toolbar!).getByRole("button", { name: "More" });

    expect(trigger).toHaveFocus();
    expect(trigger).toHaveAttribute("tabindex", "0");
    expect(within(toolbar!).getByRole("button", { name: "Dashboard" })).toHaveAttribute(
      "tabindex",
      "-1",
    );
  });

  it("leaves focus elsewhere alone when a resize changes the switcher", () => {
    render(<input aria-label="Elsewhere" />);
    const { toolbar } = setup([...standard, dashboard, active], "dashboard");
    const elsewhere = screen.getByRole("textbox", { name: "Elsewhere" });

    within(toolbar!).getByRole("button", { name: "Cards" }).focus();
    elsewhere.focus();
    resize(400);
    expect(elsewhere).toHaveFocus();
  });

  it("operates from the keyboard with one Tab stop", () => {
    rootWidth = 500;
    const { onSelect, toolbar } = setup([...standard, dashboard, active], "overview");
    const buttons = within(toolbar!).getAllByRole("button");

    expect(buttons.map((button) => button.textContent)).toEqual([
      "Dashboard",
      "Active ideas · Table",
      "Overview",
      "More",
    ]);
    expect(buttons.map((button) => button.tabIndex)).toEqual([-1, -1, 0, -1]);
    buttons[2].focus();
    fireEvent.keyDown(buttons[2], { key: "ArrowRight" });
    expect(buttons[3]).toHaveFocus();
    expect(buttons.map((button) => button.tabIndex)).toEqual([-1, -1, -1, 0]);
    fireEvent.keyDown(buttons[3], { key: "Home" });
    expect(buttons[0]).toHaveFocus();
    fireEvent.keyDown(buttons[0], { key: "ArrowLeft" });
    expect(buttons[3]).toHaveFocus();
    fireEvent.keyDown(buttons[3], { key: "ArrowRight" });
    expect(buttons[0]).toHaveFocus();
    fireEvent.keyDown(buttons[0], { key: "End" });
    expect(buttons[3]).toHaveFocus();

    fireEvent.keyDown(buttons[3], { key: "ArrowDown" });
    const items = screen.getAllByRole("menuitemradio");
    expect(items.map((item) => item.textContent)).toEqual(["TableDefault", "Cards"]);
    expect(items[0]).toHaveFocus();
    fireEvent.keyDown(items[0], { key: "ArrowDown" });
    expect(items[1]).toHaveFocus();
    fireEvent.keyDown(items[1], { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(buttons[3]).toHaveFocus();
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("closes the More menu when an inline choice is chosen", () => {
    rootWidth = 400;
    const { onSelect, toolbar } = setup([...standard, dashboard, active], "cards");

    fireEvent.click(within(toolbar!).getByRole("button", { name: "Cards" }));
    expect(screen.getByRole("menu")).toBeInTheDocument();
    fireEvent.click(within(toolbar!).getByRole("button", { name: "Dashboard" }));
    expect(onSelect).toHaveBeenCalledWith("dashboard");
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("reports an unsaved or unloaded choice with a retry", () => {
    const retry = vi.fn(() => Promise.resolve());

    const status = (pending: boolean, error: Error | null) => (
      <ViewSelector
        target={{ choices: standard, defaultChoiceId: "table" }}
        selectedId="cards"
        onSelect={() => {}}
        status={{ pending, error, retry }}
      />
    );

    const { rerender } = render(status(true, null));
    expect(screen.queryByRole("alert")).toBeNull();
    rerender(status(true, new Error("offline")));
    expect(screen.getByRole("alert")).toHaveTextContent("View choice not saved.");
    fireEvent.click(within(screen.getByRole("alert")).getByRole("button", { name: "Retry" }));
    expect(retry).toHaveBeenCalledOnce();
    rerender(status(false, new Error("offline")));
    expect(screen.getByRole("alert")).toHaveTextContent("Saved view choice could not load.");
  });
});
