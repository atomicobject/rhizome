import { markdown, markdownLanguage } from "@codemirror/lang-markdown";
import { syntaxTree } from "@codemirror/language";
import { EditorState } from "@codemirror/state";
import { describe, expect, it } from "vitest";
import { frontmatterBlock } from "./frontmatter";

function topLevelNodes(doc: string) {
  const state = EditorState.create({
    doc,
    extensions: [markdown({ base: markdownLanguage, extensions: [frontmatterBlock] })],
  });

  const names: string[] = [];
  syntaxTree(state)
    .topNode.cursor()
    .iterate((node) => {
      if (node.node.parent?.name === "Document") names.push(node.name);
    });

  return names;
}

const doc = "---\ntype: Spec\nsummary: Two words\n---\n\n# Title\n\nBody.\n";

describe("frontmatterBlock", () => {
  it("parses a leading YAML block as Frontmatter instead of a setext heading", () => {
    expect(topLevelNodes(doc)).toEqual(["Frontmatter", "ATXHeading1", "Paragraph"]);
  });

  it("ignores fences that do not start the document or never close", () => {
    expect(topLevelNodes("Intro\n\n---\nkey: value\n---\n")).not.toContain("Frontmatter");
    expect(topLevelNodes("---\nkey: value\nno close\n")).not.toContain("Frontmatter");
  });
});
