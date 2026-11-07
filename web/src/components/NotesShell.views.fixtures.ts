import type {
  OntologyEditSessionResponse,
  OntologyNoteListItem,
  OntologySummaryResponse,
  OntologyTypeResponse,
  ViewCatalogEntry,
  ViewExecuteResponse,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";

export const emptyGraph = { nodes: [], edges: [], truncated: false };

export const productType = {
  name: "ProductSpec",
  label: "ProductSpec",
  count: 1,
  issueCount: 0,
};

export const interfaceSummary = {
  name: "SpecLike",
  label: "SpecLike",
  count: 1,
  issueCount: 0,
  implementors: ["ProductSpec"],
};

export function summary(overrides: Partial<OntologySummaryResponse> = {}): OntologySummaryResponse {
  return {
    schemaPresent: true,
    totalNotes: 1,
    typedNotes: 1,
    untypedNotes: 0,
    ambiguousNotes: 0,
    issueNotes: 0,
    types: [],
    interfaces: [],
    ...overrides,
  };
}

export function typeResponse(name = "ProductSpec"): OntologyTypeResponse {
  const row = noteRow(name === "ProductSpec" ? "specs/alpha.md" : "notes/all.md");

  return {
    count: 1,
    issueCount: 0,
    type: { name, label: name, fields: [] },
    notes: [row],
  };
}

export function noteRow(path: string, title = "Alpha Spec"): ViewTableRow & OntologyNoteListItem {
  return {
    ref: { notePath: path, kind: "NOTE" },
    path,
    title,
    resolvedType: "ProductSpec",
    updatedAt: 1_713_000_000,
    hasIssues: false,
    fields: {},
  };
}

export function viewEntry(
  id: string,
  name: string,
  mount: ViewCatalogEntry["mount"],
  field = "title",
): ViewCatalogEntry {
  const source: ViewCatalogEntry["source"] = {
    kind: mount.kind === "standalone" ? "query_recipe" : "ontology_type",
  };

  if (mount.type) source.type = mount.type;

  if (mount.interface) source.interface = mount.interface;

  const variants = {
    table: { columns: [{ field, label: field === "status" ? "Status" : "Title" }] },
  };

  return {
    id,
    name,
    source,
    mount,
    defaults: { variant: "table" },
    variants,
    definition: {
      apiVersion: "rhizome.view.v1",
      id,
      name,
      source,
      mount,
      variants,
    },
  };
}

export function capability(field: string, edit?: ViewFieldCapability["edit"]): ViewFieldCapability {
  const result: ViewFieldCapability = {
    key: field,
    label: field === "status" ? "Status" : "Title",
    valueKind: field === "status" ? "enum" : "string",
    sortable: true,
    groupable: false,
    filterOps: ["eq", "contains"],
  };

  if (edit) result.edit = edit;

  return result;
}

export function execution(
  view: ViewCatalogEntry,
  rows: ViewTableRow[],
  capabilities: ViewFieldCapability[] = [],
): ViewExecuteResponse {
  return {
    view,
    variant: "table",
    state: {},
    capabilities,
    columns: view.variants.table?.columns ?? [],
    rows,
    pageInfo: {
      total: rows.length,
      offset: 0,
      first: 25,
      returned: rows.length,
      hasMore: false,
    },
    definitionFingerprint: "definition",
    sourceFingerprint: "source",
    executionFingerprint: `execution-${rows.map((row) => row.title).join(",")}`,
  };
}

export function editSession(
  status: "dirty" | "clean",
  hasUncommittedChanges: boolean,
): OntologyEditSessionResponse {
  return {
    sessionId: "edit-1",
    status,
    revision: 1,
    ops: [],
    hasUncommittedChanges,
    touchedPaths: hasUncommittedChanges ? ["specs/alpha.md"] : [],
    createdAt: "2026-09-06T00:00:00Z",
    updatedAt: "2026-09-06T00:00:01Z",
  };
}
