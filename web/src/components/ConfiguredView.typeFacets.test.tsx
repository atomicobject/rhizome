import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ViewExecuteRequest } from "../api/types";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { baseView, renderView } from "./ConfiguredView.testFixtures";
import { executedWith, STATUS, typeExecution, typeView } from "./ConfiguredView.typeFixtures";

type Filter = NonNullable<ViewExecuteRequest["filters"]>[number];

const STALE: Filter = { field: "stale", op: "eq", value: "true" };

const facets = () => screen.getByRole("group", { name: "Collection summary" });

describe("type collection facets", () => {
  it("combines Stale with a lifecycle facet and toggles it without touching lifecycle filters", () => {
    const onStateChange = vi.fn();
    renderView({ execution: typeExecution(), state: { filters: [STALE] }, onStateChange });

    expect(within(facets()).getByRole("button", { name: "Unchanged 30d 3" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    fireEvent.click(within(facets()).getByRole("button", { name: "Draft 9" }));
    expect(onStateChange).toHaveBeenLastCalledWith(
      expect.objectContaining({
        filters: [STALE, { field: STATUS, op: "in", values: ["draft"] }],
      }),
    );
  });

  it("removes only the stale filter when Stale is toggled off", () => {
    const onStateChange = vi.fn();
    const lifecycle: Filter = { field: STATUS, op: "in", values: ["active"] };
    renderView({
      execution: typeExecution(),
      state: { filters: [lifecycle, STALE] },
      onStateChange,
    });

    fireEvent.click(within(facets()).getByRole("button", { name: "Unchanged 30d 3" }));
    expect(onStateChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ filters: [lifecycle] }),
    );
  });
});

describe("lifecycle facets over a saved eq filter", () => {
  const savedActive: Filter = { field: STATUS, op: "eq", value: "active" };

  it("removes the saved filter when its pressed facet is released", () => {
    const onStateChange = vi.fn();
    renderView({
      execution: typeExecution(),
      state: { filters: [STALE, savedActive] },
      onStateChange,
    });

    const active = within(facets()).getByRole("button", { name: /^Active / });
    expect(active).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(active);
    expect(onStateChange).toHaveBeenLastCalledWith(expect.objectContaining({ filters: [STALE] }));
  });

  it("merges the saved filter and the pressed value into one in filter", () => {
    const onStateChange = vi.fn();
    renderView({
      execution: typeExecution(),
      state: { filters: [savedActive, STALE] },
      onStateChange,
    });

    fireEvent.click(within(facets()).getByRole("button", { name: "Draft 9" }));
    expect(onStateChange).toHaveBeenLastCalledWith(
      expect.objectContaining({
        filters: [{ field: STATUS, op: "in", values: ["active", "draft"] }, STALE],
      }),
    );
  });
});

describe("lifecycle facets over several filters on one field", () => {
  // The server requires every filter to match, so only values both allow apply.
  const both: Filter[] = [
    { field: STATUS, op: "in", values: ["active", "draft"] },
    { field: STATUS, op: "in", values: ["draft", "done"] },
  ];

  it("presses only the values every filter allows", () => {
    renderView({ execution: typeExecution(), state: { filters: both } });

    expect(within(facets()).getByRole("button", { name: "Draft 9" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(within(facets()).getByRole("button", { name: /^Active / })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
  });

  it("adds a value to the values every filter allows instead of their union", () => {
    const onStateChange = vi.fn();
    renderView({ execution: typeExecution(), state: { filters: both }, onStateChange });

    fireEvent.click(within(facets()).getByRole("button", { name: /^Active / }));
    expect(onStateChange).toHaveBeenLastCalledWith(
      expect.objectContaining({
        filters: [{ field: STATUS, op: "in", values: ["draft", "active"] }],
      }),
    );
  });
});

describe("type collection facets under their own filters", () => {
  it("forgets the previous collection's lifecycle values when another view shows", () => {
    const { rerender } = render(typeView());
    const active: Filter = { field: STATUS, op: "in", values: ["active"] };

    rerender(
      typeView({
        view: { ...baseView, id: "plans.default" },
        state: { filters: [active] },
        execution: executedWith(
          { filters: [active] },
          {
            stats: {
              total: 5,
              issueCount: 0,
              staleCount: 0,
              lifecycle: [{ value: "active", count: 5 }],
            },
          },
        ),
      }),
    );

    expect(within(facets()).getByRole("button", { name: "Active 5" })).toBeVisible();
    expect(within(facets()).queryByRole("button", { name: "Draft 0" })).toBeNull();
  });
  it("keeps lifecycle values from the unfiltered collection so the reader can add another", () => {
    const onStateChange = vi.fn();
    const { rerender } = render(typeView({ onStateChange }));
    const active: Filter = { field: STATUS, op: "in", values: ["active"] };

    rerender(
      typeView({
        onStateChange,
        state: { filters: [active] },
        execution: executedWith(
          { filters: [active] },
          {
            stats: {
              total: 31,
              issueCount: 0,
              staleCount: 3,
              lifecycle: [{ value: "active", count: 31 }],
            },
          },
        ),
      }),
    );

    const draft = within(facets()).getByRole("button", { name: "Draft 0" });
    expect(draft).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(draft);
    expect(onStateChange).toHaveBeenLastCalledWith(
      expect.objectContaining({
        filters: [{ field: STATUS, op: "in", values: ["active", "draft"] }],
      }),
    );
  });

  it("keeps implementing types from the unfiltered collection", () => {
    const kinds = [
      { value: "ProductSpec", count: 3 },
      { value: "DesignSpec", count: 2 },
    ];

    const product: Filter = { field: "resolvedType", op: "in", values: ["ProductSpec"] };

    const { rerender } = render(
      typeView({
        execution: typeExecution({ stats: { total: 5, issueCount: 0, staleCount: 0, kinds } }),
      }),
    );

    rerender(
      typeView({
        state: { filters: [product] },
        execution: executedWith(
          { filters: [product] },
          {
            stats: { total: 3, issueCount: 0, staleCount: 0, kinds: [kinds[0]] },
          },
        ),
      }),
    );

    expect(within(facets()).getByRole("button", { name: "Product spec 3" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(within(facets()).getByRole("button", { name: "Design spec 0" })).toBeVisible();
  });

  it("never hides a facet the reader has active, even when nothing matches", () => {
    const draft: Filter = { field: STATUS, op: "in", values: ["draft"] };
    const issues: Filter = { field: "hasIssues", op: "eq", value: "true" };

    renderView({
      state: { filters: [draft, STALE, issues] },
      execution: executedWith(
        { filters: [draft, STALE, issues] },
        {
          rows: [],
          stats: { total: 0, issueCount: 0, staleCount: 0, lifecycle: [] },
        },
      ),
    });

    for (const name of ["Draft 0", "Unchanged 30d 0", "Issues 0"]) {
      expect(within(facets()).getByRole("button", { name })).toHaveAttribute(
        "aria-pressed",
        "true",
      );
    }
  });
});
