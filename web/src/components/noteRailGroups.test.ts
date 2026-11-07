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

  it("leaves out standalone embedded types unless the caller includes them", () => {
    const summary: OntologySummaryResponse = {
      schemaPresent: true,
      totalNotes: 1,
      typedNotes: 1,
      untypedNotes: 0,
      ambiguousNotes: 0,
      issueNotes: 0,
      types: [
        { name: "Spec", label: "Spec", count: 1, role: "note" },
        { name: "Step", label: "Step", count: 0, role: "embedded" },
      ],
      interfaces: [],
    };

    const roots = (groups: ReturnType<typeof buildRailGroups>) =>
      groups.flatMap((group) => group.children.map(nodeName));

    expect(roots(buildRailGroups(summary))).toEqual(["Spec"]);
    expect(roots(buildRailGroups(summary, { includeEmbedded: true }))).toEqual(["Spec", "Step"]);
  });

  it("leaves out empty embedded types under an interface unless the caller includes them", () => {
    // A product model declares every kind; Notes lists only kinds that hold records.
    const summary: OntologySummaryResponse = {
      schemaPresent: true,
      totalNotes: 1,
      typedNotes: 1,
      untypedNotes: 0,
      ambiguousNotes: 0,
      issueNotes: 0,
      types: [
        { name: "Feature", label: "Feature", count: 2, role: "embedded" },
        { name: "Bet", label: "Bet", count: 0, role: "embedded" },
        { name: "Plan", label: "Plan", count: 0, role: "note" },
      ],
      interfaces: [
        { name: "Record", label: "Model records", count: 2, implementors: ["Feature", "Bet"] },
        { name: "Doc", label: "Docs", count: 0, implementors: ["Plan"] },
      ],
    };

    const children = (groups: ReturnType<typeof buildRailGroups>, name: string) =>
      groups
        .flatMap((group) => group.children)
        .find((node) => nodeName(node) === name)
        ?.children.map(nodeName);

    expect(children(buildRailGroups(summary), "Record")).toEqual(["Feature"]);
    expect(children(buildRailGroups(summary), "Doc")).toEqual(["Plan"]);
    expect(children(buildRailGroups(summary, { includeEmbedded: true }), "Record")).toEqual([
      "Bet",
      "Feature",
    ]);
  });

  it("leaves out a group whose interface holds only empty embedded types", () => {
    // A knowledge-only AO KB declares every model kind but holds no model records.
    const summary: OntologySummaryResponse = {
      schemaPresent: true,
      totalNotes: 1,
      typedNotes: 1,
      untypedNotes: 0,
      ambiguousNotes: 0,
      issueNotes: 0,
      types: [
        { name: "Feature", label: "Feature", count: 0, role: "embedded" },
        { name: "Bet", label: "Bet", count: 0, role: "embedded" },
        {
          name: "Assertion",
          label: "Assertion",
          count: 1,
          role: "note",
          displayGroup: "Knowledge",
        },
      ],
      interfaces: [
        {
          name: "Record",
          label: "Model records",
          count: 0,
          implementors: ["Feature", "Bet"],
          displayGroup: "Product model",
        },
      ],
    };

    expect(buildRailGroups(summary).map((group) => group.name)).toEqual(["Knowledge"]);
    expect(buildRailGroups(summary, { includeEmbedded: true }).map((group) => group.name)).toEqual([
      "Knowledge",
      "Product model",
    ]);
  });
});
