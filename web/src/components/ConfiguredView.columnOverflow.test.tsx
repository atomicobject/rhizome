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
    // Status is grouped; the title, ID, Updated, and selection need 778px.
    beforeEach(() => {
      restore = tableWidth(700);
    });

    it("moves the ID column to the Record pane and names it in the footer", () => {
      render(<RailHarness>{view()}</RailHarness>);

      expect(screen.queryByRole("columnheader", { name: "ID" })).toBeNull();
      expect(screen.getByRole("columnheader", { name: "Updated" })).toBeVisible();
      expect(screen.getAllByText(/fields? in Record/)[0]).toBeVisible();
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
      expect(screen.queryByText(/fields? in Record/)).toBeNull();
      expect(screen.getByText("Hidden: Status")).toBeVisible();
      expect(screen.getByRole("columnheader", { name: "Title" }).style.width).toBe("");
      // The grouped Status leaves 480 title + 176 ID + 92 Updated + 30 selection.
      expect(screen.getByRole("table")).toHaveStyle({ minWidth: "778px" });
    });
  });
});

it("names every moved and hidden field in the visible summary", () => {
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
  const summary = screen.getByText("2 fields in Record · Hidden: Status, Type").closest("summary")!;
  expect(summary).toBeVisible();
  expect(summary).toHaveAttribute(
    "title",
    "In Record: Governing specs, Created at. Hidden: Status, Type",
  );
  fireEvent.click(summary);
  expect(screen.getByText(/Governing specs, Created at show in the Record pane/)).toBeVisible();
  expect(screen.getByText(/Status column hidden/)).toBeVisible();
  expect(screen.getByText("1 group collapsed")).toBeVisible();
});
