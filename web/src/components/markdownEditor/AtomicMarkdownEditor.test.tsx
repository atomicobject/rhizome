import "@testing-library/jest-dom/vitest";

import { forceParsing } from "@codemirror/language";
import { EditorView } from "@codemirror/view";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createRef } from "react";
import { describe, expect, it, vi } from "vitest";
import { AtomicMarkdownEditor } from "./AtomicMarkdownEditor";
import type { MarkdownEditorHandle } from "./types";

type AtomicCodeMirrorEditorHandle = MarkdownEditorHandle;

describe("AtomicMarkdownEditor", () => {
  function requiredElement<T extends Element>(container: ParentNode, selector: string): T {
    const element = container.querySelector<T>(selector);

    if (!element) throw new Error(`Expected editor element: ${selector}`);

    return element;
  }

  it("mounts without rewriting Rhizome and Obsidian markdown source", async () => {
    const handleRef = createRef<AtomicCodeMirrorEditorHandle | null>();
    const onChange = vi.fn();

    const markdown = [
      "---",
      "title: Demo",
      "aliases:",
      "  - Demo Note",
      "---",
      "",
      "id:: ^SPEC-0001-US1",
      "owner:: [[People/Ada|Ada]]",
      "#inline-tag",
      "> [!NOTE] Rhizome callout",
      "![[assets/diagram.png|Diagram]]",
      "Inline math $x^2$ and block math:",
      "$$x = 1$$",
      ":::unknown-directive keep=true",
      "",
      "Paragraph with [[docs/spec.md#^SPEC-0001-US1|story link]] and block ^block-id",
      "",
      "| A | B |",
      "| --- | --- |",
      "| 1 | 2 |",
      "",
      '<section data-kind="raw">Keep this HTML</section>',
      "",
      '```ts title="demo"',
      "const x = 1;",
      "```",
    ].join("\n");

    render(<AtomicMarkdownEditor value={markdown} onChange={onChange} editorRef={handleRef} />);

    await waitFor(() => expect(handleRef.current).not.toBeNull());
    expect(handleRef.current?.getMarkdown()).toBe(markdown);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("does not decorate frontmatter metadata as a heading", async () => {
    const { container } = render(
      <AtomicMarkdownEditor value={"---\ntype: Spec\n---\n"} onChange={vi.fn()} />,
    );

    const metadataLine = await waitFor(() => {
      const line = [...container.querySelectorAll<HTMLElement>(".cm-line")].find(
        (candidate) => candidate.textContent === "type: Spec",
      );

      expect(line).toBeDefined();

      return line!;
    });

    expect(metadataLine).not.toHaveClass("cm-atomic-heading");
    expect(metadataLine.querySelector(".cm-atomic-heading")).toBeNull();
  });

  it("preserves raw islands and wikilinks when editing a neighboring block", async () => {
    const onChange = vi.fn();

    const markdown = [
      "- [ ] Toggle this task",
      "",
      "| A | B |",
      "| --- | --- |",
      "| 1 | 2 |",
      "",
      '<section data-kind="raw">Keep this HTML</section>',
      "",
      '```ts title="demo"',
      "const x = 1;",
      "```",
      "",
      "See [[docs/spec.md#^story|Story]].",
    ].join("\n");

    const { container } = render(<AtomicMarkdownEditor value={markdown} onChange={onChange} />);
    await waitFor(() =>
      expect(
        container.querySelector<HTMLInputElement>("input.cm-atomic-task-checkbox"),
      ).not.toBeNull(),
    );

    fireEvent.click(requiredElement<HTMLInputElement>(container, "input.cm-atomic-task-checkbox"));

    await waitFor(() =>
      expect(onChange).toHaveBeenCalledWith(
        markdown.replace("- [ ] Toggle this task", "- [x] Toggle this task"),
      ),
    );
  });

  it("continues and removes Markdown list markers with Enter and Backspace", async () => {
    const handleRef = createRef<AtomicCodeMirrorEditorHandle | null>();

    const { container } = render(
      <AtomicMarkdownEditor value="- First item" onChange={vi.fn()} editorRef={handleRef} />,
    );

    await waitFor(() => expect(handleRef.current).not.toBeNull());
    const content = requiredElement<HTMLElement>(container, ".cm-content");
    const view = EditorView.findFromDOM(content);

    if (!view) throw new Error("Expected CodeMirror editor view");

    view.dispatch({ selection: { anchor: view.state.doc.length } });
    view.focus();
    // WHY: the list commands read the syntax tree, which CodeMirror parses
    // within a ~20ms budget per transaction and finishes in the background.
    // A loaded worker can leave the new line unparsed, so Backspace falls back
    // to deleting one character. Finish the parse before each keystroke.
    const parsed = () => forceParsing(view, view.state.doc.length, 5_000);
    expect(parsed()).toBe(true);
    fireEvent.keyDown(content, { key: "Enter", code: "Enter" });
    expect(handleRef.current?.getMarkdown()).toBe("- First item\n- ");

    expect(parsed()).toBe(true);
    fireEvent.keyDown(content, { key: "Backspace", code: "Backspace" });
    expect(handleRef.current?.getMarkdown()).toBe("- First item\n  ");
    expect(handleRef.current?.getMarkdown()).not.toContain("\n- ");
  });

  it("synchronizes a controlled value without emitting a synthetic edit", async () => {
    const handleRef = createRef<AtomicCodeMirrorEditorHandle | null>();
    const onChange = vi.fn();

    const { rerender } = render(
      <AtomicMarkdownEditor value="Original" onChange={onChange} editorRef={handleRef} />,
    );

    await waitFor(() => expect(handleRef.current?.getMarkdown()).toBe("Original"));

    rerender(
      <AtomicMarkdownEditor value="Replacement" onChange={onChange} editorRef={handleRef} />,
    );

    await waitFor(() => expect(handleRef.current?.getMarkdown()).toBe("Replacement"));
    expect(onChange).not.toHaveBeenCalled();
  });

  it("blocks document mutations in read-only mode", async () => {
    const handleRef = createRef<AtomicCodeMirrorEditorHandle | null>();
    const onChange = vi.fn();

    const { container } = render(
      <AtomicMarkdownEditor
        value="- [ ] Task"
        onChange={onChange}
        readOnly
        editorRef={handleRef}
      />,
    );

    await waitFor(() => expect(handleRef.current).not.toBeNull());
    const content = container.querySelector(".cm-content");
    expect(content).toHaveAttribute("contenteditable", "false");

    await waitFor(() =>
      expect(
        container.querySelector<HTMLInputElement>("input.cm-atomic-task-checkbox"),
      ).not.toBeNull(),
    );
    const checkbox = requiredElement<HTMLInputElement>(container, "input.cm-atomic-task-checkbox");
    fireEvent.click(checkbox);

    expect(handleRef.current?.getMarkdown()).toBe("- [ ] Task");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("toggles read-only without remounting the Atomic document session", async () => {
    const handleRef = createRef<AtomicCodeMirrorEditorHandle | null>();
    const onChange = vi.fn();

    const { rerender } = render(
      <AtomicMarkdownEditor
        value="- [ ] Task"
        onChange={onChange}
        documentId="sessioned-note"
        editorRef={handleRef}
      />,
    );

    const editorElement = await waitFor(() => requiredElement<HTMLElement>(document, ".cm-editor"));

    rerender(
      <AtomicMarkdownEditor
        value="- [ ] Task"
        onChange={onChange}
        documentId="sessioned-note"
        readOnly
        editorRef={handleRef}
      />,
    );

    await waitFor(() =>
      expect(document.querySelector(".cm-content")).toHaveAttribute("contenteditable", "false"),
    );
    expect(requiredElement<HTMLElement>(document, ".cm-editor")).toBe(editorElement);
    expect(handleRef.current?.getMarkdown()).toBe("- [ ] Task");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("resolves and opens Rhizome wikilinks through Atomic's wiki extension", async () => {
    const onOpenWikiLink = vi.fn();

    const resolveWikiLink = vi.fn((target: string) => ({
      target: target === "Spec" ? "docs/spec.md" : target,
      label: target === "Spec" ? "Resolved Spec" : target,
      status: "resolved" as const,
    }));

    const { container } = render(
      <AtomicMarkdownEditor
        value="See [[Spec]] and [[docs/other.md|Other]]."
        onChange={vi.fn()}
        links={{ resolveWikiLink, onOpenWikiLink }}
      />,
    );

    await waitFor(() =>
      expect(
        container.querySelector<HTMLElement>(
          '[data-wiki-link-target="Spec"].cm-atomic-wiki-link-resolved',
        ),
      ).toHaveTextContent("Resolved Spec"),
    );

    await waitFor(() =>
      expect(
        container.querySelector<HTMLElement>('[data-wiki-link-target="docs/other.md"]'),
      ).toHaveTextContent("Other"),
    );

    const labelled = requiredElement<HTMLElement>(
      container,
      '[data-wiki-link-target="docs/other.md"]',
    );

    expect(labelled).toHaveAttribute("role", "link");
    expect(labelled).toHaveAttribute("tabindex", "0");
    fireEvent.click(labelled);
    expect(onOpenWikiLink).toHaveBeenCalledWith("docs/other.md");
    onOpenWikiLink.mockClear();
    fireEvent.keyDown(labelled, { key: "Enter" });
    expect(onOpenWikiLink).toHaveBeenCalledWith("docs/other.md");
  });

  it("preserves the editor session when link metadata changes", async () => {
    const handleRef = createRef<AtomicCodeMirrorEditorHandle | null>();

    const firstResolver = vi.fn(() => ({
      target: "docs/first.md",
      label: "First target",
      status: "resolved" as const,
    }));

    const secondResolver = vi.fn(() => ({
      target: "docs/second.md",
      label: "Second target",
      status: "resolved" as const,
    }));

    const { container, rerender } = render(
      <AtomicMarkdownEditor
        value="See [[Spec]]."
        onChange={vi.fn()}
        links={{ resolveWikiLink: firstResolver }}
        editorRef={handleRef}
      />,
    );

    await waitFor(() =>
      expect(
        container.querySelector<HTMLElement>('[data-wiki-link-target="Spec"]'),
      ).toHaveTextContent("First target"),
    );
    const editorElement = requiredElement<HTMLElement>(container, ".cm-editor");
    const contentDOM = requiredElement<HTMLElement>(container, ".cm-content");
    const editorView = EditorView.findFromDOM(contentDOM);
    editorView?.dispatch({
      changes: { from: editorView.state.doc.length, insert: " Draft" },
      selection: { anchor: 4 },
    });
    rerender(
      <AtomicMarkdownEditor
        value="See [[Spec]]."
        onChange={vi.fn()}
        links={{ resolveWikiLink: secondResolver }}
        editorRef={handleRef}
      />,
    );

    await waitFor(() =>
      expect(
        container.querySelector<HTMLElement>('[data-wiki-link-target="Spec"]'),
      ).toHaveTextContent("Second target"),
    );
    expect(requiredElement<HTMLElement>(container, ".cm-editor")).toBe(editorElement);
    expect(editorView?.state.selection.main.anchor).toBe(4);
    expect(handleRef.current?.getMarkdown()).toBe("See [[Spec]]. Draft");
    expect(secondResolver).toHaveBeenCalledWith("Spec");
  });

  it("renders GFM tables with Atomic's table editor", async () => {
    const handleRef = createRef<AtomicCodeMirrorEditorHandle | null>();
    const onChange = vi.fn();
    const markdown = ["Name | Value", ":--- | ---:", "Alpha | Beta"].join("\n");

    const { container } = render(
      <AtomicMarkdownEditor value={markdown} onChange={onChange} editorRef={handleRef} />,
    );

    await waitFor(() => expect(handleRef.current).not.toBeNull());
    await waitFor(() => expect(container.querySelector(".cm-atomic-table")).not.toBeNull());
    expect(handleRef.current?.getMarkdown()).toBe(markdown);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("reveals a source line at its start", async () => {
    const handleRef = createRef<AtomicCodeMirrorEditorHandle | null>();
    render(
      <AtomicMarkdownEditor
        value={"first\nsecond\nthird"}
        onChange={vi.fn()}
        editorRef={handleRef}
      />,
    );

    await waitFor(() => expect(handleRef.current).not.toBeNull());
    handleRef.current?.revealLine(2);
    const contentDOM = screen.getByRole<HTMLElement>("textbox", { name: "Markdown editor" });
    const view = EditorView.findFromDOM(contentDOM);

    expect(view?.state.selection.main.anchor).toBe(6);
    expect(contentDOM).toHaveFocus();
  });
});
