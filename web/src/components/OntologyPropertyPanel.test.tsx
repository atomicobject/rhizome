import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { NodeFieldLink, NodePreview, NodeWorkspace, WorkspaceFieldNode } from "../api/types";
import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { OntologyPropertyPanel } from "./OntologyPropertyPanel";

function makeField(
  name: string,
  values: string[],
  options?: {
    typeName?: string;
    enumValues?: string[];
    valueKind?: "scalar" | "list" | "section-ref";
    present?: boolean;
    required?: boolean;
    importance?: "KEY" | "NORMAL" | "DETAIL";
    issueCount?: number;
    links?: NodeFieldLink[];
  },
): WorkspaceFieldNode {
  return {
    id: `field|focused|${name}`,
    kind: "field",
    ref: { notePath: "docs/example.md", kind: "NOTE" },
    notePath: "docs/example.md",
    status: {
      dirty: false,
      validation: { issueCount: options?.issueCount ?? 0 },
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
      valueKind: options?.links ? "relation" : (options?.valueKind ?? "scalar"),
      typeName: options?.typeName,
      enumValues: options?.enumValues,
      present: options?.present ?? true,
      values,
      links: options?.links,
      range: { start: 0, end: 0 },
      capability: {
        ownerRef: { notePath: "docs/example.md", kind: "NOTE", typeName: "Example" },
        ownerType: "Example",
        typeName: options?.typeName || "String",
        valueKind: options?.links
          ? "relation"
          : options?.typeName === "DateTime"
            ? "datetime"
            : options?.typeName === "Date"
              ? "date"
              : options?.enumValues
                ? "enum"
                : "text",
        list: options?.valueKind === "list",
        required: options?.required ?? false,
        enumValues: options?.enumValues || [],
        valueOrigin: "authored",
        identifier: false,
        preferredIdentifier: false,
        displayImportance: options?.importance ?? "NORMAL",
        writeOperation: "setField",
      },
    },
  };
}

type WorkspaceOverrides = Partial<Omit<NodeWorkspace, "node">> & {
  node?: Partial<NodeWorkspace["node"]>;
};

function makeWorkspace(overrides: WorkspaceOverrides = {}): NodeWorkspace {
  const { node: nodeOverrides, ...rest } = overrides;

  const base: NodeWorkspace = {
    requestedRef: "docs/example.md",
    node: {
      ref: { notePath: "docs/example.md", kind: "NOTE" },
      resolvedType: "Example",
      notePath: "docs/example.md",
      title: "Example",
      locator: "FILE",
    },
    content: {
      path: "docs/example.md",
      title: "Example",
      resolvedType: "Example",
    },
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

  return {
    ...base,
    ...rest,
    node: {
      ...base.node,
      ...nodeOverrides,
    },
  };
}

describe("OntologyPropertyPanel", () => {
  beforeEach(() => window.sessionStorage.clear());

  it("renders nothing when there are no fields", () => {
    const { container } = render(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={[]}
        editing={false}
        changedFieldNames={new Set()}
      />,
    );

    expect(container).toBeEmptyDOMElement();
  });

  it("renders a scalar text field as an inline text widget", () => {
    render(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={[makeField("summary", ["A short note"], { typeName: "String" })]}
        editing={false}
        changedFieldNames={new Set()}
      />,
    );
    expect(screen.getByText("Summary")).toBeInTheDocument();
    expect(screen.getByText("A short note")).toBeInTheDocument();
  });

  it("opens wikilinks in read-only field values", () => {
    const onOpen = vi.fn();
    renderWithQueryClient(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={[makeField("sourceRef", ["From [[Sync Note|the sync]] today"])]}
        editing={false}
        changedFieldNames={new Set()}
        onOpen={onOpen}
        rendered={{
          path: "docs/example.md",
          title: "Example",
          links: [{ target: "Log/Sync Note.md", text: "Sync Note", kind: "wikilink" }],
        }}
      />,
    );

    expect(screen.getByText("Source ref").nextElementSibling).toHaveTextContent(
      "From the sync today",
    );
    fireEvent.click(screen.getByRole("link", { name: "the sync" }));
    expect(onOpen).toHaveBeenCalledWith("Log/Sync Note.md", "stack");
  });

  it("keeps staged wikilinks clickable in browse mode", () => {
    const onOpen = vi.fn();
    renderWithQueryClient(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={[makeField("sourceRef", ["[[Sync Note|the sync]]"])]}
        editing={false}
        changedFieldNames={new Set(["sourceRef"])}
        onOpen={onOpen}
        rendered={{
          path: "docs/example.md",
          title: "Example",
          links: [{ target: "Log/Sync Note.md", text: "Sync Note", kind: "wikilink" }],
        }}
      />,
    );

    const link = screen.getByRole("link", { name: "the sync" });
    expect(link.closest(".widget-text")).toHaveClass("is-dirty");
    fireEvent.click(link);
    expect(onOpen).toHaveBeenCalledWith("Log/Sync Note.md", "stack");
  });

  it("shows relation values as their target titles and opens the target", () => {
    const onOpen = vi.fn();
    const value = "[[IP opportunity - AI in the products we build]]";

    const relation = makeField("opportunity", [value], {
      links: [
        {
          value,
          title: "AI in the products we build",
          ref: {
            notePath: "opportunities/IP opportunity - AI in the products we build.md",
            kind: "NOTE",
          },
        },
      ],
    });

    renderWithQueryClient(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={[relation]}
        editing={false}
        changedFieldNames={new Set()}
        onOpen={onOpen}
      />,
    );

    expect(screen.getByText("Opportunity").nextElementSibling).toHaveTextContent(
      /^AI in the products we build$/,
    );
    fireEvent.click(screen.getByRole("link", { name: "AI in the products we build" }));
    expect(onOpen).toHaveBeenCalledWith(
      "opportunities/IP opportunity - AI in the products we build.md",
      "stack",
    );
  });

  it("renders an enum field as a single badge in browse mode", () => {
    render(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={[
          makeField("estimate", ["M"], {
            typeName: "UserStoryEstimate",
            enumValues: ["XS", "S", "M", "L", "XL"],
          }),
        ]}
        editing={false}
        changedFieldNames={new Set()}
      />,
    );
    expect(screen.getByText("M")).toBeInTheDocument();
    expect(screen.queryByRole("radio")).not.toBeInTheDocument();
  });

  it("renders a date field with ISO display", () => {
    render(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={[makeField("start", ["2026-03-15"], { typeName: "Date" })]}
        editing={false}
        changedFieldNames={new Set()}
      />,
    );
    expect(screen.getByText("2026-03-15")).toBeInTheDocument();
  });

  it("orders key fields before normal fields and discloses detail fields", () => {
    const fields = [
      makeField("normalB", ["Normal B"]),
      makeField("keyB", ["Key B"], { importance: "KEY" }),
      makeField("detailA", ["Detail A"], { importance: "DETAIL" }),
      makeField("keyA", ["Key A"], { importance: "KEY" }),
      makeField("normalA", ["Normal A"]),
    ];

    const { unmount } = render(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={fields}
        editing={false}
        changedFieldNames={new Set()}
      />,
    );

    expect(screen.getAllByRole("term").map((term) => term.textContent)).toEqual([
      "Key b",
      "Key a",
      "Normal b",
      "Normal a",
    ]);
    expect(screen.queryByText("Detail A")).not.toBeInTheDocument();
    const disclosure = screen.getByRole("button", { name: "1 more" });
    expect(disclosure).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(disclosure);
    expect(screen.getByText("Detail A")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Hide details" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );

    unmount();
    render(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={fields}
        editing={false}
        changedFieldNames={new Set()}
      />,
    );
    expect(screen.getByText("Detail A")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Hide details" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
  });

  it("shows every detail field when one has a validation issue", () => {
    render(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={[
          makeField("diagnosed", ["Needs work"], { importance: "DETAIL", issueCount: 1 }),
          makeField("context", ["Extra context"], { importance: "DETAIL" }),
        ]}
        editing={false}
        changedFieldNames={new Set()}
      />,
    );

    expect(screen.getByText("Needs work")).toBeInTheDocument();
    expect(screen.getByText("Extra context")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /more|Hide details/ })).not.toBeInTheDocument();
  });

  it("shows detail fields when a required field is missing", () => {
    render(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={[
          makeField("requiredDetail", [], {
            importance: "DETAIL",
            present: false,
            required: true,
          }),
        ]}
        editing={false}
        changedFieldNames={new Set()}
      />,
    );

    expect(screen.getByText("Required detail")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /more|Hide details/ })).not.toBeInTheDocument();
  });

  it("shows detail fields without a disclosure in edit mode", () => {
    render(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={[makeField("detail", ["Visible while editing"], { importance: "DETAIL" })]}
        editing
        changedFieldNames={new Set()}
      />,
    );

    expect(screen.getByDisplayValue("Visible while editing")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /more|Hide details/ })).not.toBeInTheDocument();
  });

  it("stages DateTime edits as RFC3339 instead of dropping time", () => {
    const onStageOps = vi.fn();
    render(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={[
          makeField("updatedAt", ["2026-03-15T14:30:00Z"], {
            typeName: "DateTime",
          }),
        ]}
        editing={true}
        changedFieldNames={new Set()}
        onStageOps={onStageOps}
      />,
    );

    const input = screen.getByLabelText("updatedAt");
    expect(input).toHaveAttribute("type", "datetime-local");
    fireEvent.change(input, { target: { value: "2026-03-16T09:45" } });
    fireEvent.blur(input);

    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        kind: "setField",
        path: "docs/example.md",
        nodeId: undefined,
        field: "updatedAt",
        value: "2026-03-16T09:45:00Z",
        fieldValue: { kind: "scalar", scalar: "2026-03-16T09:45:00Z" },
      }),
    ]);
  });

  it("stages a setField op when an enum field is changed in edit mode", () => {
    const onStageOps = vi.fn();
    render(
      <OntologyPropertyPanel
        workspace={makeWorkspace()}
        fields={[
          makeField("estimate", ["M"], {
            typeName: "UserStoryEstimate",
            enumValues: ["XS", "S", "M", "L", "XL"],
          }),
        ]}
        editing={true}
        changedFieldNames={new Set()}
        onStageOps={onStageOps}
      />,
    );
    fireEvent.click(screen.getByText("L"));
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        kind: "setField",
        path: "docs/example.md",
        nodeId: undefined,
        field: "estimate",
        value: "L",
        fieldValue: { kind: "scalar", scalar: "L" },
      }),
    ]);
  });

  it("renders locator with a caret and stages block-id edits without the caret", () => {
    const onStageOps = vi.fn();
    render(
      <OntologyPropertyPanel
        workspace={makeWorkspace({
          node: {
            ref: {
              notePath: "docs/example.md",
              fragment: "^story-a",
              nodeId: "docs/example.md#^story-a",
              kind: "EMBEDDED",
            },
            notePath: "docs/example.md",
            title: "Story A",
            locator: "EMBEDDED",
          },
        })}
        fields={[makeField("locator", ["story-a"], { typeName: "String" })]}
        editing={true}
        changedFieldNames={new Set()}
        onStageOps={onStageOps}
      />,
    );

    expect(screen.getByText("^")).toBeInTheDocument();
    const input = screen.getByLabelText("locator");
    expect(input).toHaveValue("story-a");
    fireEvent.change(input, { target: { value: "^story-b" } });
    fireEvent.blur(input);

    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        kind: "setBlockID",
        path: "docs/example.md#^story-a",
        nodeId: "docs/example.md#^story-a",
        blockId: "story-b",
      }),
    ]);
  });

  it("stages embedded scalar fields with the embedded locator path", () => {
    const onStageOps = vi.fn();
    render(
      <OntologyPropertyPanel
        workspace={makeWorkspace({
          node: {
            ref: {
              notePath: "docs/example.md",
              fragment: "^story-a",
              nodeId: "docs/example.md#^story-a",
              kind: "EMBEDDED",
            },
            notePath: "docs/example.md",
            title: "Story A",
            locator: "EMBEDDED",
          },
        })}
        fields={[
          makeField("status", ["ready"], {
            typeName: "Status",
            enumValues: ["ready", "done"],
          }),
        ]}
        editing={true}
        changedFieldNames={new Set()}
        onStageOps={onStageOps}
      />,
    );

    fireEvent.click(screen.getByText("Done"));

    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        kind: "setField",
        path: "docs/example.md#^story-a",
        nodeId: "docs/example.md#^story-a",
        field: "status",
        value: "done",
        fieldValue: { kind: "scalar", scalar: "done" },
      }),
    ]);
  });

  it("does not stage invalid locator identifiers", () => {
    const onStageOps = vi.fn();
    render(
      <OntologyPropertyPanel
        workspace={makeWorkspace({
          node: {
            ref: {
              notePath: "docs/example.md",
              fragment: "^story-a",
              nodeId: "docs/example.md#^story-a",
              kind: "EMBEDDED",
            },
            notePath: "docs/example.md",
            title: "Story A",
            locator: "EMBEDDED",
          },
        })}
        fields={[makeField("locator", ["story-a"], { typeName: "String" })]}
        editing={true}
        changedFieldNames={new Set()}
        onStageOps={onStageOps}
      />,
    );

    const input = screen.getByLabelText("locator");
    fireEvent.change(input, { target: { value: "bad id" } });
    fireEvent.blur(input);

    expect(onStageOps).not.toHaveBeenCalled();
  });
});

describe("OntologyPropertyPanel parent preview", () => {
  const http = withFakeFetch();

  const preview: NodePreview = {
    ref: "docs/example.md#struct:parent-stable",
    path: "docs/example.md",
    title: "Parent section",
    typeLabel: "Section",
    format: "markdown",
    fragmentResolved: true,
    fields: [],
    hasIssues: false,
  };

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      writable: true,
      value: vi.fn().mockReturnValue({ matches: true }),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("labels the parent link with the workspace's parent title without fetching a preview", () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    const onOpen = vi.fn();
    renderWithQueryClient(
      <OntologyPropertyPanel
        workspace={makeWorkspace({
          node: {
            ref: {
              notePath: "docs/example.md",
              fragment: "^story-a",
              nodeId: "docs/example.md#^story-a",
              kind: "EMBEDDED",
            },
            parentRef: {
              notePath: "docs/example.md",
              kind: "NOTE",
            },
            parentTitle: "Example",
            notePath: "docs/example.md",
            title: "Story A",
            locator: "EMBEDDED",
          },
        })}
        fields={[makeField("locator", ["story-a"], { typeName: "String" })]}
        editing={false}
        changedFieldNames={new Set()}
        onOpen={onOpen}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Example" }));
    expect(onOpen).toHaveBeenCalledWith("docs/example.md", "stack");
    expect(http.requests("GET", "/api/v1/nodes/preview")).toHaveLength(0);
  });

  it("renders embedded parent and locator as the first two properties", () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    renderWithQueryClient(
      <OntologyPropertyPanel
        workspace={makeWorkspace({
          node: {
            ref: {
              notePath: "docs/example.md",
              fragment: "^story-a",
              nodeId: "docs/example.md#^story-a",
              kind: "EMBEDDED",
            },
            parentRef: {
              notePath: "docs/example.md",
              kind: "NOTE",
            },
            notePath: "docs/example.md",
            title: "Story A",
            locator: "EMBEDDED",
          },
        })}
        fields={[
          makeField("status", ["ready"], {
            typeName: "Status",
            enumValues: ["ready", "done"],
          }),
          makeField("locator", ["story-a"], { typeName: "String" }),
          makeField("summary", ["A short note"], { typeName: "String" }),
        ]}
        editing={false}
        changedFieldNames={new Set()}
      />,
    );

    const rows = screen.getAllByRole("term").map((row) => row.textContent);
    expect(rows).toEqual(["Parent", "Locator", "Status", "Summary"]);
  });
  it("previews and opens the parent's canonical stable ref", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    const onOpen = vi.fn();
    renderWithQueryClient(
      <OntologyPropertyPanel
        workspace={makeWorkspace({
          node: {
            ref: {
              notePath: "docs/example.md",
              fragment: "^story-a",
              nodeId: "docs/example.md#^story-a",
              kind: "EMBEDDED",
            },
            parentRef: {
              notePath: "docs/example.md",
              fragment: "Old parent heading",
              structuralFingerprint: "parent-stable",
              kind: "SECTION",
            },
            notePath: "docs/example.md",
            title: "Story A",
            locator: "EMBEDDED",
          },
        })}
        fields={[makeField("locator", ["story-a"], { typeName: "String" })]}
        editing={false}
        changedFieldNames={new Set()}
        onOpen={onOpen}
      />,
    );

    const button = screen.getByRole("button", {
      name: "docs/example.md#Old parent heading",
    });

    fireEvent.mouseEnter(button);
    await act(async () => vi.advanceTimersByTimeAsync(100));
    await act(async () => vi.advanceTimersByTimeAsync(200));

    expect(screen.getByRole("region", { name: "Preview of Parent section" })).toBeInTheDocument();
    expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("ref")).toBe(
      "docs/example.md#struct:parent-stable",
    );
    fireEvent.click(button, { metaKey: true });
    expect(onOpen).toHaveBeenCalledWith("docs/example.md#struct:parent-stable", "beside");
  });
});
