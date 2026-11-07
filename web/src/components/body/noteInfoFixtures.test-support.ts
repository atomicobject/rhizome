import type { NodeWorkspace } from "../../api/types";

/** A person note whose only relation is an inbound action item inside another note. */
export function workspaceWithEmbeddedRelation(): NodeWorkspace {
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

export function workspaceWithNavigation(): NodeWorkspace {
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

export function workspaceWithSectionsAndNearby(): NodeWorkspace {
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
