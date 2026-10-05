import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ViewCatalogEntry, ViewExecuteResponse, ViewTableRow } from "../api/types";
import { emptyValidationSummariesReply, withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { ViewTab } from "./ViewTab";
import type { ViewTab as ViewTabModel } from "./useNoteTabs";

const http = withFakeFetch();

const backlog: ViewCatalogEntry = {
  id: "planning.backlog",
  name: "Planning backlog",
  source: { kind: "query_recipe" },
  mount: { kind: "standalone", group: "Planning", order: 1 },
  defaults: { variant: "table" },
  variants: { table: { columns: [{ field: "title", label: "Title" }] } },
  definition: {
    apiVersion: "rhizome.view.v1",
    id: "planning.backlog",
    name: "Planning backlog",
    source: { kind: "query_recipe" },
    mount: { kind: "standalone" },
    variants: { table: { columns: [{ field: "title", label: "Title" }] } },
  },
};

const row: ViewTableRow = {
  ref: { notePath: "planning/next.md", kind: "NOTE" },
  path: "planning/next.md",
  title: "Next planning item",
  updatedAt: 1_713_000_000,
  hasIssues: false,
  fields: {},
};

const executed: ViewExecuteResponse = {
  view: backlog,
  variant: "table",
  state: {},
  capabilities: [],
  columns: backlog.variants.table?.columns ?? [],
  rows: [row],
  pageInfo: { total: 1, offset: 0, first: 25, returned: 1, hasMore: false },
  definitionFingerprint: "definition",
  sourceFingerprint: "source",
  executionFingerprint: "execution",
};

function viewTab(viewId: string, title = viewId): ViewTabModel {
  return { id: `view:${viewId}`, kind: "view", viewId, title };
}

function renderTab(tab: ViewTabModel, onTitle = vi.fn(), onOpenNote = vi.fn()) {
  renderWithQueryClient(
    <ViewTab
      tab={tab}
      active
      editSession={{ session: null, vaultKey: "vault" }}
      onOpenNote={onOpenNote}
      onStageOps={async () => undefined}
      onOpenIssues={vi.fn()}
      onTitle={onTitle}
    />,
  );

  return onTitle;
}

describe("ViewTab", () => {
  it.each(["read-only", "editable", "card"])(
    "preserves the embedded target when opening a %s row or its preview link",
    async (presentation) => {
      const target = "planning/next.md#struct:item-fingerprint";

      const embedded = {
        ...row,
        ref: { ...row.ref, kind: "EMBEDDED" as const, structuralFingerprint: "item-fingerprint" },
        fields: { summary: row.title },
      };

      const card = { title: { field: "summary" }, fields: [] };
      const variant = presentation === "card" ? "card" : "table";

      const view = {
        ...backlog,
        availableVariants: ["table", "card"],
        defaults: { variant },
        variants: { ...backlog.variants, card },
      };

      const onOpenNote = vi.fn();
      http
        .json("GET", "/api/v1/views", { views: [view] })
        .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 })
        .json("POST", "/api/v1/views/planning.backlog/execute", {
          ...executed,
          view,
          variant,
          card,
          columns: [{ field: "summary", label: "Summary" }],
          rows: [embedded],
          capabilities: [
            {
              key: "summary",
              label: "Summary",
              sortable: false,
              groupable: false,
              edit:
                presentation === "editable"
                  ? { kind: "scalar", operation: "setField", field: "summary" }
                  : undefined,
            },
          ],
        })
        .json("GET", "/api/v1/nodes/preview", {
          ref: target,
          path: row.path,
          title: row.title,
          format: "markdown",
          hasIssues: false,
          fields: [
            {
              name: "related",
              label: "Related",
              kind: "link",
              importance: "NORMAL",
              values: [{ text: "This item", target }],
            },
          ],
        })
        .on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply);
      renderTab(viewTab("planning.backlog"), vi.fn(), onOpenNote);

      const title = await screen.findByRole("button", {
        name: presentation === "card" ? "Next planning item" : "Open Next planning item",
      });

      fireEvent.focus(title);
      const previewLink = await screen.findByRole("link", { name: "This item" });
      fireEvent.click(previewLink, { ctrlKey: true });
      expect(onOpenNote).toHaveBeenLastCalledWith(target, "beside");

      // A card title opens on click; a table title selects its row and opens on double-click.
      fireEvent.click(title);

      if (presentation !== "card") fireEvent.doubleClick(title);

      expect(onOpenNote).toHaveBeenLastCalledWith(target, "activate");
    },
  );

  it("renders the configured table for a resolved catalog entry and reports its title", async () => {
    http
      .json("GET", "/api/v1/views", { views: [backlog] })
      .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 })
      .json("POST", "/api/v1/views/planning.backlog/execute", executed)
      .on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply);

    const onTitle = renderTab(viewTab("planning.backlog"));

    expect(await screen.findByText("Next planning item")).toBeVisible();
    expect(onTitle).toHaveBeenCalledWith("view:planning.backlog", "Planning backlog");
  });

  it("opens a native row with its complete embedded node identity", async () => {
    const ref = {
      notePath: "planning/next.md",
      kind: "EMBEDDED",
      nodeId: "task-1",
      fragment: "node:task-1",
      structuralFingerprint: "fingerprint",
    };

    const onOpenNode = vi.fn();
    http
      .json("GET", "/api/v1/views", { views: [backlog], targets: [] })
      .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 })
      .json("POST", "/api/v1/views/planning.backlog/execute", {
        ...executed,
        rows: [{ ...row, ref }],
      })
      .on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply);
    renderWithQueryClient(
      <ViewTab
        tab={viewTab(backlog.id)}
        active
        editSession={{ session: null, vaultKey: "vault" }}
        onOpenNote={vi.fn()}
        onOpenNode={onOpenNode}
        onStageOps={async () => undefined}
        onOpenIssues={vi.fn()}
        onTitle={vi.fn()}
      />,
    );
    const title = await screen.findByText("Next planning item");
    fireEvent.click(title);
    fireEvent.doubleClick(title);
    expect(onOpenNode).toHaveBeenCalledWith(ref, { beside: false });
  });

  it("reports an unknown view id once the catalog has loaded", async () => {
    http
      .json("GET", "/api/v1/views", { views: [] })
      .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 })
      .on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply);

    renderTab(viewTab("planning.retired", "Retired view"));

    expect(await screen.findByText(/This view is no longer available/)).toBeVisible();
  });
});
