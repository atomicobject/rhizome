import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { NodeWorkspace, NodeWorkspaceGraph, RenderedSection } from "../api/types";
import { stagedQueryKey } from "../staging/stagedQuery";
import { queryKeys } from "../api/queryKeys";
import { jsonReply, withFakeFetch } from "../test/fakeFetch";
import { installFakeWebGL } from "../test/fakeWebGL";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import type { NoteTabContext } from "./noteTabContext";
import { useState } from "react";

import { NotesRightRail, type NotesRightRailProps } from "./NotesRightRail";

const http = withFakeFetch();

const tabKey = "rhizome:notes:right-rail:tab:v1";

function makeWorkspace(): NodeWorkspace {
  const status = {
    dirty: false,
    validation: { issueCount: 0 },
    freshness: {},
    session: {},
    hasWarnings: false,
  };

  const capabilities = {
    canEdit: false,
    canEditFields: false,
    canEditCollections: false,
    canNavigateChildren: true,
    canSubscribe: true,
  };

  return {
    requestedRef: "notes/alpha.md",
    focusedNodeId: "note|notes/alpha.md||",
    node: {
      ref: { notePath: "notes/alpha.md", kind: "NOTE" },
      notePath: "notes/alpha.md",
      title: "Alpha",
      resolvedType: "Spec",
      locator: "FILE",
    },
    content: {
      path: "notes/alpha.md",
      title: "Alpha",
      resolvedType: "Spec",
    },
    nodes: [
      {
        id: "field-summary",
        kind: "field",
        parentId: "note|notes/alpha.md||",
        ref: { notePath: "notes/alpha.md", kind: "NOTE" },
        notePath: "notes/alpha.md",
        status,
        capabilities,
        field: {
          name: "summary",
          valueKind: "scalar",
          present: true,
          values: ["A useful summary"],
          range: { start: 1, end: 2 },
        },
      },
      {
        id: "field-requirements",
        kind: "field",
        parentId: "note|notes/alpha.md||",
        ref: { notePath: "notes/alpha.md", kind: "NOTE" },
        notePath: "notes/alpha.md",
        status,
        capabilities,
        field: {
          name: "requirements",
          valueKind: "section-ref",
          present: true,
          values: ["notes/alpha.md#details"],
          sectionRefs: [{ notePath: "notes/alpha.md", fragment: "details", kind: "SECTION" }],
          range: { start: 3, end: 4 },
        },
      },
    ],
    relations: [
      {
        key: "related",
        label: "Related notes",
        items: [
          {
            path: "notes/beta.md",
            title: "Beta",
            kind: "note",
            relationName: "related",
          },
        ],
      },
    ],
    loaded: {
      rendered: true,
      assessment: true,
      structure: true,
      relations: true,
    },
    capabilities,
    status,
    version: "alpha-v1",
  };
}

function outline(): RenderedSection[] {
  return [
    {
      id: "intro",
      title: "Introduction",
      level: "H1",
      children: [
        {
          id: "details",
          title: "Details",
          level: "H2",
        },
      ],
    },
  ];
}

function context(navigate = vi.fn()): NoteTabContext {
  return {
    workspace: makeWorkspace(),
    outline: { sections: outline(), navigate },
  };
}

function graphFailure() {
  http.onGraphQL("PublicLocalGraph", () =>
    jsonReply({ errors: [{ message: "graph unavailable" }] }),
  );
}

function RailHarness(props: Omit<NotesRightRailProps, "open" | "onClose">) {
  const [open, setOpen] = useState(true);

  return (
    <>
      <button type="button" onClick={() => setOpen((value) => !value)}>
        Toggle pane
      </button>
      <NotesRightRail {...props} open={open} onClose={() => setOpen(false)} />
    </>
  );
}

beforeEach(() => {
  window.sessionStorage.clear();
});

describe("NotesRightRail", () => {
  it("renders nothing while closed and requests no graph", () => {
    render(
      <NotesRightRail
        open={false}
        onClose={vi.fn()}
        context={context()}
        editSession={null}
        onOpen={vi.fn()}
        onOpenNode={vi.fn()}
      />,
    );

    expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
    expect(http.requests()).toHaveLength(0);
  });

  it("shows the selected record's slot under a Record title", () => {
    const onRecordSlot = vi.fn();
    render(
      <RailHarness
        context={null}
        record
        onRecordSlot={onRecordSlot}
        editSession={null}
        onOpen={vi.fn()}
        onOpenNode={vi.fn()}
      />,
    );

    const pane = screen.getByRole("complementary", { name: "Record" });
    expect(within(pane).getByRole("heading", { name: "Record" })).toBeVisible();
    expect(onRecordSlot).toHaveBeenCalledWith(expect.any(HTMLElement));
    fireEvent.click(within(pane).getByRole("button", { name: "Hide right pane" }));
    expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
  });

  it("shows note context, preserves outline hierarchy, and opens relations with tab targets", async () => {
    graphFailure();
    const navigate = vi.fn();
    const onOpen = vi.fn();
    const noteContext = context(navigate);
    render(
      <RailHarness context={noteContext} editSession={null} onOpen={onOpen} onOpenNode={vi.fn()} />,
    );

    const rail = screen.getByRole("complementary", { name: "Note context" });
    const tabs = within(rail).getByRole("tablist", { name: "Note context" });
    expect(within(tabs).getAllByRole("tab")).toHaveLength(3);
    expect(within(tabs).getByRole("tab", { name: "Info" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(await screen.findByRole("button", { name: "Beta" })).toBeVisible();
    expect(within(rail).queryByText("A useful summary")).not.toBeInTheDocument();
    expect(within(rail).queryByText("notes/alpha.md#details")).not.toBeInTheDocument();
    // The note's own sections are on the page and in Outline, not in Info.
    expect(within(rail).queryByRole("button", { name: "details" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Beta" }));
    expect(onOpen).toHaveBeenCalledWith("notes/beta.md", "stack");
    fireEvent.click(screen.getByRole("button", { name: "Beta" }), { ctrlKey: true });
    expect(onOpen).toHaveBeenLastCalledWith("notes/beta.md", "beside");

    fireEvent.click(within(tabs).getByRole("tab", { name: "Outline" }));
    expect(screen.getByRole("button", { name: "Introduction" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Details" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Collapse Introduction" }));
    expect(screen.queryByRole("button", { name: "Details" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Expand Introduction" }));
    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    expect(navigate).toHaveBeenCalledWith(outline()[0].children?.[0]);

    fireEvent.click(within(tabs).getByRole("tab", { name: "Graph" }));
    await waitFor(() => expect(screen.getByText("Unable to load local graph.")).toBeVisible());
    const requestsBeforeRetry = http.count("POST", "/api/v1/graphql");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() =>
      expect(http.count("POST", "/api/v1/graphql")).toBe(requestsBeforeRetry + 1),
    );
  });

  it("keeps the selected tab while closed and for the session", () => {
    graphFailure();

    const first = render(
      <RailHarness context={context()} editSession={null} onOpen={vi.fn()} onOpenNode={vi.fn()} />,
    );

    fireEvent.click(screen.getByRole("tab", { name: "Outline" }));
    fireEvent.click(screen.getByRole("button", { name: "Hide right pane" }));
    expect(screen.queryByRole("tablist", { name: "Note context" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Toggle pane" }));
    expect(screen.getByRole("tab", { name: "Outline" })).toHaveAttribute("aria-selected", "true");
    expect(window.sessionStorage.getItem(tabKey)).toBe("outline");

    first.unmount();
    render(
      <RailHarness context={context()} editSession={null} onOpen={vi.fn()} onOpenNode={vi.fn()} />,
    );
    expect(screen.getByRole("tab", { name: "Outline" })).toHaveAttribute("aria-selected", "true");
  });

  it("uses cached graph data without showing a loading flash", () => {
    graphFailure();
    const noteContext = context();

    const { queryClient } = render(
      <RailHarness
        context={noteContext}
        editSession={null}
        onOpen={vi.fn()}
        onOpenNode={vi.fn()}
      />,
    );

    const emptyGraph: NodeWorkspaceGraph = {
      focusedNodeId: noteContext.workspace.focusedNodeId,
      nodes: [],
      edges: [],
      views: { localGraph: { nodeIds: [], centerNodeIds: [] } },
    };

    queryClient.setQueryData(
      stagedQueryKey(
        queryKeys.graph.local(noteContext.workspace.node.ref, 500, { notesOnly: true }),
        noteContext.workspace.version,
        null,
      ),
      emptyGraph,
    );
    fireEvent.click(screen.getByRole("tab", { name: "Graph" }));
    expect(screen.queryByText("Loading local graph…")).not.toBeInTheDocument();
    expect(screen.getByText("No local graph yet.")).toBeVisible();
  });

  it("loads the local graph renderer when the Graph tab shows a graph", async () => {
    graphFailure();
    const restoreWebGL = installFakeWebGL();
    const noteContext = context();

    const { queryClient, unmount } = render(
      <RailHarness
        context={noteContext}
        editSession={null}
        onOpen={vi.fn()}
        onOpenNode={vi.fn()}
      />,
    );

    const nodeId = noteContext.workspace.focusedNodeId ?? "focused";
    const notePath = noteContext.workspace.node.notePath;

    const graph: NodeWorkspaceGraph = {
      focusedNodeId: nodeId,
      nodes: [
        {
          id: nodeId,
          kind: "note",
          ref: { notePath, kind: "NOTE" },
          notePath,
          status: noteContext.workspace.status,
          capabilities: noteContext.workspace.capabilities,
          data: { title: "Focused", locator: "FILE" },
        },
      ],
      edges: [],
      views: { localGraph: { nodeIds: [nodeId], centerNodeIds: [nodeId] } },
    };

    queryClient.setQueryData(
      stagedQueryKey(
        queryKeys.graph.local(noteContext.workspace.node.ref, 500, { notesOnly: true }),
        noteContext.workspace.version,
        null,
      ),
      graph,
    );

    try {
      fireEvent.click(screen.getByRole("tab", { name: "Graph" }));
      // The renderer arrives in its own chunk, after the tab opens.
      expect(await screen.findByRole("button", { name: "Zoom in" })).toBeVisible();
    } finally {
      unmount();
      restoreWebGL();
    }
  });
});
