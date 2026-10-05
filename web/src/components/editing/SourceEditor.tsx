import { useCallback, useEffect, useRef, useState } from "react";

import type { NodeRef, NodeWorkspace, OntologyEditOp, RenderedFile } from "../../api/types";
import { MarkdownEditor } from "../markdownEditor/MarkdownEditor";
import type { MarkdownEditorHandle } from "../markdownEditor/types";
import { useRhizomeMarkdownLinks } from "../markdownEditor/useRhizomeMarkdownLinks";
import {
  clearPersistedEditorDraft,
  readPersistedEditorDraft,
  readPersistedEditorDraftRecord,
  type OntologyEditExpected,
  writePersistedEditorDraft,
} from "./draftStorage";
import { sameIgnoringLineEndings, useCompositionAwareFlush } from "./editorFlush";

type SourceRevision = NonNullable<NodeWorkspace["sourceRevision"]>;

function sourceExpected(revision: SourceRevision): OntologyEditExpected {
  return {
    sourceHash: revision.contentFingerprint,
    sourceContent: revision.content,
  };
}

function sourceOperation(
  revision: SourceRevision,
  content: string,
  expected: OntologyEditExpected,
): OntologyEditOp {
  return {
    id: `source:${encodeURIComponent(revision.notePath)}`,
    kind: "setSource",
    path: revision.notePath,
    markdown: content,
    previousMarkdown: expected.sourceContent || revision.content,
    expected,
  };
}

async function clearDraftAfterStage(
  staged: Promise<void> | undefined,
  isDiscarded: () => boolean,
  clearDraft: () => void,
): Promise<void> {
  try {
    await staged;
  } catch {
    return;
  }

  if (!isDiscarded()) clearDraft();
}

export function SourceEditor({
  revision,
  vaultKey = null,
  targetLine,
  rendered,
  onOpen,
  onStageOps,
}: {
  revision: SourceRevision;
  vaultKey?: string | null;
  targetLine?: number;
  rendered?: RenderedFile | null;
  onOpen?: (path: string, target?: "current" | "stack" | "beside") => void;
  onStageOps: (ops: OntologyEditOp[]) => Promise<void> | undefined;
}) {
  const operationTarget = `source:${encodeURIComponent(revision.notePath)}`;

  const [persistedAtMount] = useState(() =>
    readPersistedEditorDraftRecord(vaultKey, operationTarget),
  );

  const [draft, setDraft] = useState(() => persistedAtMount?.value ?? revision.content);
  const draftRef = useRef(draft);
  const revisionRef = useRef(revision);
  const stageRef = useRef(onStageOps);
  const editorRef = useRef<MarkdownEditorHandle | null>(null);
  const stagedContentRef = useRef(persistedAtMount?.expected?.sourceContent ?? revision.content);
  const capturedExpectedRef = useRef(persistedAtMount?.expected);
  const links = useRhizomeMarkdownLinks({ currentPath: revision.notePath, rendered, onOpen });

  useEffect(() => {
    const previousRevision = revisionRef.current;
    revisionRef.current = revision;
    stageRef.current = onStageOps;
    const persisted = readPersistedEditorDraftRecord(vaultKey, operationTarget);

    if (persisted?.expected) capturedExpectedRef.current = persisted.expected;

    if (draftRef.current === previousRevision.content && !capturedExpectedRef.current) {
      setDraft(revision.content);
      draftRef.current = revision.content;
      stagedContentRef.current = revision.content;
      clearPersistedEditorDraft(vaultKey, operationTarget);
    }

    if (draftRef.current === revision.content && stagedContentRef.current === draftRef.current) {
      stagedContentRef.current = revision.content;
      capturedExpectedRef.current = undefined;
      clearPersistedEditorDraft(vaultKey, operationTarget);
    }
  }, [onStageOps, operationTarget, revision, vaultKey]);

  const captureExpected = useCallback(
    (next: string): OntologyEditExpected | undefined => {
      const persisted = readPersistedEditorDraftRecord(vaultKey, operationTarget);

      if (persisted?.expected) capturedExpectedRef.current = persisted.expected;
      const original = capturedExpectedRef.current?.sourceContent;

      if (original !== undefined && next === original) {
        if (stagedContentRef.current !== original) return capturedExpectedRef.current;
        capturedExpectedRef.current = undefined;
        stagedContentRef.current = next;
        clearPersistedEditorDraft(vaultKey, operationTarget);

        return undefined;
      }

      if (!capturedExpectedRef.current && next !== revisionRef.current.content) {
        capturedExpectedRef.current = sourceExpected(revisionRef.current);
      }

      return capturedExpectedRef.current;
    },
    [operationTarget, vaultKey],
  );

  const syncDraftFromEditor = useCallback(() => {
    const next = editorRef.current?.getMarkdown();

    if (next === undefined || sameIgnoringLineEndings(next, draftRef.current)) return;
    draftRef.current = next;
    setDraft(next);
    const expected = captureExpected(next);

    if (expected) writePersistedEditorDraft(vaultKey, operationTarget, next, expected);
    else clearPersistedEditorDraft(vaultKey, operationTarget);
  }, [captureExpected, operationTarget, vaultKey]);

  const stage = useCallback((): Promise<void> => {
    syncDraftFromEditor();
    const current = revisionRef.current;

    if (draftRef.current === stagedContentRef.current) return Promise.resolve();

    const expected =
      capturedExpectedRef.current ||
      readPersistedEditorDraftRecord(vaultKey, operationTarget)?.expected;

    if (!expected) return Promise.resolve();
    stagedContentRef.current = draftRef.current;
    let staged: Promise<void>;

    try {
      staged = Promise.resolve(
        stageRef.current([sourceOperation(current, draftRef.current, expected)]),
      ).catch((stageError: Error) => {
        stagedContentRef.current = current.content;
        throw stageError;
      });
    } catch (error) {
      stagedContentRef.current = current.content;
      staged = Promise.reject(error);
    }

    return staged;
  }, [operationTarget, syncDraftFromEditor, vaultKey]);

  const { flush, onCompositionEnd, scheduleFlush } = useCompositionAwareFlush({
    editorRef,
    stage,
    onDiscard: () => {
      capturedExpectedRef.current = undefined;
      stagedContentRef.current = revisionRef.current.content;
      clearPersistedEditorDraft(vaultKey, operationTarget);
    },
  });

  const changeDraft = (next: string) => {
    draftRef.current = next;
    setDraft(next);
    const expected = captureExpected(next);

    if (expected) writePersistedEditorDraft(vaultKey, operationTarget, next, expected);
    else clearPersistedEditorDraft(vaultKey, operationTarget);
    scheduleFlush();
  };

  return (
    <div className="ontology-source-editor">
      <label
        htmlFor={`source-${encodeURIComponent(revision.notePath)}`}
        onClick={() => editorRef.current?.focus()}
      >
        Markdown source
      </label>
      <strong className="ontology-source-editor__warning">
        This replaces the complete file. Review the unified diff before saving.
      </strong>
      <MarkdownEditor
        className={`ontology-source-editor__markdown${targetLine ? " is-issue-target" : ""}`}
        documentId={operationTarget}
        editorRef={editorRef}
        inputId={`source-${encodeURIComponent(revision.notePath)}`}
        ariaLabel="Markdown source"
        value={draft}
        onChange={changeDraft}
        onBlur={() => void flush().catch(() => undefined)}
        onCompositionEnd={onCompositionEnd}
        onCommitShortcut={() => void flush().catch(() => undefined)}
        revealLine={targetLine}
        links={links}
      />
      <small>Rhizome checks protected headings, identifiers, and links before Save.</small>
    </div>
  );
}

export function EmptyNarrativeEditor({
  revision,
  nodeRef,
  vaultKey = null,
  onStageOps,
}: {
  revision: SourceRevision;
  nodeRef: NodeRef;
  vaultKey?: string | null;
  onStageOps: (ops: OntologyEditOp[]) => Promise<void> | undefined;
}) {
  const operationTarget = `narrative-insert:${encodeURIComponent(nodeRef.notePath)}:${encodeURIComponent(nodeRef.nodeId || nodeRef.fragment || "root")}`;

  const [draft, setDraft] = useState(
    () => readPersistedEditorDraft(vaultKey, operationTarget) ?? "",
  );

  const draftRef = useRef(draft);
  const discardedRef = useRef(false);

  useEffect(() => {
    const discard = () => {
      discardedRef.current = true;
      clearPersistedEditorDraft(vaultKey, operationTarget);
      draftRef.current = "";
      setDraft("");
    };

    window.addEventListener("rhizome:discard-editor-drafts", discard);

    return () => window.removeEventListener("rhizome:discard-editor-drafts", discard);
  }, [operationTarget, vaultKey]);

  return (
    <div className="body-narrative body-narrative--empty-editor">
      <label htmlFor={`narrative-${encodeURIComponent(revision.notePath)}`}>Add narrative</label>
      <textarea
        id={`narrative-${encodeURIComponent(revision.notePath)}`}
        value={draft}
        placeholder="Write the note body…"
        onChange={(event) => {
          discardedRef.current = false;
          const next = event.target.value;
          draftRef.current = next;
          setDraft(next);
          writePersistedEditorDraft(vaultKey, operationTarget, next);
        }}
        onBlur={() => {
          const markdown = draftRef.current;

          if (!markdown.trim() || discardedRef.current) return;

          const staged = onStageOps([
            {
              id: operationTarget,
              kind: "insertNarrative",
              path: nodeRef.fragment ? `${nodeRef.notePath}#${nodeRef.fragment}` : nodeRef.notePath,
              nodeId: nodeRef.nodeId,
              structuralFingerprint: nodeRef.structuralFingerprint,
              markdown: markdown.trimEnd(),
              expected: {
                sourceHash: revision.contentFingerprint,
                sourceContent: revision.content,
              },
            },
          ]);

          void clearDraftAfterStage(
            staged,
            () => discardedRef.current,
            () => clearPersistedEditorDraft(vaultKey, operationTarget),
          );
        }}
      />
    </div>
  );
}
