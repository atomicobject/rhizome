import { describe, expect, it } from "vitest";
import type { ViewCatalogEntry, ViewExecuteResponse } from "../api/types";
import { sanitizeNativePreferences } from "./sanitize";

const view: ViewCatalogEntry = {
  id: "tasks",
  name: "Tasks",
  source: { kind: "ontology_type", type: "Task" },
  mount: { kind: "type", type: "Task" },
  defaults: {},
  variants: { table: {} },
  definition: {
    apiVersion: "rhizome.view.v1",
    id: "tasks",
    name: "Tasks",
    source: { kind: "ontology_type", type: "Task" },
    mount: { kind: "type", type: "Task" },
    variants: { table: {} },
    filterPresets: [{ id: "active", filters: [] }],
  },
};

const baseline: ViewExecuteResponse = {
  view,
  variant: "table",
  state: {},
  columns: [],
  rows: [],
  capabilities: [
    {
      key: "status",
      canonicalField: "status",
      sourceKeys: ["status", "frontmatter.status"],
      source: "schema",
      sortable: true,
      groupable: true,
    },
    {
      key: "frontmatter.status",
      canonicalField: "status",
      sourceKeys: ["frontmatter.status", "inline.status"],
      sortable: true,
      groupable: true,
    },
  ],
  pageInfo: { total: 0, offset: 0, first: 25, returned: 0, hasMore: false },
  definitionFingerprint: "definition",
  sourceFingerprint: "source",
  executionFingerprint: "execution",
};

describe("obsolete native preferences", () => {
  it("preserves bare metadata aliases still referenced by shared configuration", () => {
    const authored: ViewCatalogEntry = {
      ...view,
      defaults: { group: { field: "owner" } },
      variants: { table: { columns: [{ field: "owner" }] } },
    };

    const values = {
      "native.group": { field: "owner" },
      "native.columns": ["title", "owner"],
      "native.widths": { owner: 180 },
    };

    expect(sanitizeNativePreferences(values, authored, baseline)).toMatchObject({
      values,
      changed: false,
    });
  });

  it("removes only missing references while preserving aliases, builtins, and special controls", () => {
    const values = {
      "native.sort": [
        { field: "inline.status", direction: "asc" },
        { field: "removed", direction: "desc" },
      ],
      "native.filters": [
        { field: "stale", op: "eq", value: "true" },
        { field: "removed", op: "eq", value: "yes" },
      ],
      "native.group": {
        fields: ["status", "removed"],
        values: [
          { field: "status", value: "active" },
          { field: "removed", value: "yes" },
        ],
      },
      "native.columns": ["title", "validationIssues", "removed"],
      "native.reshown": ["status", "removed"],
      "native.widths": { title: 200, removed: 100 },
      "native.columnField": "removed",
      "native.laneField": "none",
      "native.filterPreset": "active",
      custom: { untouched: true },
    };

    const result = sanitizeNativePreferences(values, view, baseline);
    expect(result.values).toEqual({
      "native.sort": [{ field: "inline.status", direction: "asc" }],
      "native.filters": [{ field: "stale", op: "eq", value: "true" }],
      "native.group": { fields: ["status"], values: [{ field: "status", value: "active" }] },
      "native.columns": ["title", "validationIssues"],
      "native.reshown": ["status"],
      "native.widths": { title: 200 },
      "native.laneField": "none",
      "native.filterPreset": "active",
      custom: { untouched: true },
    });
    expect(result.patch.unset).toEqual(["native.columnField"]);
    expect(values["native.columns"]).toEqual(["title", "validationIssues", "removed"]);
  });

  it("unsets completely obsolete overrides so authored defaults apply", () => {
    const result = sanitizeNativePreferences(
      {
        "native.sort": [{ field: "removed", direction: "asc" }],
        "native.filters": [{ field: "removed", op: "eq", value: "yes" }],
        "native.group": { field: "removed" },
        "native.columns": ["removed"],
        "native.filterPreset": "deleted-preset",
      },
      view,
      baseline,
    );

    expect(result.values).toEqual({});
    expect(result.patch.set).toEqual({});
    expect(result.changed).toBe(true);
  });

  it("keeps unknown row fields when a dynamic source cannot establish their removal", () => {
    const dynamic = { ...view, source: { kind: "query_recipe" } };
    const values = { "native.filters": [{ field: "not-in-this-page", op: "eq", value: "yes" }] };
    expect(sanitizeNativePreferences(values, dynamic, baseline)).toMatchObject({
      values,
      changed: false,
    });
    expect(
      sanitizeNativePreferences(values, view, { ...baseline, capabilities: undefined }),
    ).toMatchObject({ values, changed: false });
    // Without schema capabilities the baseline cannot tell a removed field from an unread one.
    expect(
      sanitizeNativePreferences(values, view, {
        ...baseline,
        capabilities: baseline.capabilities?.filter((capability) => capability.source !== "schema"),
      }),
    ).toMatchObject({ values, changed: false });
  });

  it("keeps frontmatter and inline keys that only unread rows may carry", () => {
    const values = {
      "native.filters": [{ field: "frontmatter.reviewer", op: "eq", value: "drew" }],
      "native.sort": [{ field: "inline.estimate", direction: "asc" }],
      "native.group": { fields: ["frontmatter.team"] },
      "native.columns": ["title", "frontmatter.reviewer"],
      "native.widths": { "inline.estimate": 120 },
      "native.laneField": "frontmatter.team",
    };

    expect(sanitizeNativePreferences(values, view, baseline)).toMatchObject({
      values,
      changed: false,
    });
  });
});
