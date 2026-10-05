import { fireEvent, screen } from "@testing-library/react";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { afterEach, vi } from "vitest";

import type {
  OntologyEditOp,
  OntologyEditSessionResponse,
  ValidationScope,
  ViewCatalogEntry,
  ViewExecuteRequest,
  ViewExecuteResponse,
  ViewTableRow,
} from "../api/types";
import { ConfiguredView, type ViewErrorInfo } from "./ConfiguredView";
import type { ComponentProps } from "react";
import { ConfiguredViewTable } from "./ConfiguredViewTable";

afterEach(() => {
  vi.unstubAllGlobals();
  window.localStorage.clear();
  window.sessionStorage.clear();
});

export const baseView: ViewCatalogEntry = {
  id: "specs.default",
  name: "Spec Table",
  source: { kind: "ontology_type", type: "ProductSpec" },
  mount: { kind: "type", type: "ProductSpec", default: true },
  defaults: { variant: "table" },
  variants: {
    table: {
      columns: [
        { field: "title", label: "Title" },
        { field: "frontmatter.spec-status", label: "Status" },
        { field: "frontmatter.id", label: "ID" },
        { field: "updatedAt", label: "Updated" },
      ],
    },
  },
  definition: {
    apiVersion: "rhizome.view.v1",
    id: "specs.default",
    name: "Spec Table",
    source: { kind: "ontology_type", type: "ProductSpec" },
    mount: { kind: "type", type: "ProductSpec", default: true },
    variants: {
      table: {
        columns: [
          { field: "title", label: "Title" },
          { field: "frontmatter.spec-status", label: "Status" },
          { field: "frontmatter.id", label: "ID" },
          { field: "updatedAt", label: "Updated" },
        ],
      },
    },
  },
};

export const firstRow: ViewTableRow = {
  ref: { notePath: "docs/specs/alpha.md", kind: "NOTE" },
  path: "docs/specs/alpha.md",
  title: "Alpha Spec",
  resolvedType: "ProductSpec",
  updatedAt: 1_713_000_000,
  tags: ["planning"],
  fields: {
    frontmatter: {
      id: "SPEC-0001",
      "spec-status": "active",
    },
  },
};

export const secondRow: ViewTableRow = {
  ref: { notePath: "docs/specs/bravo.md", kind: "NOTE" },
  path: "docs/specs/bravo.md",
  title: "Bravo Spec",
  resolvedType: "ProductSpec",
  fields: {
    frontmatter: {
      id: "SPEC-0002",
      "spec-status": "draft",
    },
  },
};

export function execution(rows: ViewTableRow[] = [firstRow, secondRow]): ViewExecuteResponse {
  return {
    view: baseView,
    variant: "table",
    // SAFETY: the generated `ViewExecutionState` intersects ViewExecuteRequest
    // with `Record<string, never>`, so no literal is assignable to it; the
    // fields below are exactly the ViewExecuteRequest half the component reads.
    state: {
      page: { offset: 0, first: 25 },
      search: "",
    } as ViewExecuteResponse["state"],
    capabilities: [
      {
        key: "title",
        label: "Title",
        sortable: true,
        groupable: false,
        filterOps: ["contains"],
      },
      {
        key: "frontmatter.spec-status",
        label: "Status",
        sortable: true,
        groupable: true,
        filterOps: ["eq", "in"],
        values: ["active", "draft"],
        edit: {
          kind: "enum",
          operation: "setField",
          field: "specStatus",
          options: ["active", "draft", "archived"],
        },
      },
      {
        key: "id",
        label: "Id",
        sortable: true,
        groupable: true,
        filterOps: ["eq", "in"],
      },
      {
        key: "frontmatter.id",
        label: "ID",
        sortable: true,
        groupable: true,
        filterOps: ["eq", "in"],
      },
      {
        key: "specStatus",
        label: "SpecStatus",
        sortable: true,
        groupable: true,
        filterOps: ["eq", "in"],
        values: ["active", "draft"],
      },
      {
        key: "updatedAt",
        label: "Updated",
        sortable: true,
        groupable: false,
        filterOps: ["gte", "lte"],
      },
    ],
    columns: [
      { field: "title", label: "Title" },
      { field: "frontmatter.spec-status", label: "Status" },
      { field: "frontmatter.id", label: "ID" },
      { field: "updatedAt", label: "Updated" },
    ],
    rows,
    groups: [
      {
        field: "frontmatter.spec-status",
        key: "frontmatter.spec-status=active",
        label: "Active",
        value: "active",
        count: 1,
        depth: 0,
        rowStart: 0,
        rowEnd: 1,
      },
      {
        field: "frontmatter.spec-status",
        key: "frontmatter.spec-status=draft",
        label: "Draft",
        value: "draft",
        count: 1,
        depth: 0,
        rowStart: 1,
        rowEnd: 2,
      },
    ],
    pageInfo: {
      total: rows.length,
      offset: 0,
      first: 25,
      returned: rows.length,
      hasMore: false,
    },
    definitionFingerprint: "def",
    sourceFingerprint: "source",
    executionFingerprint: "exec",
  };
}

export function executionWithMoreRows() {
  const next = execution();

  return {
    ...next,
    pageInfo: {
      ...next.pageInfo,
      total: 50,
      hasMore: true,
    },
  };
}

/** Option labels of the filter row's "Field" select, narrowed instead of asserted. */
export function filterFieldOptionLabels() {
  const select = screen.getByLabelText("Field");

  if (!(select instanceof HTMLSelectElement)) {
    throw new Error("Field control is not a <select>");
  }

  return Array.from(select.options).map((option) => option.text);
}

export function openEnumEditor(index = 0) {
  fireEvent.click(screen.getAllByRole("button", { name: "Edit Status" })[index]);

  const editor = screen
    .getAllByLabelText("Edit Status")
    .find((element) => element instanceof HTMLSelectElement);

  if (!(editor instanceof HTMLSelectElement)) {
    throw new Error("Status control is not a <select>");
  }

  return editor;
}

export function renderView(
  props: Partial<{
    view: ViewCatalogEntry;
    execution: ViewExecuteResponse | null;
    loading: boolean;
    error: string | ViewErrorInfo | null;
    state: ViewExecuteRequest;
    onStateChange: (state: ViewExecuteRequest) => void;
    onRefresh: () => void;
    onOpenRow: (row: ViewTableRow) => void;
    editSession: OntologyEditSessionResponse | null;
    vaultKey: string | null;
    onStageOps: (ops: OntologyEditOp[]) => Promise<void>;
    issueCounts: ReadonlyMap<string, number>;
    onOpenIssues: (scope: ValidationScope) => void;
    stagedEditsPending: boolean;
    onViewSaved: ComponentProps<typeof ConfiguredView>["onViewSaved"];
    context: ComponentProps<typeof ConfiguredView>["context"];
  }> = {},
) {
  return render(
    <ConfiguredView
      view={props.view ?? baseView}
      execution={props.execution ?? execution()}
      loading={props.loading ?? false}
      error={props.error ?? null}
      state={props.state ?? { page: { offset: 0, first: 25 } }}
      onStateChange={props.onStateChange ?? (() => {})}
      onRefresh={props.onRefresh ?? (() => {})}
      onOpenRow={props.onOpenRow ?? (() => {})}
      editSession={props.editSession}
      vaultKey={props.vaultKey}
      onStageOps={props.onStageOps}
      issueCounts={props.issueCounts}
      onOpenIssues={props.onOpenIssues}
      stagedEditsPending={props.stagedEditsPending}
      onViewSaved={props.onViewSaved}
      context={props.context}
    />,
  );
}

export const actionItemRow: ViewTableRow = {
  ref: {
    notePath: "meetings/planning.md",
    fragment: "^AI-1",
    nodeId: "AI-1",
    structuralFingerprint: "fp-1",
    kind: "EMBEDDED",
  },
  path: "meetings/planning.md",
  title: "Follow up",
  resolvedType: "ActionItem",
  fields: {
    done: false,
    assignedTo: "people/alice.md",
  },
};

export const configuredCardLayout: NonNullable<ViewExecuteResponse["card"]> = {
  eyebrow: { field: "frontmatter.id", label: "ID" },
  title: { field: "title", label: "Title" },
  preview: { field: "path", label: "Path" },
  fields: [{ field: "frontmatter.spec-status", label: "Status" }],
};

export function boardExecution(): ViewExecuteResponse {
  const result = execution();

  return {
    ...result,
    variant: "kanban",
    card: configuredCardLayout,
    board: {
      columnField: "frontmatter.spec-status",
      editable: true,
      columns: [
        {
          key: "planned",
          value: "draft",
          label: "Planned",
          tone: "neutral",
          count: 3,
          rowStart: 0,
          rowEnd: 1,
        },
        {
          key: "empty",
          value: "",
          label: "(empty)",
          tone: "muted",
          count: 0,
          rowStart: 1,
          rowEnd: 1,
        },
        {
          key: "active",
          value: "active",
          label: "Active",
          tone: "progress",
          count: 1,
          rowStart: 1,
          rowEnd: 2,
          collapsedByDefault: true,
        },
      ],
    },
  };
}

export function rendererProps(
  overrides: Partial<ComponentProps<typeof ConfiguredViewTable>> = {},
): ComponentProps<typeof ConfiguredViewTable> {
  const result = overrides.execution ?? boardExecution();

  return {
    execution: result,
    rows: result.rows,
    columns: result.columns,
    capabilities: result.capabilities ?? [],
    state: { page: { offset: 0, first: 25 } },
    viewID: result.view.id,
    loading: false,
    onOpenRow: () => {},
    onSort: () => {},
    onCollapsedGroupCountChange: () => {},
    ...overrides,
  };
}
