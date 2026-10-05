import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { isJsonObject } from "../api/parse";
import type {
  ViewCatalogEntry,
  ViewExecuteRequest,
  ViewExecuteResponse,
  ViewSaveRequest,
} from "../api/types";
import {
  decodeJSONBody,
  deferredReply,
  emptyValidationSummariesReply,
  jsonReply,
  withFakeFetch,
} from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { ideaBoard, ideaView } from "./ConfiguredView.profileFixtures";
import { baseView, execution, renderView } from "./ConfiguredView.testFixtures";
import { ViewTab } from "./ViewTab";

/** What Save sends (SPEC-0112): settings the reader left alone stay as the file has them. */

const authored: ViewCatalogEntry = {
  ...ideaView,
  id: "ip-ideas",
  generated: false,
  availableVariants: ["table", "kanban"],
  defaults: { variant: "kanban", sort: [{ field: "title", direction: "asc" }] },
  variants: {
    table: { ...ideaView.variants.table, density: "one-line" },
    kanban: { columnField: "status", laneField: "priority" },
  },
  definition: {
    ...ideaView.definition,
    filterPresets: [
      {
        id: "mine",
        label: "Mine",
        filters: [{ field: "owner", op: "eq", value: "people/drew.md" }],
      },
    ],
  },
};

const SAVE_PATH = `/api/v1/views/${authored.id}/save`;

function isSaveRequest(value: unknown): value is ViewSaveRequest {
  return isJsonObject(value) && isJsonObject(value.state);
}

/** An execution of `view` whose executed state is `executed`. */
function executed(
  view: ViewCatalogEntry,
  variant: string,
  state: ViewExecuteRequest,
): ViewExecuteResponse {
  const result = { ...ideaBoard(), view, variant };
  // SAFETY: the generated execution state has no assignable concrete shape.
  result.state = { ...result.state, ...state } as typeof result.state;

  return result;
}

describe("Save view request", () => {
  const http = withFakeFetch();

  async function save() {
    http.json("POST", SAVE_PATH, { id: authored.id, path: "views/ip.yaml", created: false });
    fireEvent.click(screen.getByRole("button", { name: /^Save view/ }));
    fireEvent.click(screen.getByRole("button", { name: "Write view YAML" }));
    await waitFor(() => expect(http.requests("POST", SAVE_PATH)).toHaveLength(1));

    return decodeJSONBody(http.requests("POST", SAVE_PATH)[0], isSaveRequest).state;
  }

  it("leaves an authored view's density, columns, grouping, and board fields alone after a sort change", async () => {
    renderView({
      view: authored,
      // The board groups by its column field; that grouping is not the view's.
      execution: executed(authored, "kanban", { group: { field: "status" } }),
      state: { variant: "kanban", sort: [{ field: "title", direction: "desc" }] },
    });

    fireEvent.click(screen.getByRole("button", { name: /^Save view/ }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Sort");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    const state = await save();

    expect(state.sort).toEqual([{ field: "title", direction: "desc" }]);

    for (const key of ["density", "laneField", "columnField", "columns", "group"]) {
      expect(state).not.toHaveProperty(key);
    }
  });

  // Execution lowercases sort directions and fills empty ones; the file keeps its own.
  it.each(["DESC", ""])(
    "keeps the file's sort, unlisted, when execution normalizes direction %j",
    async (direction) => {
      const sorted = {
        ...authored,
        defaults: { variant: "table", sort: [{ field: "title", direction }] },
      };

      renderView({
        view: sorted,
        execution: executed(sorted, "table", {
          sort: [{ field: "title", direction: direction.toLowerCase() || "asc" }],
        }),
        state: { variant: "table" },
      });

      fireEvent.click(screen.getByRole("button", { name: /^Save view/ }));
      expect(screen.getByRole("dialog")).not.toHaveTextContent("Sort");
      fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

      expect((await save()).sort).toEqual(sorted.defaults.sort);
    },
  );

  it("sends a board field or grouping only when the reader changed it", async () => {
    renderView({
      view: authored,
      execution: executed(authored, "kanban", { group: { field: "status" } }),
      state: { variant: "kanban", laneField: "none", group: { field: "priority" } },
    });

    const state = await save();

    expect(state.laneField).toBe("none");
    expect(state.group).toEqual({ field: "priority" });
    expect(state).not.toHaveProperty("columnField");
  });

  it("sends the executed filters with an active preset once, and counts the preset as a change", async () => {
    const manual = { field: "status", op: "eq", value: "captured" };
    const preset = { field: "owner", op: "eq", value: "people/drew.md" };

    renderView({
      view: authored,
      execution: executed(authored, "kanban", { filterPreset: "mine", filters: [manual, preset] }),
      state: { variant: "kanban", filterPreset: "mine", filters: [manual] },
    });

    expect((await save()).filters).toEqual([manual, preset]);
  });

  it("marks a preset alone as unsaved", () => {
    renderView({
      view: authored,
      execution: executed(authored, "kanban", { filterPreset: "mine" }),
      state: { variant: "kanban", filterPreset: "mine" },
    });

    expect(screen.getByRole("button", { name: "Save view, unsaved changes" })).toBeVisible();
  });

  it("saves Group: None as an explicit no-grouping", async () => {
    const grouped = { ...authored, defaults: { variant: "table", group: { field: "status" } } };

    renderView({
      view: grouped,
      execution: executed(grouped, "table", { group: { field: "status" } }),
      state: { variant: "table", group: { field: "" } },
    });

    expect((await save()).group).toEqual({ field: "none" });
  });

  it("reads a saved no-grouping back as Group: None without an unsaved change", () => {
    const ungrouped = { ...authored, defaults: { variant: "table", group: { field: "none" } } };

    renderView({
      view: ungrouped,
      execution: executed(ungrouped, "table", { group: { field: "none" } }),
      state: { variant: "table", group: { field: "" } },
    });

    expect(screen.getByLabelText("Group by")).toHaveDisplayValue("None");
    expect(screen.getByRole("button", { name: "Save view" })).toBeVisible();
  });

  it("saves no columns when only empty columns hide", async () => {
    const result = executed(authored, "table", {});
    result.stats = {
      total: 3,
      issueCount: 0,
      staleCount: 0,
      fields: [{ field: "frontmatter.id", filled: 0 }],
    };
    renderView({
      view: authored,
      execution: result,
      state: { variant: "table", search: "agents" },
    });

    expect(screen.queryByRole("columnheader", { name: /^ID/ })).toBeNull();
    expect(await save()).not.toHaveProperty("columns");
  });

  it("keeps a column hidden only for being empty in the columns the reader saves", async () => {
    const result = executed(authored, "table", {});
    result.stats = {
      total: 3,
      issueCount: 0,
      staleCount: 0,
      fields: [{ field: "frontmatter.id", filled: 0 }],
    };
    renderView({ view: authored, execution: result, state: { variant: "table" } });

    // The reader hides Updated while the empty ID column is hidden.
    fireEvent.click(screen.getByRole("button", { name: /^Columns/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Updated" }));

    expect((await save()).columns?.map((column) => column.field)).toEqual([
      "title",
      "frontmatter.spec-status",
      "frontmatter.id",
    ]);
  });

  it("saves a preset picked while the previous execution is still on screen", async () => {
    const preset = { field: "frontmatter.spec-status", op: "eq", value: "active" };

    const view: ViewCatalogEntry = {
      ...baseView,
      definition: {
        ...baseView.definition,
        filterPresets: [{ id: "active", label: "Active", filters: [preset] }],
      },
    };

    const execute = `/api/v1/views/${view.id}/execute`;
    const save = `/api/v1/views/${view.id}/save`;
    const narrowed = deferredReply<ViewExecuteResponse>();
    const replies = [() => jsonReply({ ...execution(), view }), () => narrowed.promise];
    http
      .json("GET", "/api/v1/views", { views: [view] })
      .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 })
      .on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply)
      .on("POST", execute, () => replies[http.count("POST", execute) - 1]())
      .json("POST", save, { id: view.id, path: "views/specs.yaml", created: false });

    renderWithQueryClient(
      <ViewTab
        tab={{ id: `view:${view.id}`, kind: "view", viewId: view.id, title: "Specs" }}
        active
        editSession={{ session: null, vaultKey: "vault" }}
        onOpenNote={vi.fn()}
        onStageOps={async () => undefined}
        onOpenIssues={vi.fn()}
        onTitle={vi.fn()}
      />,
    );

    fireEvent.click(await screen.findByRole("button", { name: "Active" }));
    await waitFor(() => expect(http.count("POST", execute)).toBe(2));
    // The preset's execution has not answered; Save still writes what the reader picked.
    fireEvent.click(screen.getByRole("button", { name: "Save view, unsaved changes" }));
    fireEvent.click(screen.getByRole("button", { name: "Write view YAML" }));
    await waitFor(() => expect(http.requests("POST", save)).toHaveLength(1));

    expect(decodeJSONBody(http.requests("POST", save)[0], isSaveRequest).state.filters).toEqual([
      preset,
    ]);
  });
});
