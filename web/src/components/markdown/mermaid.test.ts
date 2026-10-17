// @vitest-environment node
import { describe, expect, it } from "vitest";
import { renderMermaid } from "./mermaid";

describe("Mermaid syntax rendered by Pele", () => {
  it.each([
    ["flowchart LR\n A[Markdown] --> B[Context]", "flowchart"],
    ["sequenceDiagram\n Author->>Rhizome: Save a note", "sequence"],
    ["erDiagram\n NOTE ||--o{ LINK : contains", "er"],
    ['pie title Knowledge\n "Decisions" : 35\n "References" : 65', "pie"],
  ])("renders %s", async (source, type) => {
    const svg = await renderMermaid(source, "test-diagram");
    expect(svg).toContain(`pele-${type}`);
    expect(svg).toContain('viewBox="');
    expect(svg).not.toMatch(/NaN|undefined/);
  });

  it("rejects malformed diagrams", async () => {
    await expect(renderMermaid("not a diagram", "invalid")).rejects.toThrow();
  });

  it("keeps authored labels, links, callbacks, and images inert", async () => {
    const svg = await renderMermaid(
      'flowchart LR\n A["<script>alert(1)</script>"] --> B\n click A "https://example.com"\n click B callback',
      "untrusted",
    );

    expect(svg).not.toMatch(/<script|<a\b|<foreignObject|\son\w+=|href=/i);

    const image = await renderMermaid(
      'flowchart LR\n A@{ img: "https://example.com/track.png", label: "Image" }',
      "image",
    );

    expect(image).not.toMatch(/<image|href=|https:\/\/example.com/);
  });

  it("namespaces accessible titles for repeated diagrams", async () => {
    const source = "flowchart LR\n accTitle: Note workflow\n A --> B";
    const first = await renderMermaid(source, "first");
    const second = await renderMermaid(source, "second");
    expect(first).toContain('aria-labelledby="first-title"');
    expect(second).toContain('aria-labelledby="second-title"');
  });
});
