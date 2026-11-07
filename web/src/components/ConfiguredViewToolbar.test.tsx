import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { ViewExecuteRequest, ViewFieldCapability } from "../api/types";
import { ConfiguredViewToolbar } from "./ConfiguredViewToolbar";

const capabilities: ViewFieldCapability[] = [
  { key: "title", label: "Title", sortable: true, groupable: false, filterOps: ["contains"] },
];

const kept = { field: "title", op: "contains", value: "a" };

const removed = { field: "title", op: "contains", value: "b" };

afterEach(() => {
  vi.useRealTimers();
});

it("resyncs to a filter set it emitted earlier once an external change came between", () => {
  vi.useFakeTimers();
  const onResetPage = vi.fn();

  const toolbar = (filters: ViewExecuteRequest["filters"]) => (
    <ConfiguredViewToolbar
      state={{ filters }}
      capabilities={capabilities}
      columns={[]}
      visibleFields={[]}
      activeVariant="table"
      onToggleColumn={vi.fn()}
      onResetPage={onResetPage}
      placeTools={(tools) => tools}
    />
  );

  const { rerender } = render(toolbar([kept, removed]));

  fireEvent.click(screen.getByRole("button", { name: /Remove filter Title .* b/ }));
  act(() => vi.advanceTimersByTime(300));
  expect(onResetPage).toHaveBeenCalledWith({ filters: [kept] });

  // The emitted value echoes back, then history moves away and returns to it.
  rerender(toolbar([kept]));
  rerender(toolbar([]));
  act(() => vi.advanceTimersByTime(300));
  rerender(toolbar([kept]));
  act(() => vi.advanceTimersByTime(300));

  expect(onResetPage).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("button", { name: /Remove filter Title .* a/ })).toBeTruthy();
});
