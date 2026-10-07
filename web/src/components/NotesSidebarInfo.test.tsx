import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { NodePreview, NodeWorkspace } from "../api/types";
import { withFakeFetch } from "../test/fakeFetch";
import { NotesSidebarInfo } from "./NotesSidebarInfo";

const preview: NodePreview = {
  ref: "docs/playground/pizza-party-2026.md#item-1030",
  path: "docs/playground/pizza-party-2026.md",
  title: "Pizza party",
  typeLabel: "Event",
  format: "markdown",
  fragmentResolved: true,
  fields: [],
  hasIssues: false,
};

function setHover(enabled: boolean) {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: vi.fn().mockReturnValue({ matches: enabled }),
  });
}

function renderSidebar(workspace: NodeWorkspace, onOpen = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  function Wrapper({ children }: PropsWithChildren) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }

  return {
    onOpen,
    ...render(<NotesSidebarInfo workspace={workspace} onOpen={onOpen} />, {
      wrapper: Wrapper,
    }),
  };
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

/** A person note whose only relation is an inbound action item inside another note. */
function workspaceWithEmbeddedRelation(): NodeWorkspace {
  const ref = { notePath: "docs/people/drew.md", kind: "NOTE" };

  return {
    requestedRef: "docs/people/drew.md",
    focusedNodeId: "note:docs/people/drew.md",
    node: {
      ref,
      notePath: "docs/people/drew.md",
      title: "Drew Colthorp",
      locator: "docs/people/drew.md",
      nodeLocator: {
        ref,
        kind: "NOTE",
        sourceLocator: "docs/people/drew.md",
        status: "linkable",
      },
    },
    content: {
      path: "docs/people/drew.md",
      title: "Drew Colthorp",
      markdown: "",
      sourceCapabilities: [],
    },
    relations: [
      {
        key: "structural",
        label: "Structural relations",
        items: [
          {
            path: "docs/playground/pizza-party-2026.md",
            anchor: "item-1030",
            title: "Decide rain-date contingency by end of May",
            kind: "embedded",
            relationName: "assignee",
            provenance: "field",
            structural: true,
          },
        ],
      },
    ],
    nodes: [],
    edges: [],
    loaded: { rendered: true, assessment: true, structure: true, relations: true },
    capabilities: {
      canEdit: false,
      canEditFields: false,
      canEditCollections: false,
      canNavigateChildren: true,
      canSubscribe: true,
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

function workspaceWithNavigation(): NodeWorkspace {
  const workspace = workspaceWithEmbeddedRelation();
  workspace.requestedRef = "docs/efforts/example/plan.html";
  workspace.node.notePath = "docs/efforts/example/plan.html";
  workspace.content.path = "docs/efforts/example/plan.html";
  workspace.relations = [
    {
      key: "workspace:docs/efforts/example/effort.html",
      label: "In this effort",
      ownerTitle: "Schema-driven navigation",
      navigation: true,
      items: [
        {
          path: "docs/efforts/example/effort.html",
          title: "Overview",
          targetTitle: "Schema-driven navigation",
          kind: "note",
        },
        {
          path: "docs/efforts/example/plan.html",
          title: "Implementation plan",
          targetTitle: "Plan details",
          kind: "note",
          current: true,
        },
        {
          path: "docs/efforts/example/work-log.md",
          title: "Work log",
          targetTitle: "Execution history",
          kind: "note",
        },
      ],
    },
    {
      key: "structural",
      label: "Structural relations",
      items: [
        {
          path: "docs/specs/product/example.md",
          title: "Governing product behavior",
          kind: "note",
          relationName: "governingSpecs",
          structural: true,
        },
      ],
    },
  ];

  return workspace;
}

function workspaceWithSectionsAndNearby(): NodeWorkspace {
  const workspace = workspaceWithEmbeddedRelation();

  workspace.focusedNodeId = "focused-section";
  workspace.nodes = [
    {
      id: "parent-section",
      kind: "section",
      ref: {
        notePath: "docs/people/drew.md",
        fragment: "Overview",
        structuralFingerprint: "parent-stable",
        kind: "SECTION",
      },
      notePath: "docs/people/drew.md",
      status: workspace.status,
      capabilities: workspace.capabilities,
      data: { title: "Overview" },
    },
    {
      id: "focused-section",
      kind: "section",
      ref: {
        notePath: "docs/people/drew.md",
        fragment: "Current",
        structuralFingerprint: "current-stable",
        kind: "SECTION",
      },
      notePath: "docs/people/drew.md",
      parentId: "parent-section",
      status: workspace.status,
      capabilities: workspace.capabilities,
      data: { title: "Current" },
    },
    {
      id: "nearby-section",
      kind: "section",
      ref: {
        notePath: "docs/people/drew.md",
        fragment: "Old nearby heading",
        structuralFingerprint: "nearby-stable",
        kind: "SECTION",
      },
      notePath: "docs/people/drew.md",
      parentId: "parent-section",
      status: workspace.status,
      capabilities: workspace.capabilities,
      data: { title: "Nearby section" },
    },
    {
      id: "field-sections",
      kind: "field",
      ref: workspace.node.ref,
      notePath: "docs/people/drew.md",
      parentId: "focused-section",
      status: workspace.status,
      capabilities: workspace.capabilities,
      field: {
        name: "supportingSections",
        valueKind: "section-ref",
        present: true,
        values: [],
        range: { start: 0, end: 0 },
        sectionRefs: [
          {
            notePath: "docs/sections.md",
            fragment: "Old section heading",
            structuralFingerprint: "section-stable",
            kind: "SECTION",
          },
        ],
      },
    },
  ];

  return workspace;
}

function groupSection(title: string): HTMLElement {
  const section = screen
    .getByRole("heading", { name: (name) => name.replace(/\d+$/, "") === title })
    .closest("section");

  if (!section) throw new Error(`no sidebar section rendered for ${title}`);

  return section;
}

describe("NotesSidebarInfo", () => {
  it("keeps embedded relation targets with related notes and opens them at their anchor", () => {
    const onOpen = vi.fn();
    render(<NotesSidebarInfo workspace={workspaceWithEmbeddedRelation()} onOpen={onOpen} />);

    const link = within(groupSection("Relations")).getByRole("button", {
      name: "Decide rain-date contingency by end of May",
    });

    expect(screen.queryByRole("heading", { name: "Code" })).toBeNull();

    fireEvent.click(link);
    expect(onOpen).toHaveBeenCalledWith("docs/playground/pizza-party-2026.md#item-1030", "stack");
  });

  it("groups field relations by direction and merges body links and backlinks per note", () => {
    const workspace = workspaceWithEmbeddedRelation();
    workspace.relations = [
      {
        key: "structural",
        label: "Structural relations",
        items: [
          {
            path: "ideas/a.md",
            title: "Idea A",
            kind: "note",
            resolvedType: "Idea",
            relationName: "opportunities",
            provenance: "field",
            direction: "incoming",
            structural: true,
          },
          {
            path: "impacts/b.md",
            title: "Impact B",
            kind: "note",
            resolvedType: "Impact",
            relationName: "impacts",
            provenance: "field",
            direction: "outgoing",
            structural: true,
          },
        ],
      },
      {
        key: "ambient",
        label: "Ambient relations",
        items: [
          { path: "ideas/a.md", title: "Idea A", kind: "note", relationName: "related" },
          {
            path: "people/c.md",
            title: "Person C",
            kind: "note",
            resolvedType: "Person",
            relationName: "related",
            provenance: "body_link",
            direction: "outgoing",
          },
          {
            path: "people/c.md",
            title: "Person C",
            kind: "note",
            resolvedType: "Person",
            relationName: "related",
            provenance: "backlink",
            direction: "incoming",
          },
        ],
      },
      {
        key: "backlinks",
        label: "Backlinks",
        items: [
          { path: "Log/2026-10-01.md", title: "2026-10-01", kind: "note", direction: "incoming" },
          {
            path: "Log/2026-10-02.md",
            title: "2026-10-02",
            kind: "note",
            provenance: "backlink",
            direction: "outgoing",
          },
        ],
      },
    ];
    render(<NotesSidebarInfo workspace={workspace} onOpen={vi.fn()} />);

    const relations = groupSection("Relations");
    expect(relations).toHaveTextContent(/←\s*Opportunities\s*Links here\s*Idea\s*1\s*Idea A/);
    expect(relations).toHaveTextContent(/Impacts\s*→\s*Links to\s*Impact\s*1\s*Impact B/);

    const linked = groupSection("Linked notes");
    expect(within(linked).queryByRole("button", { name: "Idea A" })).toBeNull();
    // An explicit direction wins over the legacy backlink-provenance fallback.
    expect(within(linked).getByRole("img", { name: "Links to" })).toBeTruthy();
    expect(within(linked).getAllByRole("button", { name: "Person C" })).toHaveLength(1);
    expect(within(linked).getByRole("img", { name: "Linked both ways" })).toBeTruthy();
    expect(linked).toHaveTextContent("Person");
    expect(linked).toHaveTextContent("Log/");
  });

  it("collapses long buckets behind a toggle", () => {
    const workspace = workspaceWithEmbeddedRelation();
    workspace.relations = [
      {
        key: "backlinks",
        label: "Backlinks",
        items: Array.from({ length: 8 }, (_, index) => ({
          path: `Log/day-${index}.md`,
          title: `Day ${index}`,
          kind: "note",
          direction: "incoming" as const,
        })),
      },
    ];
    render(<NotesSidebarInfo workspace={workspace} onOpen={vi.fn()} />);

    const linked = groupSection("Linked notes");
    expect(within(linked).getAllByRole("listitem")).toHaveLength(5);
    fireEvent.click(within(linked).getByRole("button", { name: "3 more" }));
    expect(within(linked).getAllByRole("listitem")).toHaveLength(8);
  });

  it("renders schema-declared workspace navigation separately and marks the current member", () => {
    const onOpen = vi.fn();
    render(<NotesSidebarInfo workspace={workspaceWithNavigation()} onOpen={onOpen} />);

    const navigation = groupSection("In this effort");
    expect(navigation).toHaveTextContent("Schema-driven navigation");
    expect(within(navigation).getByRole("button", { name: "Implementation plan" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(within(navigation).getByRole("button", { name: "Implementation plan" })).toHaveAttribute(
      "title",
      "Plan details",
    );

    const related = groupSection("Relations");
    expect(
      within(related).getByRole("button", { name: "Governing product behavior" }),
    ).toBeTruthy();
    expect(related).toHaveTextContent("Governing specs");
    expect(within(related).queryByRole("button", { name: "Implementation plan" })).toBeNull();

    fireEvent.click(within(navigation).getByRole("button", { name: "Work log" }));
    expect(onOpen).toHaveBeenCalledWith("docs/efforts/example/work-log.md", "stack");
  });
});

describe("NotesSidebarInfo note previews", () => {
  const http = withFakeFetch();

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    setHover(true);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("opens a related-note preview to the left and preserves click modes", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
      function (this: HTMLElement) {
        if (this.classList.contains("note-preview")) return new DOMRect(0, 0, 240, 180);

        if (this.classList.contains("ontology-sidebar-info")) {
          return new DOMRect(880, 0, 320, 700);
        }

        if (this instanceof HTMLButtonElement) return new DOMRect(900, 120, 100, 24);

        return new DOMRect();
      },
    );

    const view = renderSidebar(workspaceWithEmbeddedRelation());

    const button = within(groupSection("Relations")).getByRole("button", {
      name: "Decide rain-date contingency by end of May",
    });

    fireEvent.mouseEnter(button, { clientY: 125 });
    await advance(100);
    await advance(200);

    const card = screen.getByRole("region", { name: "Preview of Pizza party" });
    expect(card).toHaveStyle({ left: "634px", top: "120px" });
    expect(button).toHaveAttribute("aria-describedby", card.id);
    const request = http.requests("GET", "/api/v1/nodes/preview")[0];
    expect(request?.query.get("ref")).toBe("docs/playground/pizza-party-2026.md#item-1030");
    expect(request?.query.get("from")).toBe("docs/people/drew.md");

    fireEvent.keyDown(window, { key: "Escape" });
    expect(
      screen.queryByRole("region", { name: "Preview of Pizza party" }),
    ).not.toBeInTheDocument();

    fireEvent.click(button);
    expect(view.onOpen).toHaveBeenLastCalledWith(
      "docs/playground/pizza-party-2026.md#item-1030",
      "stack",
    );
    fireEvent.click(button, { metaKey: true });
    expect(view.onOpen).toHaveBeenLastCalledWith(
      "docs/playground/pizza-party-2026.md#item-1030",
      "beside",
    );
  });

  it("does not duplicate an anchor already present in a relation path", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    const workspace = workspaceWithEmbeddedRelation();
    workspace.relations![0].items![0].path = "docs/playground/pizza-party-2026.md#item-1030";
    const view = renderSidebar(workspace);

    const button = within(groupSection("Relations")).getByRole("button", {
      name: "Decide rain-date contingency by end of May",
    });

    fireEvent.mouseEnter(button);
    await advance(300);

    expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("ref")).toBe(
      "docs/playground/pizza-party-2026.md#item-1030",
    );
    fireEvent.click(button);
    expect(view.onOpen).toHaveBeenCalledWith(
      "docs/playground/pizza-party-2026.md#item-1030",
      "stack",
    );
  });

  it("previews and opens section and nearby refs by their stable targets", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    const view = renderSidebar(workspaceWithSectionsAndNearby());

    const section = within(groupSection("Relations")).getByRole("button", {
      name: "Old section heading",
    });

    fireEvent.mouseEnter(section);
    await advance(300);

    expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("ref")).toBe(
      "docs/sections.md#struct:section-stable",
    );
    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.click(section, { ctrlKey: true });
    expect(view.onOpen).toHaveBeenLastCalledWith(
      "docs/sections.md#struct:section-stable",
      "beside",
    );

    const nearby = within(groupSection("Nearby nodes")).getByRole("button", {
      name: "Nearby section",
    });

    fireEvent.mouseEnter(nearby);
    await advance(300);

    expect(http.requests("GET", "/api/v1/nodes/preview")[1]?.query.get("ref")).toBe(
      "docs/people/drew.md#struct:nearby-stable",
    );
    fireEvent.click(nearby);
    expect(view.onOpen).toHaveBeenLastCalledWith(
      "docs/people/drew.md#struct:nearby-stable",
      "stack",
    );
  });

  it("shrinks a left preview to remain outside a narrow rail", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
      function (this: HTMLElement) {
        if (this.classList.contains("note-preview")) return new DOMRect(0, 0, 440, 180);

        if (this.classList.contains("ontology-sidebar-info")) {
          return new DOMRect(400, 0, 300, 700);
        }

        if (this instanceof HTMLButtonElement) return new DOMRect(420, 120, 100, 24);

        return new DOMRect();
      },
    );

    renderSidebar(workspaceWithEmbeddedRelation());

    const button = within(groupSection("Relations")).getByRole("button", {
      name: "Decide rain-date contingency by end of May",
    });

    fireEvent.mouseEnter(button);
    await advance(100);
    await advance(200);

    const card = screen.getByRole("region", { name: "Preview of Pizza party" });
    expect(card).toHaveStyle({ left: "8px", maxWidth: "386px" });
    expect(
      Number.parseFloat(card.style.left) + Number.parseFloat(card.style.maxWidth),
    ).toBeLessThan(400);
  });

  it("opens from focus, supports Tab transfer, and closes on blur", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    const view = renderSidebar(workspaceWithEmbeddedRelation());

    const button = within(groupSection("Relations")).getByRole("button", {
      name: "Decide rain-date contingency by end of May",
    });

    act(() => button.focus());
    await advance(100);
    await advance(200);

    const card = screen.getByRole("region", { name: "Preview of Pizza party" });
    fireEvent.keyDown(button, { key: "Tab" });
    expect(card).toHaveFocus();
    fireEvent.keyDown(card, { key: "Tab", shiftKey: true });
    expect(button).toHaveFocus();

    fireEvent.blur(button);
    await advance(150);
    expect(
      screen.queryByRole("region", { name: "Preview of Pizza party" }),
    ).not.toBeInTheDocument();
    expect(view.onOpen).not.toHaveBeenCalled();
  });
});
