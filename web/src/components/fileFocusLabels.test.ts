import { describe, expect, it } from "vitest";
import { fileFocusLabels, fileFocusRowHeight } from "./fileFocusLabels";
import { fileFocusGraph } from "./fileFocusGraph";

const measure = (text: string) => text.length * 10;

const code = (name: string) => ({ id: name, kind: "code", path: `src/${name}`, label: name });

describe("file focus labels", () => {
  it("keeps ordinary code names whole up to 240 pixels", () => {
    expect(fileFocusLabels([code("intent_profiles.go")], measure)[0].label).toBe(
      "intent_profiles.go",
    );
  });
  it("trims a shared prefix or test suffix before distinctive stems", () => {
    const prefix = "planner_shared_long_context_";
    expect(
      fileFocusLabels([code(`${prefix}alpha.go`), code(`${prefix}beta.go`)], measure).map(
        (n) => n.label,
      ),
    ).toEqual(["…alpha.go", "…beta.go"]);
    expect(
      fileFocusLabels(
        [code("long_distinctive_alpha_test.go"), code("long_distinctive_beta_test.go")],
        measure,
      ).map((n) => n.label),
    ).toEqual(["…alpha…", "…beta…"]);
  });
  it("disambiguates identical titles with parent folders", () => {
    expect(
      fileFocusLabels(
        [
          { id: "a", kind: "note", label: "search", path: "docs/code-anchors/search.md" },
          { id: "b", kind: "note", label: "search", path: "docs/subsystems/search.md" },
        ],
        measure,
      ).map((n) => n.labelDetail),
    ).toEqual(["code-anchors/", "subsystems/"]);
  });
  it("leaves room for both lines and a gap before the next group", () => {
    const note = {
      id: "a",
      kind: "note",
      label: "Search - Seed expansion strategies and retrieval",
    };

    expect(fileFocusRowHeight(note, measure)).toBe(134);

    const graph = fileFocusGraph(
      {
        nodes: [
          code("main.go"),
          note,
          { ...note, id: "b" },
          { id: "c", kind: "note", label: "Other" },
        ],
        edges: [],
      },
      "src/main.go",
      {
        path: "src/main.go",
        kind: "code",
        relatedNotes: [{ path: "spec.md", title: "Spec", reason: "coderef" }],
      },
    );

    const items = graph.nodes.filter((n) => ["a", "b", "c"].includes(n.id));
    expect(
      Math.abs(items.find((n) => n.id === "a")!.y! - items.find((n) => n.id === "b")!.y!),
    ).toBeGreaterThanOrEqual(134);
    const heading = graph.nodes.find((n) => n.id === "file-group:Documented by")!;
    expect(Math.abs(heading.y! - items.at(-1)!.y!)).toBeGreaterThanOrEqual(80);
  });
});
