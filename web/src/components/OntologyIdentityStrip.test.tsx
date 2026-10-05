import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { NodeWorkspace, WorkspaceFieldNode } from "../api/types";
import { OntologyIdentityStrip } from "./OntologyIdentityStrip";
import { pickIdentityFields } from "./ontologyFieldPicking";

function makeFieldNode(
  name: string,
  values: string[],
  typeName?: string,
  enumValues?: string[],
): WorkspaceFieldNode {
  return {
    id: `field|focused|${name}`,
    kind: "field",
    ref: {
      notePath: "docs/specs/product/search-refactor.md",
      kind: "NOTE",
    },
    notePath: "docs/specs/product/search-refactor.md",
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    capabilities: {
      canEdit: false,
      canEditFields: false,
      canEditCollections: false,
      canNavigateChildren: false,
      canSubscribe: false,
    },
    field: {
      name,
      valueKind: "scalar",
      typeName,
      enumValues,
      present: true,
      values,
      range: { start: 0, end: 0 },
      capability: {
        ownerRef: {
          notePath: "docs/specs/product/search-refactor.md",
          kind: "NOTE",
          typeName: "ProductSpec",
        },
        ownerType: "ProductSpec",
        typeName: typeName || "String",
        valueKind:
          typeName === "DateTime"
            ? "datetime"
            : typeName === "Date"
              ? "date"
              : enumValues
                ? "enum"
                : "text",
        list: false,
        required: false,
        enumValues: enumValues || [],
        valueOrigin: "authored",
        identifier: name === "id",
        preferredIdentifier: name === "id",
        displayImportance: "NORMAL",
        writeOperation: "setField",
      },
    },
  };
}

function makeWorkspace(fields: WorkspaceFieldNode[]): NodeWorkspace {
  return {
    requestedRef: "docs/specs/product/search-refactor.md",
    node: {
      ref: {
        notePath: "docs/specs/product/search-refactor.md",
        kind: "NOTE",
      },
      resolvedType: "ProductSpec",
      notePath: "docs/specs/product/search-refactor.md",
      title: "Cross-cutting search refactor",
      locator: "FILE",
    },
    content: {
      path: "docs/specs/product/search-refactor.md",
      title: "Cross-cutting search refactor",
      resolvedType: "ProductSpec",
    },
    nodes: fields,
    loaded: {
      rendered: true,
      assessment: true,
      structure: true,
      relations: true,
    },
    capabilities: {
      canEdit: false,
      canEditFields: false,
      canEditCollections: false,
      canNavigateChildren: false,
      canSubscribe: false,
    },
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    version: "v1",
  };
}

describe("OntologyIdentityStrip", () => {
  it("renders the note title and compact identity fields", () => {
    const fields = [
      makeFieldNode("id", ["SPEC-0007"], "String"),
      makeFieldNode("version", ["1.2"], "String"),
      makeFieldNode("specStatus", ["proposed"], "SpecStatus", [
        "proposed",
        "active",
        "superseded",
        "archived",
      ]),
      makeFieldNode("lastUpdated", ["2026-04-12"], "Date"),
    ];

    const workspace = makeWorkspace(fields);
    const identity = pickIdentityFields(fields);

    render(
      <OntologyIdentityStrip
        workspace={workspace}
        identity={identity}
        editing={false}
        changedFieldNames={new Set()}
        issueCount={0}
        dirty={false}
      />,
    );

    expect(screen.getByText("Cross-cutting search refactor")).toBeInTheDocument();
    expect(screen.getByText("SPEC-0007")).toBeInTheDocument();
    expect(screen.getByText("Proposed")).toBeInTheDocument();
    expect(screen.getByText("2026-04-12")).toBeInTheDocument();
    expect(screen.getByText("Updated")).toBeInTheDocument();
  });

  it("keeps the title visible when the note has no identity fields", () => {
    const workspace = makeWorkspace([]);
    workspace.content.resolvedType = "";
    workspace.node.resolvedType = undefined;
    render(
      <OntologyIdentityStrip
        workspace={workspace}
        identity={pickIdentityFields([])}
        editing={false}
        changedFieldNames={new Set()}
        issueCount={0}
        dirty={false}
      />,
    );
    expect(screen.getByRole("heading", { name: "Cross-cutting search refactor" })).toBeVisible();
  });

  it("surfaces issue and dirty chips", () => {
    const workspace = makeWorkspace([]);
    render(
      <OntologyIdentityStrip
        workspace={workspace}
        identity={pickIdentityFields([])}
        editing={false}
        changedFieldNames={new Set()}
        issueCount={2}
        dirty={true}
      />,
    );
    expect(screen.getByText(/2/)).toBeInTheDocument();
    expect(screen.getByText("Staged")).toBeInTheDocument();
  });

  it("stages a setField op when the status pill is changed in edit mode", () => {
    const fields = [
      makeFieldNode("id", ["SPEC-0007"], "String"),
      makeFieldNode("specStatus", ["proposed"], "SpecStatus", [
        "proposed",
        "active",
        "superseded",
        "archived",
      ]),
    ];

    const workspace = makeWorkspace(fields);
    const identity = pickIdentityFields(fields);
    const onStageOps = vi.fn();
    render(
      <OntologyIdentityStrip
        workspace={workspace}
        identity={identity}
        editing={true}
        changedFieldNames={new Set()}
        issueCount={0}
        dirty={false}
        onStageOps={onStageOps}
      />,
    );
    fireEvent.click(screen.getByText("Active"));
    expect(onStageOps).toHaveBeenCalledTimes(1);
    const ops = onStageOps.mock.calls[0][0];
    expect(ops).toEqual([
      expect.objectContaining({
        id: "field:docs%2Fspecs%2Fproduct%2Fsearch-refactor.md:note:specStatus",
        kind: "setField",
        path: "docs/specs/product/search-refactor.md",
        nodeId: undefined,
        field: "specStatus",
        value: "active",
        fieldValue: { kind: "scalar", scalar: "active" },
        expected: { field: { kind: "scalar", scalar: "proposed" } },
      }),
    ]);
  });

  it("stages DateTime identity edits as RFC3339", () => {
    const fields = [makeFieldNode("lastUpdated", ["2026-04-12T14:30:00Z"], "DateTime")];
    const workspace = makeWorkspace(fields);
    const identity = pickIdentityFields(fields);
    const onStageOps = vi.fn();
    render(
      <OntologyIdentityStrip
        workspace={workspace}
        identity={identity}
        editing={true}
        changedFieldNames={new Set()}
        issueCount={0}
        dirty={false}
        onStageOps={onStageOps}
      />,
    );

    const input = screen.getByLabelText("lastUpdated");
    expect(input).toHaveAttribute("type", "datetime-local");
    fireEvent.change(input, { target: { value: "2026-04-13T08:05" } });
    fireEvent.blur(input);

    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        kind: "setField",
        path: "docs/specs/product/search-refactor.md",
        nodeId: undefined,
        field: "lastUpdated",
        value: "2026-04-13T08:05:00Z",
        fieldValue: { kind: "scalar", scalar: "2026-04-13T08:05:00Z" },
      }),
    ]);
  });
});
