import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
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

describe("ViewSelector", () => {
  it("is hidden when a target has fewer than two choices", () => {
    expect(setup(standard.slice(0, 1), "overview", "overview").toolbar).toBeNull();
  });

  it("shows standard choices as pressed-state buttons and selects one on click", () => {
    const { onSelect, toolbar } = setup(standard);
    const buttons = within(toolbar!).getAllByRole("button");

    expect(buttons.map((button) => button.textContent)).toEqual(["Overview", "Table", "Cards"]);
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

  it("closes the custom-view menu when a segment is chosen", () => {
    const { onSelect, toolbar } = setup([...standard, dashboard, active]);

    fireEvent.click(within(toolbar!).getByRole("button", { name: "Views" }));
    expect(screen.getByRole("menu")).toBeInTheDocument();
    fireEvent.click(within(toolbar!).getByRole("button", { name: "Cards" }));
    expect(onSelect).toHaveBeenCalledWith("cards");
    expect(screen.queryByRole("menu")).toBeNull();
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

  it("shows a single custom view as its own segment", () => {
    const { onSelect, toolbar } = setup([...standard, dashboard]);
    const segment = within(toolbar!).getByRole("button", { name: "Dashboard" });

    expect(segment).toHaveAttribute("aria-pressed", "false");
    expect(segment.querySelector(".view-selector__icon")).not.toBeNull();
    expect(screen.queryByRole("button", { expanded: false })).toBeNull();
    fireEvent.click(segment);
    expect(onSelect).toHaveBeenCalledWith("dashboard");
  });

  it("collects several custom views in a menu that marks the default", () => {
    const { onSelect, toolbar } = setup([...standard, dashboard, active], "table", "dashboard");
    const trigger = within(toolbar!).getByRole("button", { name: "Views" });

    expect(within(toolbar!).queryByRole("button", { name: "Dashboard" })).toBeNull();
    expect(trigger).toHaveAttribute("aria-haspopup", "menu");
    expect(trigger).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(trigger);

    const menu = screen.getByRole("menu", { name: "Custom views" });
    const items = within(menu).getAllByRole("menuitemradio");

    expect(items.map((item) => item.textContent)).toEqual(["Dashboard", "Active ideas · Table"]);
    expect(items[0]).toHaveAccessibleDescription("Default view");
    fireEvent.click(items[1]);
    expect(onSelect).toHaveBeenCalledWith("active");
    expect(screen.queryByRole("menu")).toBeNull();
    expect(trigger).toHaveFocus();
  });

  it("labels the menu trigger with the selected custom view and marks it pressed", () => {
    const { toolbar } = setup([...standard, dashboard, active], "active");
    const trigger = within(toolbar!).getByRole("button", { name: "Active ideas · Table" });

    expect(trigger).toHaveAttribute("aria-haspopup", "menu");
    expect(trigger).toHaveAttribute("aria-pressed", "true");
    expect(within(toolbar!).getByRole("button", { name: "Table" })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
    expect(trigger).toHaveAttribute("tabindex", "0");
    fireEvent.click(trigger);
    expect(screen.getByRole("menuitemradio", { name: "Active ideas · Table" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
  });

  it("operates from the keyboard with one Tab stop", () => {
    const { onSelect, toolbar } = setup([...standard, dashboard, active]);
    const buttons = within(toolbar!).getAllByRole("button");

    expect(buttons.map((button) => button.tabIndex)).toEqual([-1, 0, -1, -1]);
    buttons[1].focus();
    fireEvent.keyDown(buttons[1], { key: "ArrowRight" });
    expect(buttons[2]).toHaveFocus();
    expect(buttons.map((button) => button.tabIndex)).toEqual([-1, -1, 0, -1]);
    fireEvent.keyDown(buttons[2], { key: "End" });
    expect(buttons[3]).toHaveFocus();
    fireEvent.keyDown(buttons[3], { key: "ArrowRight" });
    expect(buttons[0]).toHaveFocus();
    fireEvent.keyDown(buttons[0], { key: "ArrowLeft" });
    expect(buttons[3]).toHaveFocus();

    fireEvent.keyDown(buttons[3], { key: "ArrowDown" });
    const items = screen.getAllByRole("menuitemradio");
    expect(items[0]).toHaveFocus();
    fireEvent.keyDown(items[0], { key: "ArrowDown" });
    expect(items[1]).toHaveFocus();
    fireEvent.keyDown(items[1], { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(buttons[3]).toHaveFocus();
    expect(onSelect).not.toHaveBeenCalled();
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
