import { describe, expect, it } from "vitest";
import type { NodeBodyBlock, WorkspaceContentNode } from "../../api/types";
import { narrativeClassName } from "./NarrativeView";

function lead(markdown: string, kind: WorkspaceContentNode["kind"] = "note") {
  const block: NodeBodyBlock = { kind: "narrative", markdown, range: { start: 0, end: 0 } };
  // SAFETY: narrativeClassName reads only kind and body from the node.
  const node = { kind, body: [block] } as WorkspaceContentNode;

  return narrativeClassName("body-narrative", { block, kindOrdinal: 0, node });
}

describe("narrativeClassName", () => {
  it("marks a note's lead line of links as metadata", () => {
    expect(lead("\nHome: [[DAI IP tracking]] · [[MOC DAI]]\n")).toContain("body-narrative--meta");
    expect(lead("Related: [Spec](docs/spec.md), [[Effort]]")).toContain("body-narrative--meta");
  });

  it("leaves prose, multi-line leads, and sections alone", () => {
    expect(lead("Note: see [[Effort]] for details")).toBe("body-narrative");
    expect(lead("Home: [[A]]\n\nMore prose.")).toBe("body-narrative");
    expect(lead("Home: [[A]] · [[B]]", "section")).toBe("body-narrative");
  });
});
