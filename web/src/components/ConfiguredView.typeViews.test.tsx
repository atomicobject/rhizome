import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ViewTableRow } from "../api/types";
import { jsonReply, withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { ConfiguredView } from "./ConfiguredView";
import {
  baseView,
  execution,
  firstRow,
  renderView,
  secondRow,
} from "./ConfiguredView.testFixtures";
import { monthKey, RailHarness, STATUS, typeExecution } from "./ConfiguredView.typeFixtures";

const http = withFakeFetch();

describe("ConfiguredView type collections", () => {
  it("summarizes the collection from statistics and toggles facet filters", () => {
    const onStateChange = vi.fn();
    const { rerender } = renderView({ execution: typeExecution(), onStateChange });

    const facets = screen.getByRole("group", { name: "Collection summary" });
    // Counts come from every matching row, not the two loaded rows. Without
    // schema labels the type reads as its humanized name.
    expect(facets).toHaveTextContent("40 product spec");
    expect(within(facets).getByRole("button", { name: "Active 31" })).toBeVisible();
    expect(within(facets).getByRole("button", { name: "Draft 9" })).toBeVisible();
    expect(within(facets).queryByRole("button", { name: /Archived/ })).toBeNull();
    expect(within(facets).getByRole("button", { name: "Unchanged 30d 3" })).toBeVisible();
    expect(facets).toHaveTextContent("✓ no issues");

    const gap = within(facets).getByRole("button", { name: "ID empty 12/40" });
    expect(gap).toHaveAttribute("title", "Specs are cited by id.");
    expect(gap).not.toHaveClass("is-gap", "is-risk");

    const age = within(facets).getByRole("button", { name: "Unchanged 30d 3" });

    expect(age).not.toHaveClass("is-gap", "is-risk");
    expect(age).toHaveAttribute("title", "In an active stage and unchanged for 30 days");

    fireEvent.click(within(facets).getByRole("button", { name: "Active 31" }));
    expect(onStateChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ filters: [{ field: STATUS, op: "in", values: ["active"] }] }),
    );

    fireEvent.click(gap);
    expect(onStateChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ filters: [{ field: "frontmatter.id", op: "missing" }] }),
    );

    // Stale is the server's rule, the one stats.staleCount counts by.
    fireEvent.click(within(facets).getByRole("button", { name: "Unchanged 30d 3" }));
    expect(onStateChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ filters: [{ field: "stale", op: "eq", value: "true" }] }),
    );

    // An applied facet reads as pressed, shows as a filter chip, and clicking it again removes it.
    rerender(
      <ConfiguredView
        view={baseView}
        execution={typeExecution()}
        loading={false}
        error={null}
        state={{ filters: [{ field: STATUS, op: "in", values: ["active"] }] }}
        onStateChange={onStateChange}
        onRefresh={() => {}}
        onOpenRow={() => {}}
      />,
    );
    const active = screen.getByRole("button", { name: "Active 31" });
    expect(active).toHaveAttribute("aria-pressed", "true");
    expect(
      screen.getByRole("button", { name: /Remove filter Status is any of Active/ }),
    ).toBeVisible();
    fireEvent.click(active);
    expect(onStateChange).toHaveBeenLastCalledWith(expect.objectContaining({ filters: [] }));
  });

  it("shows the primary date span, monthly spark, and recent month counts", () => {
    renderView({
      execution: typeExecution({
        stats: {
          total: 12,
          issueCount: 2,
          staleCount: 0,
          dates: {
            field: "date",
            first: "2025-05-04",
            last: new Date().toISOString().slice(0, 10),
            months: [
              { value: "2025-05", count: 7 },
              { value: monthKey(-1), count: 4 },
              { value: monthKey(0), count: 1 },
            ],
          },
        },
      }),
    });

    const facets = screen.getByRole("group", { name: "Collection summary" });
    expect(facets).toHaveTextContent("May 2025 – now");
    expect(facets).toHaveTextContent("1 this month · 4 last");
    expect(within(facets).getByRole("img", { name: /Records per month/ })).toBeVisible();
    expect(within(facets).getByRole("button", { name: "Issues 2" })).toBeVisible();
  });

  it("hides columns no matching row fills and lists them in the Columns menu", () => {
    renderView({
      execution: typeExecution({
        stats: {
          total: 2,
          issueCount: 0,
          staleCount: 0,
          fields: [{ field: "frontmatter.id", filled: 0 }],
        },
      }),
    });

    expect(screen.queryByRole("columnheader", { name: "ID" })).toBeNull();
    const columns = screen.getByRole("button", { name: "Columns" });
    expect(columns).toHaveTextContent("1 hidden empty");

    fireEvent.click(columns);
    const menu = screen.getByRole("group", { name: "Visible columns" });
    expect(within(menu).getByText("hidden empty")).toBeVisible();
    fireEvent.click(within(menu).getByLabelText(/ID/));
    expect(screen.getByRole("columnheader", { name: "ID" })).toBeVisible();
  });

  it("shows the summary as a second line and remembers the Rows choice", () => {
    const { unmount } = renderView({ execution: typeExecution(), vaultKey: "vault" });

    expect(screen.getByText("Alpha defines the first contract.")).toBeVisible();
    fireEvent.change(screen.getByLabelText("Rows"), { target: { value: "one-line" } });
    expect(screen.queryByText("Alpha defines the first contract.")).toBeNull();
    unmount();

    renderView({ execution: typeExecution(), vaultKey: "vault" });
    expect(screen.getByLabelText("Rows")).toHaveValue("one-line");
    expect(screen.queryByText("Alpha defines the first contract.")).toBeNull();
  });

  it("defaults row density from the view's YAML", () => {
    renderView({
      view: {
        ...baseView,
        variants: { table: { ...baseView.variants.table, density: "one-line" } },
      },
      execution: typeExecution(),
    });

    expect(screen.getByLabelText("Rows")).toHaveValue("one-line");
    expect(screen.queryByText("Alpha defines the first contract.")).toBeNull();
  });

  it("renders Changed as a relative age with the absolute time on hover", () => {
    const now = Date.now();
    renderView({
      execution: typeExecution({}, [{ ...firstRow, updatedAt: Math.floor(now / 1000) - 4 * 3600 }]),
    });

    const changed = screen.getByText("4h");
    expect(changed.tagName).toBe("TIME");
    expect(changed).toHaveAttribute("title");
  });

  it("shows the selected row as a record in the rail and edits it through the edit session", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    const onOpenRow = vi.fn();
    render(
      <RailHarness>
        <ConfiguredView
          view={baseView}
          execution={typeExecution()}
          loading={false}
          error={null}
          state={{}}
          onStateChange={() => {}}
          onRefresh={() => {}}
          onOpenRow={onOpenRow}
          onStageOps={onStageOps}
        />
      </RailHarness>,
    );

    // The table holds the rail open, so selecting a row never reflows the table.
    const rail = screen.getByRole("complementary", { name: "Rail" });
    expect(rail).toHaveAttribute("data-open", "true");
    expect(rail).toHaveTextContent("Select a row to see its record here.");

    const row = screen.getByRole("button", { name: "Open Alpha Spec" }).closest("tr")!;
    fireEvent.click(row.cells[2]);
    expect(rail).toHaveAttribute("data-open", "true");

    const record = within(rail).getByRole("article", { name: "Selected record Alpha Spec" });
    expect(within(record).getByRole("heading", { name: "Alpha Spec" })).toBeVisible();
    expect(record).toHaveTextContent("Alpha defines the first contract.");

    fireEvent.click(within(record).getByRole("button", { name: "Edit Status" }));
    fireEvent.change(within(record).getByLabelText("Edit Status"), {
      target: { value: "archived" },
    });
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({ kind: "setField", path: "docs/specs/alpha.md", value: "archived" }),
    ]);

    fireEvent.click(within(record).getByRole("button", { name: "Open Alpha Spec" }));
    expect(onOpenRow).toHaveBeenLastCalledWith(expect.objectContaining({ title: "Alpha Spec" }));

    // Double-click and Enter open the note in a tab.
    onOpenRow.mockClear();
    fireEvent.doubleClick(row.cells[3]);
    fireEvent.keyDown(row, { key: "Enter" });
    expect(onOpenRow).toHaveBeenCalledTimes(2);

    // Escape on the row clears the record.
    fireEvent.keyDown(row, { key: "Escape" });
    expect(within(rail).queryByRole("article")).toBeNull();
    expect(rail).toHaveTextContent("Select a row to see its record here.");
  });

  it("lists the record's reverse relations with their lifecycle marks", async () => {
    http
      .json("GET", "/api/v1/ontology/summary", { types: [], interfaces: [] })
      .json("GET", "/api/v1/ontology/types/ProductSpec", {
        count: 2,
        type: {
          name: "ProductSpec",
          fields: [{ name: "efforts", kind: "reverse", typeName: "Effort", reverseField: "spec" }],
        },
      })
      .on("POST", "/api/v1/views/generated.type.Effort.table/execute", (request) => {
        expect(JSON.parse(request.body ?? "{}")).toMatchObject({
          filters: [{ field: "spec", op: "eq", value: "docs/specs/alpha.md" }],
        });

        return jsonReply({
          ...execution([
            {
              ref: { notePath: "efforts/one.md", kind: "NOTE" },
              path: "efforts/one.md",
              title: "Effort one",
              fields: { status: "active" },
            },
          ]),
          // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- TypeProfile.shape is the generated API contract field.
          profile: { shape: "workflow", lifecycleField: "status" },
          capabilities: [
            {
              key: "status",
              sortable: true,
              groupable: true,
              enumValues: [{ value: "active", label: "In flight", tone: "progress" }],
            },
          ],
          pageInfo: { total: 3, offset: 0, first: 8, returned: 1, hasMore: true },
        });
      });

    const result = typeExecution();
    render(
      <RailHarness>
        <ConfiguredView
          view={baseView}
          execution={{ ...result, profile: { ...result.profile!, reverseFields: ["efforts"] } }}
          loading={false}
          error={null}
          state={{}}
          onStateChange={() => {}}
          onRefresh={() => {}}
          onOpenRow={() => {}}
        />
      </RailHarness>,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Open Alpha Spec" }).closest("tr")!.cells[2],
    );
    const reverse = await screen.findByRole("region", { name: "Effort" });
    expect(await within(reverse).findByRole("button", { name: "Effort one" })).toBeVisible();
    expect(reverse).toHaveTextContent("In flight");
    expect(reverse).toHaveTextContent("2 more");
  });

  it("lists the records linking through an inbound neighbor field from the row's card execution", async () => {
    const onOpenRow = vi.fn();
    http
      .json("GET", "/api/v1/ontology/summary", { types: [], interfaces: [] })
      .json("GET", "/api/v1/ontology/types/ProductSpec", {
        count: 2,
        type: {
          name: "ProductSpec",
          fields: [
            {
              name: "citedBy",
              kind: "neighbor",
              typeName: "Effort",
              direction: "inbound",
              list: true,
            },
          ],
        },
      })
      .on("POST", "/api/v1/views/generated.type.ProductSpec.table/execute", (request) => {
        expect(JSON.parse(request.body ?? "{}")).toMatchObject({
          variant: "card",
          filters: [{ field: "path", op: "eq", value: "docs/specs/alpha.md" }],
        });

        return jsonReply(
          execution([
            {
              ...firstRow,
              fields: { ...firstRow.fields, citedBy: 3 },
              relationValues: {
                citedBy: [
                  {
                    value: "[[efforts/one]]",
                    ref: { notePath: "efforts/one.md", kind: "NOTE" },
                    title: "Effort one",
                    status: { value: "active", label: "In flight", tone: "progress" },
                  },
                ],
              },
            },
          ]),
        );
      });

    const result = typeExecution();
    render(
      <RailHarness>
        <ConfiguredView
          view={baseView}
          execution={{ ...result, profile: { ...result.profile!, reverseFields: ["citedBy"] } }}
          loading={false}
          error={null}
          state={{}}
          onStateChange={() => {}}
          onRefresh={() => {}}
          onOpenRow={onOpenRow}
        />
      </RailHarness>,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Open Alpha Spec" }).closest("tr")!.cells[2],
    );
    const linked = await screen.findByRole("region", { name: "Effort" });
    const effort = await within(linked).findByRole("button", { name: "Effort one" });
    expect(linked).toHaveTextContent("In flight");
    expect(linked).toHaveTextContent("2 more");
    expect(linked).not.toHaveTextContent("None yet");

    fireEvent.click(effort);
    expect(onOpenRow).toHaveBeenLastCalledWith(
      expect.objectContaining({ ref: { notePath: "efforts/one.md", kind: "NOTE" } }),
    );
  });

  it("checks rows by checkbox, shift-range, and X, and stages one bulk edit for all of them", () => {
    const third: ViewTableRow = {
      ...secondRow,
      ref: { notePath: "docs/specs/charlie.md", kind: "NOTE" },
      path: "docs/specs/charlie.md",
      title: "Charlie Spec",
    };

    const onStageOps = vi.fn().mockResolvedValue(undefined);
    const result = typeExecution({ groups: [] }, [firstRow, secondRow, third]);
    renderView({ execution: result, onStageOps });

    fireEvent.click(screen.getByRole("checkbox", { name: "Select Alpha Spec" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select Charlie Spec" }), {
      shiftKey: true,
    });
    const bar = screen.getByRole("toolbar", { name: "Edit selected rows" });
    expect(bar).toHaveTextContent("3 selected");

    const bravo = screen.getByRole("button", { name: "Open Bravo Spec" }).closest("tr")!;
    fireEvent.keyDown(bravo, { key: "x" });
    expect(bar).toHaveTextContent("2 selected");

    fireEvent.change(within(bar).getByLabelText("Set Status"), { target: { value: "archived" } });
    expect(onStageOps).toHaveBeenCalledTimes(1);
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        path: "docs/specs/alpha.md",
        field: "specStatus",
        value: "archived",
      }),
      expect.objectContaining({
        path: "docs/specs/charlie.md",
        field: "specStatus",
        value: "archived",
      }),
    ]);

    fireEvent.keyDown(bravo, { key: "Escape" });
    expect(screen.queryByRole("toolbar", { name: "Edit selected rows" })).toBeNull();
  });

  it("adds a link value to every selected row that lacks it", () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);

    const base = typeExecution({ groups: [] }, [
      { ...firstRow, fields: { ...firstRow.fields, areas: ["areas/search.md"] } },
      secondRow,
    ]);

    renderView({
      onStageOps,
      execution: {
        ...base,
        profile: { ...base.profile!, relationFields: ["areas"] },
        capabilities: [
          ...(base.capabilities ?? []),
          {
            key: "areas",
            label: "Areas",
            valueKind: "relation",
            sortable: false,
            groupable: true,
            edit: {
              kind: "node",
              operation: "setLinkField",
              field: "areas",
              list: true,
              candidates: [
                {
                  ref: { notePath: "areas/search.md", kind: "NOTE" },
                  value: "areas/search.md",
                  label: "Search",
                },
                {
                  ref: { notePath: "areas/views.md", kind: "NOTE" },
                  value: "areas/views.md",
                  label: "Views",
                },
              ],
            },
          },
        ],
      },
    });

    fireEvent.click(screen.getByRole("checkbox", { name: "Select all shown rows" }));
    const bar = screen.getByRole("toolbar", { name: "Edit selected rows" });
    fireEvent.change(within(bar).getByLabelText("Add Areas"), {
      target: { value: "areas/search.md" },
    });

    // Alpha already links Search, so only Bravo changes.
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        kind: "setLinkField",
        path: "docs/specs/bravo.md",
        values: ["areas/search.md"],
      }),
    ]);
  });

  it("loads further rows on scroll instead of paging", () => {
    const onStateChange = vi.fn();
    const result = typeExecution();
    renderView({
      execution: { ...result, pageInfo: { ...result.pageInfo, total: 400, hasMore: true } },
      state: { page: { offset: 0, first: 25 } },
      onStateChange,
    });

    expect(screen.queryByRole("button", { name: "Next page" })).toBeNull();
    const wrap = document.querySelector<HTMLElement>(".configured-view__table-wrap")!;
    Object.defineProperties(wrap, {
      scrollHeight: { configurable: true, value: 2000 },
      clientHeight: { configurable: true, value: 600 },
      scrollTop: { configurable: true, value: 100, writable: true },
    });
    fireEvent.scroll(wrap);
    expect(onStateChange).not.toHaveBeenCalled();

    wrap.scrollTop = 1300;
    fireEvent.scroll(wrap);
    expect(onStateChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: { offset: 0, first: 225 } }),
    );
  });

  it("adds sort keys on shift-click", () => {
    const onStateChange = vi.fn();
    renderView({
      execution: typeExecution(),
      state: { sort: [{ field: STATUS, direction: "asc" }] },
      onStateChange,
    });

    fireEvent.click(screen.getByRole("button", { name: "Sort Title ascending" }), {
      shiftKey: true,
    });
    expect(onStateChange).toHaveBeenLastCalledWith(
      expect.objectContaining({
        sort: [
          { field: STATUS, direction: "asc" },
          { field: "title", direction: "asc" },
        ],
      }),
    );
  });
});
