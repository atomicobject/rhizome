import { EditorView } from "@codemirror/view";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { readPersistedEditorDraftRecord, writePersistedEditorDraft } from "./draftStorage";
import { flushEditors } from "./editorFlush";
import { EmptyNarrativeEditor, SourceEditor } from "./SourceEditor";

const revision = {
  notePath: "notes/a.md",
  contentFingerprint: "base-hash",
  content: "---\ntype: Note\n---\n",
};

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
  window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "source-test");
});

afterEach(() => vi.useRealTimers());

async function sourceView() {
  const contentDOM = await screen.findByRole<HTMLElement>(
    "textbox",
    { name: "Markdown source" },
    { timeout: 10_000 },
  );

  const view = EditorView.findFromDOM(contentDOM);

  if (!view) throw new Error("Expected a CodeMirror source editor");

  return view;
}

function replaceSource(view: EditorView, next: string) {
  act(() => {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: next } });
  });
}

describe("SourceEditor", () => {
  it("mounts the exact whole-file source in the labelled content DOM", async () => {
    render(<SourceEditor revision={revision} onStageOps={vi.fn()} />);
    const view = await sourceView();

    expect(view.state.doc.toString()).toBe(revision.content);
    expect(view.contentDOM).toHaveAttribute("id", "source-notes%2Fa.md");
  });

  it("reveals the requested diagnostic line", async () => {
    const { container } = render(
      <SourceEditor revision={revision} targetLine={2} onStageOps={vi.fn()} />,
    );

    const view = await sourceView();

    await waitFor(() => expect(view.state.selection.main.anchor).toBe(4));
    expect(view.contentDOM).toHaveFocus();
    expect(container.querySelector(".ontology-source-editor__markdown")).toHaveClass(
      "is-issue-target",
    );
  });

  it("coalesces continuous source input into one witnessed operation", async () => {
    const stage = vi.fn();
    render(<SourceEditor revision={revision} onStageOps={stage} />);
    const view = await sourceView();
    vi.useFakeTimers();
    replaceSource(view, `${revision.content}a`);
    replaceSource(view, `${revision.content}ab`);
    replaceSource(view, `${revision.content}abc`);
    expect(stage).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(300));
    expect(stage).toHaveBeenCalledTimes(1);
    expect(stage).toHaveBeenCalledWith([
      expect.objectContaining({
        id: "source:notes%2Fa.md",
        kind: "setSource",
        markdown: `${revision.content}abc`,
        expected: {
          sourceHash: "base-hash",
          sourceContent: revision.content,
        },
      }),
    ]);
  });

  it("clears a discarded persisted draft without staging it", async () => {
    const stage = vi.fn();

    const rendered = render(
      <SourceEditor revision={revision} vaultKey="/vault" onStageOps={stage} />,
    );

    const view = await sourceView();
    vi.useFakeTimers();
    replaceSource(view, `${revision.content}discarded`);
    expect(readPersistedEditorDraftRecord("/vault", "source:notes%2Fa.md")?.value).toBe(
      `${revision.content}discarded`,
    );

    window.dispatchEvent(new Event("rhizome:discard-editor-drafts"));
    expect(readPersistedEditorDraftRecord("/vault", "source:notes%2Fa.md")).toBeNull();
    fireEvent.blur(view.contentDOM);
    rendered.unmount();
    act(() => vi.runAllTimers());

    expect(stage).not.toHaveBeenCalled();
  });

  it("keeps the original source witness when the revision rerenders", async () => {
    const stage = vi.fn();

    const updatedRevision = {
      ...revision,
      contentFingerprint: "external-hash",
      content: `${revision.content}external`,
    };

    const { rerender } = render(<SourceEditor revision={revision} onStageOps={stage} />);
    const view = await sourceView();
    vi.useFakeTimers();
    replaceSource(view, `${revision.content}draft`);
    rerender(<SourceEditor revision={updatedRevision} onStageOps={stage} />);
    act(() => vi.advanceTimersByTime(300));

    expect(stage).toHaveBeenCalledWith([
      expect.objectContaining({
        markdown: `${revision.content}draft`,
        previousMarkdown: revision.content,
        expected: {
          sourceHash: "base-hash",
          sourceContent: revision.content,
        },
      }),
    ]);
  });

  it("keeps an editor flush pending until source staging finishes", async () => {
    let resolveStage: (() => void) | undefined;
    const staged = new Promise<void>((resolve) => (resolveStage = resolve));
    const stage = vi.fn(() => staged);
    render(<SourceEditor revision={revision} onStageOps={stage} />);
    const view = await sourceView();
    replaceSource(view, `${revision.content}changed`);
    replaceSource(view, `${revision.content}latest before save`);

    let flushed = false;

    const pendingFlush = flushEditors().then(() => {
      flushed = true;
    });

    await act(async () => Promise.resolve());
    expect(flushed).toBe(false);
    expect(stage).toHaveBeenCalledTimes(1);
    expect(stage).toHaveBeenCalledWith([
      expect.objectContaining({ markdown: `${revision.content}latest before save` }),
    ]);

    resolveStage?.();
    await act(async () => pendingFlush);
    expect(flushed).toBe(true);
  });

  it("resets rejected staging so the next flush retries the source", async () => {
    const stage = vi
      .fn()
      .mockRejectedValueOnce(new Error("stage failed"))
      .mockResolvedValue(undefined);

    render(<SourceEditor revision={revision} vaultKey="/vault" onStageOps={stage} />);
    const view = await sourceView();
    const changed = `${revision.content}retry this source`;
    replaceSource(view, changed);

    await expect(flushEditors()).rejects.toThrow("stage failed");

    expect(readPersistedEditorDraftRecord("/vault", "source:notes%2Fa.md")?.value).toBe(changed);
    await act(async () => flushEditors());
    expect(stage).toHaveBeenCalledTimes(2);
    expect(stage).toHaveBeenLastCalledWith([expect.objectContaining({ markdown: changed })]);
  });

  it("defers a flush during composition until the final source is available", async () => {
    const stage = vi.fn();
    render(<SourceEditor revision={revision} onStageOps={stage} />);
    const view = await sourceView();

    fireEvent.compositionStart(view.contentDOM);
    replaceSource(view, `${revision.content}intermediate`);
    let flushed = false;

    const pendingFlush = flushEditors().then(() => {
      flushed = true;
    });

    await act(async () => Promise.resolve());

    expect(stage).not.toHaveBeenCalled();
    expect(flushed).toBe(false);

    replaceSource(view, `${revision.content}final 漢`);
    fireEvent.compositionEnd(view.contentDOM);
    await act(async () => pendingFlush);

    expect(stage).toHaveBeenCalledTimes(1);
    expect(stage).toHaveBeenCalledWith([
      expect.objectContaining({ markdown: `${revision.content}final 漢` }),
    ]);
  });

  it("does not stage a composition that ends after discard", async () => {
    const stage = vi.fn();
    render(<SourceEditor revision={revision} vaultKey="/vault" onStageOps={stage} />);
    const view = await sourceView();
    vi.useFakeTimers();

    fireEvent.compositionStart(view.contentDOM);
    replaceSource(view, `${revision.content}discarded composition`);
    window.dispatchEvent(new Event("rhizome:discard-editor-drafts"));
    fireEvent.compositionEnd(view.contentDOM);
    await act(async () => vi.runAllTimers());

    expect(readPersistedEditorDraftRecord("/vault", "source:notes%2Fa.md")).toBeNull();
    expect(stage).not.toHaveBeenCalled();
  });

  it("does not stage an active composition during unmount", async () => {
    const stage = vi.fn();
    const rendered = render(<SourceEditor revision={revision} onStageOps={stage} />);
    const view = await sourceView();

    fireEvent.compositionStart(view.contentDOM);
    replaceSource(view, `${revision.content}unmounted composition`);
    rendered.unmount();

    expect(stage).not.toHaveBeenCalled();
  });

  it("does not stage a CRLF note that was only focused and flushed", async () => {
    const stage = vi.fn();
    const crlf = { ...revision, content: "---\r\ntype: Note\r\n---\r\nBody line\r\n" };
    render(<SourceEditor revision={crlf} onStageOps={stage} />);
    const view = await sourceView();
    view.focus();
    fireEvent.blur(view.contentDOM);
    await act(async () => flushEditors());

    expect(stage).not.toHaveBeenCalled();
  });

  it("flushes the current source with the commit shortcut", async () => {
    const stage = vi.fn();
    render(<SourceEditor revision={revision} onStageOps={stage} />);
    const view = await sourceView();
    replaceSource(view, `${revision.content}shortcut`);

    fireEvent.keyDown(view.contentDOM, { key: "Enter", ctrlKey: true });

    expect(stage).toHaveBeenCalledWith([
      expect.objectContaining({ markdown: `${revision.content}shortcut` }),
    ]);
  });

  it("keeps a pending source reversion across an equivalent revision rerender", async () => {
    const stage = vi.fn();
    const { rerender } = render(<SourceEditor revision={revision} onStageOps={stage} />);
    const view = await sourceView();
    replaceSource(view, `${revision.content}changed`);
    fireEvent.blur(view.contentDOM);
    replaceSource(view, revision.content);
    rerender(<SourceEditor revision={{ ...revision }} onStageOps={stage} />);
    fireEvent.blur(view.contentDOM);

    expect(stage).toHaveBeenCalledTimes(2);
    expect(stage).toHaveBeenLastCalledWith([
      expect.objectContaining({
        markdown: revision.content,
        expected: { sourceHash: "base-hash", sourceContent: revision.content },
      }),
    ]);
  });

  it("uses a persisted source witness after the editor is recreated", async () => {
    const recoveredDraft = `${revision.content}recovered`;
    writePersistedEditorDraft("/vault", "source:notes%2Fa.md", recoveredDraft, {
      sourceHash: "base-hash",
      sourceContent: revision.content,
    });
    const stage = vi.fn();
    render(
      <SourceEditor
        revision={{
          ...revision,
          contentFingerprint: "external-hash",
          content: `${revision.content}external`,
        }}
        vaultKey="/vault"
        onStageOps={stage}
      />,
    );
    const view = await sourceView();
    expect(view.state.doc.toString()).toBe(recoveredDraft);
    fireEvent.blur(view.contentDOM);

    expect(stage).toHaveBeenCalledWith([
      expect.objectContaining({
        markdown: recoveredDraft,
        previousMarkdown: revision.content,
        expected: {
          sourceHash: "base-hash",
          sourceContent: revision.content,
        },
      }),
    ]);
  });

  it("opens a rendered wikilink through the note pane callback", async () => {
    const source = `${revision.content}\nSee [[notes/target|Target]].`;
    const onOpen = vi.fn();

    const { container } = render(
      <SourceEditor
        revision={{ ...revision, content: source }}
        rendered={{
          path: revision.notePath,
          title: "A",
          rendered: source,
          links: [{ text: "notes/target", target: "notes/target.md", kind: "wikilink" }],
        }}
        onOpen={onOpen}
        onStageOps={vi.fn()}
      />,
    );

    await sourceView();

    const link = await waitFor(() => {
      const element = container.querySelector<HTMLElement>(
        '[data-wiki-link-target="notes/target"]',
      );

      expect(element).not.toBeNull();

      return element;
    });

    fireEvent.click(link!);

    await waitFor(() => expect(onOpen).toHaveBeenCalledWith("notes/target.md", "stack"));
  });

  it("stages the first narrative insertion with its source witness", () => {
    const stage = vi.fn();
    render(
      <EmptyNarrativeEditor
        revision={revision}
        nodeRef={{ notePath: "notes/a.md", kind: "NOTE" }}
        onStageOps={stage}
      />,
    );
    const input = screen.getByRole("textbox", { name: "Add narrative" });
    fireEvent.change(input, { target: { value: "First paragraph." } });
    fireEvent.blur(input);
    expect(stage).toHaveBeenCalledWith([
      expect.objectContaining({
        id: "narrative-insert:notes%2Fa.md:root",
        kind: "insertNarrative",
        path: "notes/a.md",
        markdown: "First paragraph.",
        expected: { sourceHash: "base-hash", sourceContent: revision.content },
      }),
    ]);
  });
});
