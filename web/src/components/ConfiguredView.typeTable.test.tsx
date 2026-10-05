import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useState } from "react";

import type {
  OntologyEditOp,
  ViewExecuteResponse,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";
import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { nativePreferenceScope } from "../viewPreferences/native";
import { getPreferenceStore } from "../viewPreferences/store";
import { baseView, secondRow } from "./ConfiguredView.testFixtures";
import {
  executedWith,
  RailHarness,
  STATUS,
  typeExecution,
  typeView as view,
} from "./ConfiguredView.typeFixtures";

function stats(filled: Record<string, number>): ViewExecuteResponse["stats"] {
  return {
    total: 2,
    issueCount: 0,
    staleCount: 0,
    fields: Object.entries(filled).map(([field, count]) => ({ field, filled: count })),
  };
}

describe("type collection table columns", () => {
  it("keeps the column of a field the reader filters on, though no matching row fills it", () => {
    render(
      view({
        execution: typeExecution({ stats: stats({ "frontmatter.id": 0 }) }),
        state: { filters: [{ field: "frontmatter.id", op: "missing" }] },
      }),
    );

    expect(screen.getByRole("columnheader", { name: "ID" })).toBeVisible();
  });

  it("re-shows one empty column without freezing whether the others hide", () => {
    const { rerender } = render(
      view({
        execution: typeExecution({ stats: stats({ "frontmatter.id": 0, updatedAt: 0 }) }),
        vaultKey: "vault",
      }),
    );

    expect(screen.queryByRole("columnheader", { name: "Updated" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Columns" }));
    const menu = screen.getByRole("group", { name: "Visible columns" });
    fireEvent.click(within(menu).getByLabelText(/^ID/));
    expect(screen.getByRole("columnheader", { name: "ID" })).toBeVisible();
    expect(screen.queryByRole("columnheader", { name: "Updated" })).toBeNull();

    // Updated is still a default column, so it shows as soon as rows fill it.
    rerender(
      view({
        execution: typeExecution({ stats: stats({ "frontmatter.id": 0, updatedAt: 2 }) }),
        vaultKey: "vault",
      }),
    );
    expect(screen.getByRole("columnheader", { name: "Updated" })).toBeVisible();
    expect(screen.getByRole("columnheader", { name: "ID" })).toBeVisible();

    // Hiding the re-shown column returns it to the hidden-empty list.
    fireEvent.click(within(menu).getByLabelText(/^ID/));
    expect(screen.queryByRole("columnheader", { name: "ID" })).toBeNull();
    expect(screen.getByRole("button", { name: "Columns" })).toHaveTextContent("1 hidden empty");
  });
});

describe("type collection table rows", () => {
  it("opens a row on a double-click of its title or a plain cell, not of its controls", () => {
    const onOpenRow = vi.fn();
    render(view({ onOpenRow, onStageOps: vi.fn() }));
    const alphaRow = screen.getByRole("button", { name: "Open Alpha Spec" }).closest("tr")!;

    fireEvent.doubleClick(within(alphaRow).getByRole("button", { name: "Edit Status" }));
    expect(onOpenRow).not.toHaveBeenCalled();
    fireEvent.doubleClick(alphaRow.cells[3]);
    expect(onOpenRow).toHaveBeenCalledWith(
      expect.objectContaining({ title: "Alpha Spec" }),
      "activate",
    );
  });

  it("reads Changed in the same steps as board cards: hours under two days, days under two months", () => {
    const now = Math.floor(Date.now() / 1000);

    const rows = [
      { ...specRow("alpha", "active"), updatedAt: now - 30 * 3600 },
      { ...specRow("bravo", "active"), updatedAt: now - 45 * 86_400 },
      { ...specRow("charlie", "active"), updatedAt: now - 400 * 86_400 },
    ];

    render(view({ execution: typeExecution({ groups: [] }, rows) }));

    expect(screen.getAllByRole("time").map((time) => time.textContent)).toEqual([
      "30h",
      "45d",
      "13mo",
    ]);
  });

  it("selects a row from its title and opens it on Enter, double-click, or Cmd-click", () => {
    const onOpenRow = vi.fn();
    render(<RailHarness>{view({ onOpenRow })}</RailHarness>);
    const rail = screen.getByRole("complementary", { name: "Rail" });
    const title = screen.getByRole("button", { name: "Open Alpha Spec" });

    fireEvent.click(title);
    expect(onOpenRow).not.toHaveBeenCalled();
    expect(within(rail).getByRole("article", { name: "Selected record Alpha Spec" })).toBeVisible();

    const alpha = expect.objectContaining({ title: "Alpha Spec" });
    fireEvent.keyDown(title, { key: "Enter" });
    expect(onOpenRow).toHaveBeenLastCalledWith(alpha, "activate");
    fireEvent.doubleClick(title);
    expect(onOpenRow).toHaveBeenCalledTimes(2);
    fireEvent.click(title, { metaKey: true });
    expect(onOpenRow).toHaveBeenLastCalledWith(alpha, "beside");
    expect(onOpenRow).toHaveBeenCalledTimes(3);
  });
});

function specRow(name: string, status: string): ViewTableRow {
  return {
    ...secondRow,
    ref: { notePath: `docs/specs/${name}.md`, kind: "NOTE" },
    path: `docs/specs/${name}.md`,
    title: `${name} spec`,
    fields: { frontmatter: { id: name, "spec-status": status } },
  };
}

function statusGroup(value: string, rowStart: number, rowEnd: number, collapsed = false) {
  return {
    field: STATUS,
    key: `${STATUS}:${value}`,
    value,
    label: value,
    count: rowEnd - rowStart,
    depth: 0,
    rowStart,
    rowEnd,
    collapsedByDefault: collapsed,
  };
}

const bar = () => screen.getByRole("toolbar", { name: "Edit selected rows" });

describe("type collection table selection", () => {
  it("selects a shift-range over the rendered rows only, skipping collapsed groups", () => {
    const rows = [
      specRow("alpha", "active"),
      specRow("bravo", "active"),
      specRow("charlie", "archived"),
      specRow("delta", "archived"),
      specRow("echo", "draft"),
    ];

    const groups = [
      statusGroup("active", 0, 2),
      statusGroup("archived", 2, 4, true),
      statusGroup("draft", 4, 5),
    ];

    render(view({ execution: typeExecution({ groups }, rows), onStageOps: vi.fn() }));

    fireEvent.click(screen.getByRole("checkbox", { name: "Select alpha spec" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select echo spec" }), { shiftKey: true });
    expect(bar()).toHaveTextContent("3 selected");

    fireEvent.click(screen.getByRole("checkbox", { name: "Select all shown rows" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select all shown rows" }));
    expect(bar()).toHaveTextContent("3 selected");
  });

  it("unchecks checked rows that a filter hides", () => {
    const rows = [
      specRow("alpha", "active"),
      specRow("bravo", "active"),
      specRow("charlie", "draft"),
    ];

    const props = { onStageOps: vi.fn() };
    const { rerender } = render(view({ ...props, execution: typeExecution({ groups: [] }, rows) }));

    fireEvent.click(screen.getByRole("checkbox", { name: "Select alpha spec" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select charlie spec" }));
    expect(bar()).toHaveTextContent("2 selected");

    rerender(view({ ...props, execution: typeExecution({ groups: [] }, rows.slice(0, 2)) }));
    expect(bar()).toHaveTextContent("1 selected");

    // Clearing the filter does not bring the hidden row's check back.
    rerender(view({ ...props, execution: typeExecution({ groups: [] }, rows) }));
    expect(bar()).toHaveTextContent("1 selected");
  });
});

describe("type collection bulk edits", () => {
  it("clears the checked rows on Escape from the bulk bar and returns focus to the rows", async () => {
    render(view({ execution: typeExecution({ groups: [] }), onStageOps: vi.fn() }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select all shown rows" }));

    // Checked rows are announced by their checkboxes, not as selected table rows.
    const alpha = screen.getByRole("button", { name: "Open Alpha Spec" }).closest("tr")!;
    expect(alpha).not.toHaveAttribute("aria-selected");

    const set = within(bar()).getByLabelText("Set Status");
    set.focus();
    fireEvent.keyDown(set, { key: "Escape" });
    expect(screen.queryByRole("toolbar", { name: "Edit selected rows" })).toBeNull();
    await waitFor(() => expect(alpha).toHaveFocus());
  });

  it("says why a bulk edit could not stage", async () => {
    const onStageOps = vi.fn().mockRejectedValue(new Error("the edit session is conflicted"));
    render(view({ execution: typeExecution({ groups: [] }), onStageOps }));

    fireEvent.click(screen.getByRole("checkbox", { name: "Select all shown rows" }));
    fireEvent.change(within(bar()).getByLabelText("Set Status"), { target: { value: "archived" } });

    expect(await within(bar()).findByRole("alert")).toHaveTextContent(
      "Could not set Status on 2 rows: the edit session is conflicted",
    );
  });
});

function StagingHarness({
  rows,
  onStaged,
}: {
  rows: ViewTableRow[];
  onStaged: (ops: OntologyEditOp[]) => void;
}) {
  const [ops, setOps] = useState<OntologyEditOp[]>([]);

  const owners: ViewFieldCapability = {
    key: "owners",
    label: "Owners",
    sortable: false,
    groupable: false,
    filterOps: [],
    edit: {
      kind: "node",
      operation: "setLinkField",
      field: "owners",
      list: true,
      candidates: ["Alice", "Bob"].map((name) => ({
        ref: { notePath: `people/${name.toLowerCase()}.md`, kind: "NOTE" },
        value: `[[${name}]]`,
        label: name,
        path: `people/${name.toLowerCase()}.md`,
      })),
    },
  };

  const base = typeExecution({ groups: [] }, rows);

  return view({
    execution: {
      ...base,
      capabilities: [...(base.capabilities ?? []), owners],
      profile: { ...base.profile!, relationFields: ["owners"] },
    },
    editSession: {
      sessionId: "session-1",
      status: "dirty",
      hasUncommittedChanges: ops.length > 0,
      createdAt: "2026-05-06T00:00:00Z",
      updatedAt: "2026-05-06T00:00:01Z",
      ops,
    },
    // Same-target edits replace the earlier op, as the edit session does.
    onStageOps: async (staged) => {
      onStaged(staged);
      setOps((current) => [
        ...current.filter((op) => !staged.some((next) => next.id === op.id)),
        ...staged,
      ]);
    },
  });
}

describe("type collection bulk link edits", () => {
  it("keeps an earlier staged link when another is added before the rows refresh", async () => {
    const onStaged = vi.fn();
    render(<StagingHarness rows={[specRow("alpha", "active")]} onStaged={onStaged} />);
    const add = () => within(bar()).getByLabelText("Add Owners");

    fireEvent.click(screen.getByRole("checkbox", { name: "Select all shown rows" }));
    fireEvent.change(add(), { target: { value: "[[Alice]]" } });
    await waitFor(() => expect(onStaged).toHaveBeenCalledTimes(1));
    fireEvent.change(add(), { target: { value: "[[Bob]]" } });
    await waitFor(() => expect(onStaged).toHaveBeenCalledTimes(2));

    expect(onStaged.mock.calls[1][0]).toEqual([
      expect.objectContaining({ field: "owners", values: ["[[Alice]]", "[[Bob]]"] }),
    ]);
  });
});

describe("type collection table sorting", () => {
  it("marks the sort the view ran with until the reader picks one, and adds to it", () => {
    const onStateChange = vi.fn();
    const changed = { field: "updatedAt", direction: "desc" as const };
    render(view({ onStateChange, execution: executedWith({ sort: [changed] }) }));

    expect(screen.getByRole("columnheader", { name: "Updated" })).toHaveAttribute(
      "aria-sort",
      "descending",
    );
    fireEvent.click(screen.getByRole("button", { name: "Sort Title ascending" }), {
      shiftKey: true,
    });
    expect(onStateChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ sort: [changed, { field: "title", direction: "asc" }] }),
    );
  });
});

describe("type collection column resizing", () => {
  withFakeFetch();

  const storedWidths = () => {
    const values = getPreferenceStore(nativePreferenceScope(baseView), "vault").getSnapshot()
      .values;

    return Object.hasOwn(values, "native.widths") ? JSON.stringify(values["native.widths"]) : null;
  };

  function statusHeader() {
    const header = screen.getByRole("columnheader", { name: "Status" });
    vi.spyOn(header, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 120, 26));

    return header;
  }

  function drag(handle: Element, to: number) {
    fireEvent.pointerDown(handle, { clientX: 100 });
    act(() => {
      window.dispatchEvent(new MouseEvent("pointermove", { clientX: to }));
    });
  }

  it("sizes the column while dragging and keeps the width on release", () => {
    render(view({ vaultKey: "vault" }));
    const header = statusHeader();

    drag(screen.getByRole("separator", { name: "Resize Status" }), 160);
    expect(header).toHaveStyle({ width: "180px" });
    expect(storedWidths()).toBeNull();

    act(() => {
      window.dispatchEvent(new MouseEvent("pointerup"));
    });
    expect(header).toHaveStyle({ width: "180px" });
    expect(JSON.parse(storedWidths() ?? "{}")).toEqual({ "frontmatter.spec-status": 180 });
  });

  it("puts the width back when the pointer is canceled", () => {
    render(view({ vaultKey: "vault" }));
    const header = statusHeader();

    drag(screen.getByRole("separator", { name: "Resize Status" }), 160);
    act(() => {
      window.dispatchEvent(new MouseEvent("pointercancel"));
      window.dispatchEvent(new MouseEvent("pointermove", { clientX: 220 }));
    });

    expect(header.style.width).toBe("");
    expect(storedWidths()).toBeNull();
  });

  it("resizes from the keyboard with the arrow keys on the focused handle", () => {
    render(view({ vaultKey: "vault" }));
    const header = statusHeader();
    const handle = screen.getByRole("separator", { name: "Resize Status" });

    handle.focus();
    fireEvent.keyDown(handle, { key: "ArrowRight" });
    expect(header).toHaveStyle({ width: "136px" });
    expect(handle).toHaveAttribute("aria-valuenow", "136");
  });
});
