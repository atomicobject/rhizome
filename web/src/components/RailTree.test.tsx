import { describe, expect, it } from "vitest";
import { type RailGroup, type RailNode } from "./noteRailGroups";
import { withoutEmptyViewDuplicates } from "./RailTree";

const type = (name: string, count: number, children: RailNode[] = []): RailNode => ({
  kind: "type",
  type: { name, pluralLabel: name, count, role: "note" },
  children,
});

describe("empty type duplicates", () => {
  it("hides only zero-record types with a same-group saved-view display name, retaining descendants", () => {
    const groups: RailGroup[] = [
      {
        name: "Knowledge",
        children: [
          type("Agent findings", 0),
          type("Context shards", 0, [type("Child", 2)]),
          type("Reference docs", 4),
          type("Unmatched", 0),
          {
            kind: "interface",
            interface: { name: "Interface", count: 0, implementors: [] },
            children: [],
          },
        ],
      },
      { name: "Other", children: [type("Agent findings", 0)] },
    ];

    const result = withoutEmptyViewDuplicates(
      groups,
      new Map([
        ["Knowledge", new Set(["Agent findings", "Context shards", "Reference docs", "Interface"])],
      ]),
    );

    expect(
      result.map((group) =>
        group.children.map((node) => (node.kind === "type" ? node.type.name : node.interface.name)),
      ),
    ).toEqual([["Child", "Reference docs", "Unmatched", "Interface"], ["Agent findings"]]);
    expect(groups[0].children).toHaveLength(5);
  });
});
