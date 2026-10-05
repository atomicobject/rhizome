import { readFileSync } from "node:fs";
import path from "node:path";

import { buildSchema, Kind, parse, validate, type FieldNode, type SelectionSetNode } from "graphql";
import { describe, expect, it } from "vitest";

import { GRAPHQL_EXPLORER_QUERY } from "../../components/GraphQLExplorer";
import { PUBLIC_LOCAL_GRAPH_QUERY, PUBLIC_NODE_DETAIL_QUERY } from "./operations";

const publicSchema = buildSchema(
  readFileSync(
    path.resolve(
      process.cwd(),
      "../tests/integration/python/testdata/ontology_query_schema.golden.graphql",
    ),
    "utf8",
  ),
);

describe("public GraphQL operations", () => {
  it.each([
    ["node detail", PUBLIC_NODE_DETAIL_QUERY],
    ["local graph", PUBLIC_LOCAL_GRAPH_QUERY],
    ["explorer default", GRAPHQL_EXPLORER_QUERY],
  ])("keeps the %s operation valid against the public schema", (_name, query) => {
    const errors = validate(publicSchema, parse(query));

    expect(errors.map((error) => error.message)).toEqual([]);
  });

  it.each([
    [
      PUBLIC_NODE_DETAIL_QUERY,
      [
        "node.workspace.bodies.binding.identifierField",
        "node.workspace.parentTitle",
        "node.workspace.fields.capability.displayImportance",
        "node.workspace.bodies.fields.capability.displayImportance",
      ],
    ],
    [
      PUBLIC_LOCAL_GRAPH_QUERY,
      ["node.ref", "node.nodeKind", "node.path", "node.resolvedType", "node.locator"],
    ],
  ])("selects fields used by public workspace adapters", (query, requiredPaths) => {
    const selectedPaths = fieldPaths(parse(query));

    for (const requiredPath of requiredPaths) expect(selectedPaths).toContain(requiredPath);
  });

  it("keeps the explorer default on bounded validation and notes roots", () => {
    const document = parse(GRAPHQL_EXPLORER_QUERY);
    const selectedPaths = fieldPaths(document);

    const operation = document.definitions.find(
      (definition) => definition.kind === Kind.OPERATION_DEFINITION,
    );

    const roots = operation?.selectionSet.selections.filter(
      (selection): selection is FieldNode => selection.kind === Kind.FIELD,
    );

    expect(selectedPaths).toContain("validation.checks.issues.code");
    expect(selectedPaths).toContain("notes.nodes.path");
    expect(roots?.find((field) => field.name.value === "validation")?.arguments).toContainEqual(
      expect.objectContaining({
        name: expect.objectContaining({ value: "firstIssues" }),
        value: expect.objectContaining({ value: "5" }),
      }),
    );
    expect(roots?.find((field) => field.name.value === "notes")?.arguments).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          name: expect.objectContaining({ value: "find" }),
          value: expect.objectContaining({ value: "docs" }),
        }),
        expect.objectContaining({
          name: expect.objectContaining({ value: "first" }),
          value: expect.objectContaining({ value: "10" }),
        }),
      ]),
    );
  });
});

function fieldPaths(document: ReturnType<typeof parse>): string[] {
  const paths: string[] = [];

  const walk = (set: SelectionSetNode, parent = "") => {
    for (const selection of set.selections) {
      if (selection.kind !== Kind.FIELD) continue;

      const path = parent ? `${parent}.${selection.name.value}` : selection.name.value;

      paths.push(path);

      if (selection.selectionSet) walk(selection.selectionSet, path);
    }
  };

  for (const definition of document.definitions) {
    if (definition.kind === Kind.OPERATION_DEFINITION) walk(definition.selectionSet);
  }

  return paths;
}
