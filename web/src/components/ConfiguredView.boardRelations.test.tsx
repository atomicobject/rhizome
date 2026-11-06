import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { ConfiguredViewBoard } from "./ConfiguredViewBoard";
import { idea, ideaBoard } from "./ConfiguredView.profileFixtures";
import { rendererProps } from "./ConfiguredView.testFixtures";

it("expands only this card's relations without opening, selecting, or dragging the card", () => {
  const onOpenRow = vi.fn();
  const onParentEvent = vi.fn();
  render(
    <div
      onClick={onParentEvent}
      onPointerDown={onParentEvent}
      onPointerUp={onParentEvent}
      onKeyDown={onParentEvent}
      onKeyUp={onParentEvent}
    >
      <ConfiguredViewBoard
        {...rendererProps({
          execution: ideaBoard(
            [
              idea("Alpha", "captured", { opportunities: ["One", "Two", "Three"] }),
              idea("Bravo", "pursuing", { opportunities: ["One", "Two", "Three"] }),
            ],
            { lanes: false },
          ),
          onOpenRow,
        })}
      />
    </div>,
  );
  const cards = screen.getAllByRole("article");
  const first = within(cards[0]);
  const button = first.getByRole("button", { name: "Show 1 more" });

  for (const kind of ["pointerdown", "pointerup", "keydown", "keyup", "click"]) {
    const event = new Event(kind, { bubbles: true, cancelable: true });
    fireEvent(button, event);
  }

  expect(onParentEvent).not.toHaveBeenCalled();
  expect(first.getByRole("button", { name: "Show less" })).toHaveAttribute("aria-expanded", "true");
  expect(first.getByText("Three")).toBeVisible();
  expect(within(cards[1]).queryByText("Three")).toBeNull();
  expect(onOpenRow).not.toHaveBeenCalled();
  expect(fireEvent.dragStart(button)).toBe(false);
  fireEvent.click(first.getByRole("button", { name: "Show less" }));
  expect(first.queryByText("Three")).toBeNull();
});
