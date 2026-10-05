import type { ViewExecuteResponse, ViewFieldCapability, ViewTableRow } from "../api/types";
import { baseView, execution } from "./ConfiguredView.testFixtures";

/** A workflow type's profile execution: IP ideas with a lifecycle, priority, and links. */

const DAY = 86_400;

const nowSeconds = Math.floor(Date.now() / 1000);

const statusCapability: ViewFieldCapability = {
  key: "status",
  label: "Status",
  valueKind: "enum",
  sortable: true,
  groupable: true,
  enumValues: [
    { value: "captured", label: "Captured", tone: "neutral", stage: "open" },
    { value: "exploring", label: "Exploring", tone: "progress", stage: "active" },
    { value: "pursuing", label: "Pursuing", tone: "progress", stage: "active" },
    { value: "parked", label: "Parked", tone: "muted", stage: "dropped", collapsedByDefault: true },
  ],
  edit: {
    kind: "enum",
    operation: "setField",
    field: "status",
    options: ["captured", "exploring", "pursuing", "parked"],
  },
};

const priorityCapability: ViewFieldCapability = {
  key: "priority",
  label: "Priority",
  valueKind: "enum",
  sortable: true,
  groupable: true,
  enumValues: [
    { value: "high", label: "High", tone: "warning", stage: "open" },
    { value: "low", label: "Low", tone: "neutral", stage: "open" },
  ],
  edit: { kind: "enum", operation: "setField", field: "priority", options: ["high", "low"] },
};

const capabilities: ViewFieldCapability[] = [
  { key: "title", label: "Title", sortable: true, groupable: false },
  statusCapability,
  priorityCapability,
  { key: "summary", label: "Summary", sortable: false, groupable: false },
  { key: "nextStep", label: "Next step", sortable: false, groupable: false },
  {
    key: "opportunities",
    label: "Opportunities",
    valueKind: "relation",
    sortable: false,
    groupable: true,
  },
  { key: "owner", label: "Owner", valueKind: "relation", sortable: false, groupable: true },
  { key: "ideas", label: "Ideas", valueKind: "number", sortable: true, groupable: false },
];

function opportunity(name: string) {
  return {
    value: `[[opps/${name}.md|${name}]]`,
    ref: { notePath: `opps/${name}.md`, kind: "NOTE" as const },
    title: name,
  };
}

export function idea(
  title: string,
  status: string,
  options: { opportunities?: string[]; priority?: string; daysAgo?: number } = {},
): ViewTableRow {
  return {
    ref: { notePath: `ideas/${title}.md`, kind: "NOTE" },
    path: `ideas/${title}.md`,
    title,
    resolvedType: "IPIdea",
    updatedAt: nowSeconds - (options.daysAgo ?? 3) * DAY,
    fields: {
      status,
      priority: options.priority ?? "low",
      summary: `${title} summary`,
      nextStep: `${title} next step`,
      opportunities: (options.opportunities ?? []).map((name) => opportunity(name).value),
      owner: "[[people/drew.md|Drew Colthorp]]",
      ideas: 3,
    },
    relationValues: {
      opportunities: (options.opportunities ?? []).map(opportunity),
      owner: [
        {
          value: "[[people/drew.md|Drew Colthorp]]",
          ref: { notePath: "people/drew.md", kind: "NOTE" },
          title: "Drew Colthorp",
        },
      ],
    },
  };
}

export const ideaView = { ...baseView, id: "generated.type.IPIdea", generated: true };

/** Alpha is captured; Bravo is pursuing in two opportunities and stale; Charlie has none. */
export function ideaBoard(rows?: ViewTableRow[], { lanes = true } = {}): ViewExecuteResponse {
  const base = execution(
    rows ?? [
      idea("Alpha", "captured", { opportunities: ["Agents"], priority: "high" }),
      idea("Bravo", "pursuing", { opportunities: ["Agents", "Products"], daysAgo: 40 }),
      idea("Charlie", "pursuing"),
    ],
  );

  const captured = base.rows.filter((row) => row.fields?.status === "captured").length;

  return {
    ...base,
    view: ideaView,
    variant: "kanban",
    capabilities,
    groups: [],
    profile: {
      // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- `shape` is the profile's API field.
      shape: "workflow",
      lifecycleField: "status",
      orderedFields: ["priority"],
      summaryField: "summary",
      keyTextFields: ["nextStep"],
      relationFields: ["opportunities"],
      peopleFields: ["owner"],
      reverseFields: ["ideas"],
    },
    board: {
      columnField: "status",
      editable: true,
      laneField: lanes ? "opportunities" : undefined,
      lanes: lanes
        ? [
            {
              key: "opportunities=agents",
              value: "agents",
              label: "Agents",
              tone: "progress",
              count: 2,
              cells: [
                { value: "captured", rows: [0] },
                { value: "pursuing", rows: [1] },
              ],
            },
            {
              key: "opportunities=products",
              value: "products",
              label: "Products",
              count: 1,
              cells: [{ value: "pursuing", rows: [1] }],
            },
            {
              key: "opportunities=",
              value: "",
              label: "No opportunity",
              count: 1,
              cells: [{ value: "pursuing", rows: [2] }],
            },
          ]
        : undefined,
      columns: [
        {
          key: "captured",
          value: "captured",
          label: "Captured",
          tone: "neutral",
          count: captured,
          rowStart: 0,
          rowEnd: captured,
        },
        {
          key: "exploring",
          value: "exploring",
          label: "Exploring",
          tone: "progress",
          count: 0,
          rowStart: captured,
          rowEnd: captured,
        },
        {
          key: "pursuing",
          value: "pursuing",
          label: "Pursuing",
          tone: "progress",
          count: base.rows.length - captured,
          rowStart: captured,
          rowEnd: base.rows.length,
        },
        {
          key: "parked",
          value: "parked",
          label: "Parked",
          tone: "muted",
          count: 0,
          rowStart: base.rows.length,
          rowEnd: base.rows.length,
          collapsedByDefault: true,
        },
      ],
    },
  };
}
