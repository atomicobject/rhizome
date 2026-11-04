import { afterEach, describe, expect, it } from "vitest";

import { appQueryClient } from "../api/queryClient";
import { queryKeys } from "../api/queryKeys";
import { typeAccentColor, typeAccentIndex } from "./typeAccent";

function setSummary(types: { name: string; count: number }[], interfaces: string[] = []) {
  appQueryClient.setQueryData(queryKeys.ontology.summary(), {
    types,
    interfaces: interfaces.map((name) => ({ name, implementors: [] })),
  });
}

describe("typeAccent", () => {
  afterEach(() => appQueryClient.removeQueries({ queryKey: queryKeys.ontology.summary() }));

  it("gives twelve types twelve different hues and never the neutral", () => {
    const names = Array.from({ length: 12 }, (_, i) => `Type${String(i).padStart(2, "0")}`);
    setSummary(names.map((name) => ({ name, count: 1 })));
    const slots = names.map(typeAccentIndex);

    expect(new Set(slots).size).toBe(12);
    expect(slots).not.toContain(8);
  });

  it("puts types with records first and keeps the thirteenth off the first type's hue", () => {
    const names = Array.from({ length: 13 }, (_, i) => `T${String(i).padStart(2, "0")}`);
    setSummary([{ name: "AAEmpty", count: 0 }, ...names.map((name) => ({ name, count: 3 }))]);

    expect(typeAccentIndex("T00")).not.toBe(typeAccentIndex("T12"));
    expect(typeAccentIndex("T01")).not.toBe(typeAccentIndex("T00"));
  });

  it("draws interfaces in the neutral and falls back to a hash for unknown names", () => {
    setSummary([{ name: "Spec", count: 2 }], ["SpecLike"]);

    expect(typeAccentColor("SpecLike")).toBe("var(--type-8)");
    expect(typeAccentColor("Unlisted")).toMatch(/^var\(--type-(?:[1-79]|1[0-3])\)$/);
  });
});
