import { render, waitFor } from "@testing-library/react";
import { createRef } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { renderMermaid } from "../src/components/markdown/mermaid";
import { Markdown, markdownLinks, type MarkdownLinkTo } from "./markdown";

// oxlint-disable-next-line anti-slop/no-module-mocking -- Mermaid renders through a lazily loaded boundary.
vi.mock("../src/components/markdown/mermaid", () => ({ renderMermaid: vi.fn() }));

const mockedRenderMermaid = vi.mocked(renderMermaid);

const node = (notePath: string) => ({ notePath, kind: "NOTE" });

// Every target that ends in "claim" resolves; anything else is unresolved.
const resolve = (target: string): MarkdownLinkTo | undefined =>
  /claim(\.md)?(#.*)?$/.test(target) ? { node: node("assertions/claim.md") } : undefined;

const mark = (at: number) => ({ at, label: "This source links to the assertion here" });

// One document per structure a citation can sit in. Each holds exactly one
// link to the claim and names the element the mark must land on.
const cases: [name: string, source: string, block: string][] = [
  [
    "a paragraph after a fence with blank lines",
    "Intro.\n\n```md\nSee [[claim]].\n\n[the claim](../assertions/claim.md)\n```\n\nLater, [[claim]] again.",
    "p",
  ],
  ["a tight list item", "Intro\n- [[claim]]\n- other", "li"],
  [
    "a tight list nested in a loose list",
    "- first\n\n- second\n  - [[claim]]\n  - sibling\n\n- third",
    "li",
  ],
  ["a loose list item's paragraph", "- first\n\n- see [[claim]]\n\n- third", "p"],
  ["a task list item", "- [ ] todo\n- [x] [[claim]] done", "li"],
  ["a table cell", "| a | b |\n| - | - |\n| 1 | [[claim]] |\n| 2 | 3 |", "tr"],
  ["a table alias", "| Reference |\n| --- |\n| [[claim\\|Alias]] |", "tr"],
  ["a heading", "# Title\n\n## About [[claim]]\n\nBody.", "h2"],
  [
    "a reference link resolved from a later definition",
    "Intro.\n\nSee [the claim][c] here.\n\n[c]: ../assertions/claim.md",
    "p",
  ],
  ["an aliased wikilink", "Read [[claim|the aliased claim]] now.", "p"],
  ["a blockquote's paragraph", "> quoted [[claim]]\n> continues", "p"],
  ["a continued blockquote", "> quoted\n> continued [[claim]]", "p"],
  ["CRLF line endings", "Intro.\r\n\r\n- one\r\n- [[claim]]\r\n", "li"],
  ["an inline link with a fragment", "Intro.\n\nSee [the claim](../assertions/claim.md#Top).", "p"],
];

describe("markdownLinks", () => {
  it.each(cases)("finds the link in %s at its source offset", (_name, source) => {
    const links = markdownLinks(source);

    expect(links).toHaveLength(1);
    expect(links[0].target).toMatch(/claim/);
    // The offset is the link's own first character in the exact input.
    expect(source.slice(links[0].offset, links[0].offset + 1)).toBe("[");
    expect(source.indexOf(links[0].target.replace(/^\.\.\//, ""), links[0].offset)).toBeGreaterThan(
      links[0].offset,
    );
  });

  it("returns links in source order, decoded, with aliases and fragments stripped only by the caller", () => {
    const source = "[b](notes/b%20two.md) then [[a|Alias]] and [[c#Heading]]";
    const links = markdownLinks(source);

    expect(links).toEqual([
      { target: "notes/b two.md", offset: 0 },
      { target: "a", offset: source.indexOf("[[a") },
      { target: "c#Heading", offset: source.indexOf("[[c") },
    ]);
  });

  it("ignores code, definitions, external links, fragments, and unsafe schemes", () => {
    const source = [
      "Use `[[claim]]` inline.",
      "",
      "    [[claim]] indented",
      "",
      "~~~",
      "[the claim](../assertions/claim.md)",
      "~~~",
      "",
      "[def]: ../assertions/claim.md",
      "",
      "[site](https://example.test/claim) [mail](mailto:a@b.test) [here](#claim) [bad](javascript:alert(1)) [app](obsidian://open?vault=claim)",
      "",
      "www.example.test/claim",
    ].join("\n");

    expect(markdownLinks(source)).toEqual([]);
  });

  it("keeps a wikilink literal when its own text holds no target", () => {
    expect(markdownLinks("[[ ]] and [[|alias]]")).toEqual([]);
  });

  it("keeps escaped wikilinks literal and recognizes an unescaped link after them", () => {
    const source = String.raw`\[[claim]] then [[other]] and \\[[claim]]`;

    expect(markdownLinks(source)).toEqual([
      { target: "other", offset: source.indexOf("[[other]]") },
      { target: "claim", offset: source.lastIndexOf("[[claim]]") },
    ]);
  });

  it.each(["__init__.py", "_draft_", "claim|*Alias*", String.raw`claim|Alias \[label\]`])(
    "preserves wiki syntax containing Markdown punctuation: %s",
    (value) => {
      const source = `Before [[${value}]] after.`;
      expect(markdownLinks(source)).toEqual([{ target: value.split("|")[0], offset: 7 }]);

      const { container } = render(
        <Markdown source={source} resolveLink={(target) => ({ node: node(target) })} />,
      );

      expect(container.querySelectorAll("a")).toHaveLength(1);
      expect(container).toHaveTextContent("Before");
      expect(container).toHaveTextContent("after.");
    },
  );

  it("keeps an escaped embed bang as text before an unescaped wikilink", () => {
    const source = String.raw`\![[claim]]`;
    expect(markdownLinks(source)).toEqual([{ target: "claim", offset: 2 }]);
    const { container } = render(<Markdown source={source} resolveLink={resolve} />);
    expect(container.querySelector("p")?.firstChild?.textContent).toBe("!");
    expect(container.querySelectorAll("a")).toHaveLength(1);
  });

  it.each([String.raw`\[\[claim]]`, "&lbrack;[claim]]"])(
    "keeps decoded brackets literal in %s before a real link",
    (literal) => {
      const source = `${literal} then [[claim]]`;
      expect(markdownLinks(source)).toEqual([
        { target: "claim", offset: source.lastIndexOf("[[claim]]") },
      ]);
      const { container } = render(<Markdown source={source} resolveLink={resolve} />);
      expect(container.querySelectorAll("a")).toHaveLength(1);
      expect(container.querySelector("p")?.firstChild?.textContent).toBe("[[claim]] then ");
    },
  );

  it("keeps wikilink-shaped text inside an existing Markdown link as its label", () => {
    const source = "[Read [[claim]] here](notes/guide.md)";

    expect(markdownLinks(source)).toEqual([{ target: "notes/guide.md", offset: 0 }]);
    const { container } = render(<Markdown source={source} resolveLink={resolve} />);

    expect(container.querySelectorAll("a a")).toHaveLength(0);
    expect(container).toHaveTextContent("Read [[claim]] here");
  });

  it.each(["[[claim]](notes/guide.md)", "[[claim]][reference]\n\n[reference]: notes/guide.md"])(
    "preserves an explicit Markdown destination in %s",
    (source) => {
      expect(markdownLinks(source)).toEqual([{ target: "notes/guide.md", offset: 0 }]);
    },
  );
});

describe("Markdown", () => {
  afterEach(() => {
    mockedRenderMermaid.mockReset();
  });

  it.each(cases)(
    "marks exactly the innermost block holding the link in %s",
    (_name, source, block) => {
      const [link] = markdownLinks(source);
      const ref = createRef<HTMLElement>();

      const { container } = render(
        <Markdown source={source} resolveLink={resolve} mark={{ ...mark(link.offset), ref }} />,
      );

      const marks = container.querySelectorAll(".rz-markdown-mark");

      expect(marks).toHaveLength(1);
      expect(marks[0].tagName.toLowerCase()).toBe(block);
      expect(marks[0].querySelector(".rz-markdown-mark-label")).toHaveTextContent(
        "This source links to the assertion here",
      );
      const resolved = marks[0].querySelector("a.rz-note-link");

      expect(resolved).not.toBeNull();
      expect(resolved).toHaveAttribute("href", expect.stringContaining("assertions%2Fclaim.md"));
      expect(ref.current).toBe(marks[0]);
    },
  );

  it("marks nothing when the offset is inside code or past the document", () => {
    const source = "Intro.\n\n```\n[[claim]]\n```\n";

    for (const at of [source.indexOf("[[claim"), source.length + 10]) {
      const { container, unmount } = render(<Markdown source={source} mark={mark(at)} />);

      expect(container.querySelector(".rz-markdown-mark")).toBeNull();
      unmount();
    }
  });

  it("moves the ref when the mark changes and clears it when the mark goes", () => {
    const source = "- [[claim]]\n- [[other]]\n";
    const [first, second] = markdownLinks(source);
    const ref = createRef<HTMLElement>();

    const view = (at?: number) => (
      <Markdown
        source={source}
        resolveLink={resolve}
        mark={at === undefined ? undefined : { ...mark(at), ref }}
      />
    );

    const { rerender } = render(view(first.offset));

    expect(ref.current).toHaveTextContent("claim");
    rerender(view(second.offset));
    expect(ref.current).toHaveTextContent("other");
    rerender(view(undefined));
    expect(ref.current).toBeNull();
  });

  it("renders pending, resolved, unresolved, external, in-page, and blocked links", () => {
    const source =
      "[[claim]] [[missing|Gone]] [web](https://example.test/) [note](#top) [bad](javascript:alert(1)) [data](data:text/plain,x)";

    const pending = render(<Markdown source={source} />);

    expect(pending.container.querySelector("a")).toHaveAttribute("href", "https://example.test/");
    expect(pending.container.querySelectorAll("a")).toHaveLength(2);
    expect(pending.container.querySelector(".rz-markdown-unresolved")).toBeNull();
    expect(pending.container).toHaveTextContent("claim Gone web note bad data");
    pending.unmount();

    const { container } = render(<Markdown source={source} resolveLink={resolve} />);
    const links = container.querySelectorAll("a");

    expect(links).toHaveLength(3);
    expect(links[0]).toHaveClass("rz-note-link");
    expect(links[0]).toHaveTextContent("claim");
    expect(container.querySelector(".rz-markdown-unresolved")).toHaveTextContent(
      "Gone (unresolved)",
    );
    expect(container.querySelector(".rz-markdown-unresolved")).toHaveAttribute(
      "title",
      "Unresolved target: missing",
    );
    expect(links[1]).toHaveAttribute("target", "_blank");
    expect(links[1]).toHaveAttribute("rel", "noreferrer noopener");
    expect(links[2]).toHaveAttribute("href", "#top");
    expect(links[2]).not.toHaveAttribute("target");
    expect(container).toHaveTextContent("bad data");
    expect(container.innerHTML).not.toContain("javascript:");
    expect(container.innerHTML).not.toContain("data:");
  });

  it("keeps raw HTML literal and never loads an image", () => {
    const { container } = render(
      <Markdown
        source={
          "<script>alert(1)</script> <b>bold</b>\n\n![a picture](https://example.test/p.png)\n\n![[pic.png]]"
        }
        resolveLink={resolve}
      />,
    );

    expect(container.querySelector("script, b, img")).toBeNull();
    expect(container).toHaveTextContent("<script>alert(1)</script> <b>bold</b>");
    expect(container.querySelector(".rz-markdown-image")).toHaveTextContent(
      "a picture https://example.test/p.png",
    );
    expect(container.querySelector(".rz-markdown-unresolved")).toHaveTextContent("pic.png");
  });

  it("renders the document's structure, not blank-line segments", () => {
    const source = [
      "# Title",
      "",
      "Some **bold** and *italic* text with `code`.",
      "",
      "1. first",
      "2. second",
      "",
      "> quote",
      "",
      "| a | b |",
      "| - | - |",
      "| 1 | 2 |",
      "",
      "```ts",
      "const x = 1;",
      "",
      "const y = 2;",
      "```",
      "",
      "- [ ] todo",
      "",
      "Done.",
    ].join("\n");

    const { container } = render(<Markdown source={source} />);

    expect(container.querySelector("strong")).toHaveTextContent("bold");
    expect(container.querySelector("em")).toHaveTextContent("italic");
    expect(container.querySelector("ol")?.querySelectorAll("li")).toHaveLength(2);
    expect(container.querySelector("blockquote")).toHaveTextContent("quote");
    expect(container.querySelector("table td")).toHaveTextContent("1");
    expect(container.querySelector("pre > code")).toHaveTextContent("const x = 1; const y = 2;");
    expect(container.querySelector("input[type=checkbox]")).not.toBeNull();
    expect(container.querySelector("h1")).toHaveTextContent("Title");
    expect(container.firstElementChild).toHaveClass("markdown", "rz-markdown");
  });

  it("keeps a leading title when it holds the selected citation", () => {
    const source = "# About [[claim]]\n\nBody.";

    const { container } = render(
      <Markdown
        source={source}
        resolveLink={resolve}
        skipTitle
        mark={mark(markdownLinks(source)[0].offset)}
      />,
    );

    expect(container.querySelectorAll(".rz-markdown-mark")).toHaveLength(1);
    expect(container.querySelector(".rz-markdown-mark a")).toHaveTextContent("claim");
  });

  it("hides a leading title with skipTitle without shifting the mark", () => {
    const source = "# The note\n\nBody [[claim]].\n\n# Not the title";
    const [link] = markdownLinks(source);

    const { container } = render(
      <Markdown
        source={source}
        resolveLink={resolve}
        mark={mark(link.offset)}
        skipTitle
        className="ak-body"
      />,
    );

    expect(container.querySelectorAll("h1")).toHaveLength(1);
    expect(container.querySelector("h1")).toHaveTextContent("Not the title");
    expect(container.querySelector("p.rz-markdown-mark")).toHaveTextContent("Body claim");
    expect(container.firstElementChild).toHaveClass("ak-body");
  });

  it("renders Mermaid fences and highlights code through the host", async () => {
    mockedRenderMermaid.mockResolvedValue('<svg aria-label="flow"></svg>');

    const { container } = render(
      <Markdown source={"```mermaid\ngraph TD\nA-->B\n```\n\n```ts\nconst value = 1;\n```"} />,
    );

    await waitFor(() => expect(container.querySelector(".mermaid-block svg")).not.toBeNull());
    await waitFor(() => expect(container.querySelector("pre > code.hljs")).not.toBeNull());
    expect(mockedRenderMermaid).toHaveBeenCalledWith("graph TD\nA-->B", expect.any(String));
  });
});
