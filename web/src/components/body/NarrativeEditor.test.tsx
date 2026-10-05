import { EditorView } from "@codemirror/view";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { NodeBodyBlock, NodeWorkspace, WorkspaceContentNode } from "../../api/types";
import { readPersistedEditorDraftRecord } from "../editing/draftStorage";
import { flushEditors } from "../editing/editorFlush";
import { NarrativeEditor } from "./NarrativeEditor";
import type { BodyRenderContext } from "./registry";

const notePath = "docs/note.md";

const operationID = "narrative:docs%2Fnote.md:docs%2Fnote.md:0";

const revision = {
  notePath,
  contentFingerprint: "source-fingerprint",
  content: "# Note\n\nOriginal narrative",
};

function makeBlock(markdown = "Original narrative"): NodeBodyBlock {
  return {
    kind: "narrative",
    markdown,
    range: { start: 8, end: 26 },
  };
}

function makeWorkspace(markdown = "Original narrative"): NodeWorkspace {
  const node = makeNode(markdown);

  return {
    requestedRef: notePath,
    focusedNodeId: node.id,
    node: {
      ref: node.ref,
      notePath,
      title: "Note",
      locator: "NOTE",
    },
    content: {
      path: notePath,
      title: "Note",
      rendered: {
        path: notePath,
        title: "Note",
        rendered: markdown,
        links: [],
      },
    },
    nodes: [node],
    loaded: {
      rendered: true,
      assessment: false,
      structure: true,
      relations: false,
    },
    capabilities: emptyCapabilities(),
    status: emptyStatus(),
    version: "v1",
    sourceRevision: revision,
  };
}

function makeNode(markdown = "Original narrative"): WorkspaceContentNode {
  return {
    id: "node|note",
    kind: "note",
    ref: { notePath, kind: "NOTE", nodeId: notePath },
    notePath,
    status: emptyStatus(),
    capabilities: emptyCapabilities(),
    body: [makeBlock(markdown)],
    data: { title: "Note", locator: "NOTE" },
  };
}

function emptyCapabilities() {
  return {
    canEdit: true,
    canEditFields: false,
    canEditCollections: false,
    canNavigateChildren: false,
    canSubscribe: false,
  };
}

function emptyStatus() {
  return {
    dirty: false,
    validation: { issueCount: 0 },
    freshness: {},
    session: {},
    hasWarnings: false,
  };
}

function makeContext(
  workspace: NodeWorkspace,
  onStageOps?: BodyRenderContext["onStageOps"],
): BodyRenderContext {
  return {
    workspace,
    mode: "edit",
    vaultKey: "/vault",
    rendered: workspace.content.rendered,
    onStageOps,
    onOpen: vi.fn(),
    lookupNode: (id) => workspace.nodes?.find((node) => node.id === id),
  };
}

function renderEditor({
  markdown = "Original narrative",
  onStageOps = vi.fn(),
  workspace = makeWorkspace(markdown),
}: {
  markdown?: string;
  onStageOps?: BodyRenderContext["onStageOps"] | null;
  workspace?: NodeWorkspace;
} = {}) {
  return render(
    <NarrativeEditor
      block={makeBlock(markdown)}
      kindOrdinal={0}
      node={workspaceNode(workspace)}
      context={makeContext(workspace, onStageOps ?? undefined)}
    />,
  );
}

function workspaceNode(workspace: NodeWorkspace): WorkspaceContentNode {
  const node = workspace.nodes?.[0];

  if (!node || (node.kind !== "note" && node.kind !== "section" && node.kind !== "embedded")) {
    throw new Error("NarrativeEditor fixture requires a content node");
  }

  return node;
}

async function narrativeView() {
  const contentDOM = await screen.findByRole<HTMLElement>(
    "textbox",
    { name: "Narrative markdown" },
    { timeout: 10_000 },
  );

  const view = EditorView.findFromDOM(contentDOM);

  if (!view) throw new Error("Expected a CodeMirror narrative editor");

  return view;
}

function replaceNarrative(view: EditorView, next: string) {
  act(() => {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: next } });
  });
}

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
  window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "narrative-test");
});

afterEach(() => vi.useRealTimers());

describe("NarrativeEditor", () => {
  it("mounts the exact authored source in the narrative content DOM", async () => {
    renderEditor({ markdown: "Line one\n\n- [ ] Task" });
    const view = await narrativeView();

    expect(view.state.doc.toString()).toBe("Line one\n\n- [ ] Task");
    expect(view.contentDOM).toBe(screen.getByRole("textbox", { name: "Narrative markdown" }));
  });

  it("debounces typing into one witnessed operation with a stable id", async () => {
    const onStageOps = vi.fn();
    renderEditor({ onStageOps });
    const view = await narrativeView();
    vi.useFakeTimers();

    replaceNarrative(view, "First");
    replaceNarrative(view, "Latest draft");
    expect(onStageOps).not.toHaveBeenCalled();
    await act(async () => vi.advanceTimersByTime(300));

    expect(onStageOps).toHaveBeenCalledTimes(1);
    expect(onStageOps).toHaveBeenCalledWith([
      {
        id: operationID,
        kind: "setNarrative",
        path: notePath,
        nodeId: notePath,
        markdown: "Latest draft",
        previousMarkdown: "Original narrative",
        rangeStart: 8,
        rangeEnd: 26,
        expected: {
          sourceHash: "source-fingerprint",
          sourceContent: revision.content,
        },
      },
    ]);
  });

  it("flushes the latest draft before commit", async () => {
    const onStageOps = vi.fn();
    renderEditor({ onStageOps });
    const view = await narrativeView();
    replaceNarrative(view, "First");
    replaceNarrative(view, "Latest before save");

    await act(async () => flushEditors());

    expect(onStageOps).toHaveBeenCalledTimes(1);
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({ markdown: "Latest before save" }),
    ]);
  });

  it("keeps a rejected draft and retries it on the next flush", async () => {
    const onStageOps = vi
      .fn<NonNullable<BodyRenderContext["onStageOps"]>>()
      .mockRejectedValueOnce(new Error("stage failed"))
      .mockResolvedValue(undefined);

    renderEditor({ onStageOps });
    const view = await narrativeView();
    replaceNarrative(view, "Retry this narrative");

    await expect(flushEditors()).rejects.toThrow("stage failed");

    expect(readPersistedEditorDraftRecord("/vault", operationID)?.value).toBe(
      "Retry this narrative",
    );
    await act(async () => flushEditors());
    expect(onStageOps).toHaveBeenCalledTimes(2);
    expect(onStageOps).toHaveBeenLastCalledWith([
      expect.objectContaining({ markdown: "Retry this narrative" }),
    ]);
  });

  it("resets a clean document without recreating the editor", async () => {
    const onStageOps = vi.fn();
    const initialWorkspace = makeWorkspace();
    const rendered = renderEditor({ onStageOps, workspace: initialWorkspace });
    const view = await narrativeView();
    const editorElement = rendered.container.querySelector(".cm-editor");
    const refreshedWorkspace = makeWorkspace("Externally refreshed");

    rendered.rerender(
      <NarrativeEditor
        block={makeBlock("Externally refreshed")}
        kindOrdinal={0}
        node={workspaceNode(refreshedWorkspace)}
        context={makeContext(refreshedWorkspace, onStageOps)}
      />,
    );

    await waitFor(() => expect(view.state.doc.toString()).toBe("Externally refreshed"));
    expect(rendered.container.querySelector(".cm-editor")).toBe(editorElement);
  });

  it("keeps a dirty persisted draft through a workspace refresh", async () => {
    const onStageOps = vi.fn();
    const rendered = renderEditor({ onStageOps });
    const view = await narrativeView();
    vi.useFakeTimers();
    replaceNarrative(view, "Unsaved local draft");
    const refreshedWorkspace = makeWorkspace("Server refresh");

    rendered.rerender(
      <NarrativeEditor
        block={makeBlock("Server refresh")}
        kindOrdinal={0}
        node={workspaceNode(refreshedWorkspace)}
        context={makeContext(refreshedWorkspace, onStageOps)}
      />,
    );

    expect(view.state.doc.toString()).toBe("Unsaved local draft");
    expect(readPersistedEditorDraftRecord("/vault", operationID)?.value).toBe(
      "Unsaved local draft",
    );
  });

  it("stages nothing in a read-only context", async () => {
    renderEditor({ onStageOps: null });
    const view = await narrativeView();

    expect(view.contentDOM).toHaveAttribute("contenteditable", "false");
    replaceNarrative(view, "Should not stage");
    await act(async () => flushEditors());

    expect(view.state.doc.toString()).toBe("Original narrative");
    expect(window.localStorage.length).toBe(0);
  });

  it("clears a discarded persisted draft without staging it", async () => {
    const onStageOps = vi.fn();
    const rendered = renderEditor({ onStageOps });
    const view = await narrativeView();
    vi.useFakeTimers();
    replaceNarrative(view, "Discard this draft");
    expect(readPersistedEditorDraftRecord("/vault", operationID)?.value).toBe("Discard this draft");

    window.dispatchEvent(new Event("rhizome:discard-editor-drafts"));
    fireEvent.blur(view.contentDOM);
    rendered.unmount();
    await act(async () => vi.runAllTimers());

    expect(readPersistedEditorDraftRecord("/vault", operationID)).toBeNull();
    expect(onStageOps).not.toHaveBeenCalled();
  });

  it("restores a persisted draft in a newly mounted editor", async () => {
    const first = renderEditor();
    const firstView = await narrativeView();
    replaceNarrative(firstView, "Recovered narrative");
    expect(readPersistedEditorDraftRecord("/vault", operationID)?.value).toBe(
      "Recovered narrative",
    );
    first.unmount();

    renderEditor();
    const restoredView = await narrativeView();
    expect(restoredView.state.doc.toString()).toBe("Recovered narrative");
  });

  it("queues the latest staging request while a request is in flight", async () => {
    let resolveFirst: (() => void) | undefined;

    const firstStage = new Promise<void>((resolve) => {
      resolveFirst = resolve;
    });

    const onStageOps = vi
      .fn<NonNullable<BodyRenderContext["onStageOps"]>>()
      .mockReturnValueOnce(firstStage)
      .mockReturnValue(undefined);

    renderEditor({ onStageOps });
    const view = await narrativeView();

    replaceNarrative(view, "First staged draft");
    fireEvent.blur(view.contentDOM);
    expect(onStageOps).toHaveBeenCalledTimes(1);

    replaceNarrative(view, "Newest queued draft");
    let flushed = false;

    const pendingFlush = flushEditors().then(() => {
      flushed = true;
    });

    await act(async () => Promise.resolve());
    expect(onStageOps).toHaveBeenCalledTimes(1);
    expect(flushed).toBe(false);

    resolveFirst?.();
    await act(async () => pendingFlush);
    expect(onStageOps).toHaveBeenCalledTimes(2);
    expect(onStageOps).toHaveBeenLastCalledWith([
      expect.objectContaining({ markdown: "Newest queued draft" }),
    ]);
  });

  it("defers a flush during composition until the final text is available", async () => {
    const onStageOps = vi.fn();
    renderEditor({ onStageOps });
    const view = await narrativeView();

    fireEvent.compositionStart(view.contentDOM);
    replaceNarrative(view, "Intermediate composition");
    let flushed = false;

    const pendingFlush = flushEditors().then(() => {
      flushed = true;
    });

    await act(async () => Promise.resolve());

    expect(onStageOps).not.toHaveBeenCalled();
    expect(flushed).toBe(false);

    replaceNarrative(view, "Final composed character 漢");
    fireEvent.compositionEnd(view.contentDOM);
    await act(async () => pendingFlush);

    expect(onStageOps).toHaveBeenCalledTimes(1);
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({ markdown: "Final composed character 漢" }),
    ]);
  });

  it("does not stage a composition that ends after discard", async () => {
    const onStageOps = vi.fn();
    renderEditor({ onStageOps });
    const view = await narrativeView();
    vi.useFakeTimers();

    fireEvent.compositionStart(view.contentDOM);
    replaceNarrative(view, "Discarded composition");
    window.dispatchEvent(new Event("rhizome:discard-editor-drafts"));
    fireEvent.compositionEnd(view.contentDOM);
    await act(async () => vi.runAllTimers());

    expect(readPersistedEditorDraftRecord("/vault", operationID)).toBeNull();
    expect(onStageOps).not.toHaveBeenCalled();
  });

  it("does not stage an active composition during unmount", async () => {
    const onStageOps = vi.fn();
    const rendered = renderEditor({ onStageOps });
    const view = await narrativeView();

    fireEvent.compositionStart(view.contentDOM);
    replaceNarrative(view, "Unmounted composition");
    rendered.unmount();

    expect(onStageOps).not.toHaveBeenCalled();
  });
});
