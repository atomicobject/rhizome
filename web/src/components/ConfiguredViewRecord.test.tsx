import { act, fireEvent, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { ConfiguredViewRecord } from "./ConfiguredViewRecord";
import { typeExecution } from "./ConfiguredView.typeFixtures";
import { baseView, firstRow } from "./ConfiguredView.testFixtures";

afterEach(() => vi.unstubAllGlobals());

it("offers the full summary when resizing causes overflow", () => {
  const observers = new Map<Element, () => void>();
  const disconnect = vi.fn();
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(private readonly callback: ResizeObserverCallback) {}
      observe(element: Element) {
        observers.set(element, () => this.callback([], this));
      }
      unobserve() {}
      disconnect = disconnect;
    },
  );

  const { unmount } = render(
    <ConfiguredViewRecord
      row={{ ...firstRow, fields: { summary: "A summary that fits until the pane narrows." } }}
      profile={typeExecution().profile}
      capabilities={[]}
      source={baseView.source}
      viewID="test"
      onOpenRow={() => {}}
    />,
  );

  const summary = screen.getByText("A summary that fits until the pane narrows.");
  expect(screen.queryByRole("button", { name: "Show full summary" })).toBeNull();
  Object.defineProperties(summary, {
    scrollHeight: { configurable: true, value: 200 },
    clientHeight: { configurable: true, value: 100 },
  });
  act(() => observers.get(summary)?.());
  fireEvent.click(screen.getByRole("button", { name: "Show full summary" }));
  expect(summary).not.toHaveClass("is-clamped");
  fireEvent.click(screen.getByRole("button", { name: "Show less summary" }));
  Object.defineProperty(summary, "clientHeight", { configurable: true, value: 200 });
  act(() => observers.get(summary)?.());
  expect(screen.queryByRole("button", { name: "Show full summary" })).toBeNull();
  unmount();
  expect(disconnect).toHaveBeenCalled();
});
