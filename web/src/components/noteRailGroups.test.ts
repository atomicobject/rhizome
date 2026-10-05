import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import type { OntologySummaryResponse } from "../api/types";
import { buildRailGroups, type RailNode } from "./noteRailGroups";

type FixtureItem = {
  name: string;
  role?: string;
  label?: string;
  pluralLabel?: string;
  displayGroup?: string;
  displayParent?: string;
};

type Fixture = {
  types: FixtureItem[];
  interfaces: FixtureItem[];
  expected: {
    summaryTypes: string[];
    summaryInterfaces: Record<string, string[]>;
    parents: Record<string, string>;
    groups: { name: string; roots: string[] }[];
  };
};

// Shared with pkg/ontology/viewconfig/display_groups_test.go. The Go side
// proves the summary members; this side proves the rail tree built from them.
// SAFETY: the fixture is checked-in test data with the Fixture shape, and the
// Go test decodes the same file.
const fixture = JSON.parse(
  readFileSync(resolve(__dirname, "../../../testdata/display-groups/rail-groups.json"), "utf8"),
) as Fixture;

function nodeName(node: RailNode): string {
  return node.kind === "type" ? node.type.name : node.interface.name;
}

describe("buildRailGroups", () => {
  it("matches the shared display-group fixture", () => {
    const { expected } = fixture;

    const summary: OntologySummaryResponse = {
      schemaPresent: true,
      totalNotes: 0,
      typedNotes: 0,
      untypedNotes: 0,
      ambiguousNotes: 0,
      issueNotes: 0,
      types: fixture.types
        .filter((type) => expected.summaryTypes.includes(type.name))
        .map((type) => ({
          ...type,
          role: type.role === "EMBEDDED_NODE" ? "embedded" : "note",
          count: 0,
        })),
      interfaces: fixture.interfaces
        .filter((iface) => iface.name in expected.summaryInterfaces)
        .map((iface) => ({
          ...iface,
          count: 0,
          implementors: expected.summaryInterfaces[iface.name],
        })),
    };

    const groups = buildRailGroups(summary);
    const parents: Record<string, string> = {};

    const visit = (node: RailNode) => {
      for (const child of node.children) {
        parents[nodeName(child)] = nodeName(node);
        visit(child);
      }
    };

    for (const group of groups) group.children.forEach(visit);

    expect(parents).toEqual(expected.parents);
    expect(
      groups.map((group) => ({
        name: group.name,
        roots: group.children.map(nodeName).sort(),
      })),
    ).toEqual(expected.groups);
  });
});
