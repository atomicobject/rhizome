import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { ConfiguredViewFooter } from "./ConfiguredViewStatus";
import { RailHarness, typeView as view } from "./ConfiguredView.typeFixtures";

const WRAP = "configured-view__table-wrap";

/** Lays the table out at `width` pixels and reports it to its observer. */
function tableWidth(width: number) {
  const original = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "clientWidth");
  const originalObserver = globalThis.ResizeObserver;

  Object.defineProperty(HTMLElement.prototype, "clientWidth", {
    configurable: true,
    get(this: HTMLElement) {
      return this.classList.contains(WRAP) ? width : 0;
    },
  });

  globalThis.ResizeObserver = class {
    constructor(private readonly callback: ResizeObserverCallback) {}

    observe() {
      this.callback([], this);
    }

    unobserve() {}
    disconnect() {}
  };

  return () => {
    if (original) Object.defineProperty(HTMLElement.prototype, "clientWidth", original);
    globalThis.ResizeObserver = originalObserver;
  };
}

describe("table column overflow", () => {
  let restore = () => {};

  afterEach(() => restore());

  describe("in a narrow table", () => {
    // Two-line rows: title 320 + Status 136 + ID 176 + Updated 116 + the check cell is 778.
    beforeEach(() => {
      restore = tableWidth(600);
    });

    it("moves the ID column to the Record pane and names it in the footer", () => {
      render(<RailHarness>{view()}</RailHarness>);

      expect(screen.queryByRole("columnheader", { name: "ID" })).toBeNull();
      expect(screen.getByRole("columnheader", { name: "Updated" })).toBeVisible();
      expect(screen.getByText(/ID in Record/)).toBeVisible();
      expect(screen.getByText(/ID shows in the Record pane/)).toBeInTheDocument();

      fireEvent.click(screen.getByRole("button", { name: "Open Alpha Spec" }));
      const record = screen.getByRole("article", { name: "Selected record Alpha Spec" });
      expect(within(record).getByText("ID")).toBeVisible();
    });
  });

  describe("in a wide table", () => {
    beforeEach(() => {
      restore = tableWidth(1200);
    });

    it("keeps every column and lets the title take the slack", () => {
      render(<RailHarness>{view()}</RailHarness>);

      expect(screen.getByRole("columnheader", { name: "ID" })).toBeVisible();
      expect(screen.queryByText(/in Record/)).toBeNull();
      expect(screen.getByText("Status hidden")).toBeVisible();
      expect(screen.getByRole("columnheader", { name: "Title" }).style.width).toBe("");
      // The table groups by Status, so that column leaves: 754 less its 136.
      expect(screen.getByRole("table")).toHaveStyle({ minWidth: "618px" });
    });
  });
});

it("names moved columns and counts hidden ones when both are present", () => {
  render(
    <ConfiguredViewFooter
      summary="52 rows"
      collapsed="1 group collapsed"
      loadingMore={false}
      stagedCount={0}
      moved={["Governing specs", "Created at"]}
      hidden={["Status column hidden", "Type column hidden"]}
      pager={null}
      onPage={() => {}}
    />,
  );
  const summary = screen.getByText("Governing specs, Created at in Record · 2 hidden");
  expect(summary).toBeVisible();
  fireEvent.click(summary);
  expect(screen.getByText(/Governing specs, Created at show in the Record pane/)).toBeVisible();
  expect(screen.getByText(/Status column hidden/)).toBeVisible();
  expect(screen.getByText("1 group collapsed")).toBeVisible();
});
