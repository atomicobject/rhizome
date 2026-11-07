import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { LocatorWidget } from "./PropertyLocatorWidget";

function renderWidget() {
  const onStage = vi.fn();
  render(
    <LocatorWidget
      value="old"
      editing
      dirty={false}
      present
      onStage={onStage}
      ariaLabel="Locator"
    />,
  );
  const input = screen.getByLabelText<HTMLInputElement>("Locator");
  input.focus();
  fireEvent.change(input, { target: { value: "edited" } });

  return { input, onStage };
}

it("Escape reverts the draft without staging, and Enter stages once", () => {
  const escaped = renderWidget();
  fireEvent.keyDown(escaped.input, { key: "Escape" });
  expect(escaped.onStage).not.toHaveBeenCalled();
  expect(escaped.input.value).toBe("old");
  cleanup();

  const entered = renderWidget();
  fireEvent.keyDown(entered.input, { key: "Enter" });
  expect(entered.onStage).toHaveBeenCalledTimes(1);
  expect(entered.onStage).toHaveBeenCalledWith("edited");
});
