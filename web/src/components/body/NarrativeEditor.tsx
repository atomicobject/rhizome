import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { OntologyEditOp } from "../../api/types";
import {
  clearPersistedEditorDraft,
  readPersistedEditorDraft,
  writePersistedEditorDraft,
} from "../editing/draftStorage";
import { sameIgnoringLineEndings, useCompositionAwareFlush } from "../editing/editorFlush";
import { MarkdownEditor } from "../markdownEditor/MarkdownEditor";
import type { MarkdownEditorHandle } from "../markdownEditor/types";
import { useRhizomeMarkdownLinks } from "../markdownEditor/useRhizomeMarkdownLinks";
import { useActiveEditSession } from "../useOntologyEditSession";
import type { BodyRendererProps } from "./registry";

const NARRATIVE_DEBOUNCE_MS = 300;

function targetPathForNode({ node }: Pick<BodyRendererProps, "node">): string {
  if (node.kind === "note") return node.notePath;

  return node.ref.fragment ? `${node.notePath}#${node.ref.fragment}` : node.notePath;
}

// Matches narrative projection/replay normalization in pkg/ontology/edit_session.go.
function normalizeMarkdown(markdown: string): string {
  return markdown.replace(/[ \t\n]+$/, "");
}

type PendingNarrative = {
  markdown: string;
  previousMarkdown: string;
};

export function NarrativeEditor({ block, kindOrdinal, node, context }: BodyRendererProps) {
  const authoredMarkdown = block.markdown || "";
  const nodePath = useMemo(() => targetPathForNode({ node }), [node]);

  const operationID = useMemo(
    () =>
      `narrative:${encodeURIComponent(nodePath)}:${encodeURIComponent(node.ref.nodeId || node.id)}:${kindOrdinal}`,
    [kindOrdinal, node.id, node.ref.nodeId, nodePath],
  );

  const vaultKey = context.vaultKey ?? null;
  const editSession = useActiveEditSession();

  const [draft, setDraft] = useState(
    () => readPersistedEditorDraft(vaultKey, operationID) ?? authoredMarkdown,
  );

  const authoredRef = useRef(authoredMarkdown);
  const draftRef = useRef(draft);
  const nodeRef = useRef(node);
  const blockRef = useRef(block);
  const nodePathRef = useRef(nodePath);
  const operationIDRef = useRef(operationID);
  const previousMarkdownRef = useRef(authoredMarkdown);
  const lastEmittedMarkdownRef = useRef(authoredMarkdown);
  const sourceRevisionRef = useRef(context.workspace.sourceRevision);
  const onStageOpsRef = useRef(context.onStageOps);
  const inFlightRef = useRef<Promise<void> | null>(null);
  const queuedRef = useRef<PendingNarrative | null>(null);
  const editorRef = useRef<MarkdownEditorHandle | null>(null);

  const links = useRhizomeMarkdownLinks({
    currentPath: context.workspace.content.path,
    rendered: context.rendered ?? context.workspace.content.rendered,
    onOpen: context.onOpen,
  });

  useEffect(() => {
    nodeRef.current = node;
    blockRef.current = block;
    nodePathRef.current = nodePath;
    operationIDRef.current = operationID;
    onStageOpsRef.current = context.onStageOps;

    if (!sourceRevisionRef.current && context.workspace.sourceRevision) {
      sourceRevisionRef.current = context.workspace.sourceRevision;
    }
  }, [block, context.onStageOps, context.workspace.sourceRevision, node, nodePath, operationID]);

  const drainStageQueue = useCallback((): Promise<void> => {
    const active = inFlightRef.current;

    if (active) return active.then(() => drainStageQueue());

    if (!queuedRef.current || !onStageOpsRef.current) return Promise.resolve();
    const pending = queuedRef.current;
    queuedRef.current = null;
    let result: Promise<void> | void;

    try {
      result = onStageOpsRef.current([
        buildNarrativeOp(
          pending,
          operationIDRef.current,
          nodePathRef.current,
          nodeRef.current,
          blockRef.current,
          sourceRevisionRef.current,
        ),
      ]);
    } catch {
      if (!queuedRef.current) queuedRef.current = pending;

      return Promise.reject(new Error("Failed to stage narrative edit"));
    }

    const request = Promise.resolve(result);
    inFlightRef.current = request;

    return request.then(
      () => {
        if (inFlightRef.current === request) inFlightRef.current = null;

        return drainStageQueue();
      },
      (stageError: Error) => {
        if (inFlightRef.current === request) inFlightRef.current = null;

        if (!queuedRef.current) queuedRef.current = pending;
        throw stageError;
      },
    );
  }, []);

  const syncDraftFromEditor = useCallback(() => {
    const markdown = editorRef.current?.getMarkdown();

    if (markdown === undefined || sameIgnoringLineEndings(markdown, draftRef.current)) return;
    draftRef.current = markdown;
    setDraft(markdown);
    writePersistedEditorDraft(vaultKey, operationID, markdown);
  }, [operationID, vaultKey]);

  const stage = useCallback((): Promise<void> => {
    syncDraftFromEditor();
    const markdown = draftRef.current;

    if (!onStageOpsRef.current) return Promise.resolve();

    if (markdown !== lastEmittedMarkdownRef.current) {
      const pending = {
        markdown,
        previousMarkdown: previousMarkdownRef.current,
      };

      lastEmittedMarkdownRef.current = markdown;
      queuedRef.current = pending;
    }

    return drainStageQueue();
  }, [drainStageQueue, syncDraftFromEditor]);

  const { cancelScheduledFlush, flush, onCompositionEnd, scheduleFlush } = useCompositionAwareFlush(
    {
      editorRef,
      stage,
      onDiscard: () => {
        queuedRef.current = null;
        clearPersistedEditorDraft(vaultKey, operationID);
      },
      debounceMs: NARRATIVE_DEBOUNCE_MS,
    },
  );

  // A server response acknowledges a staged value through block.markdown. Keep
  // newer local typing intact while an earlier request is still in flight.
  useEffect(() => {
    const previousAuthored = authoredRef.current;
    authoredRef.current = authoredMarkdown;

    const draftMatchesAuthored =
      normalizeMarkdown(draftRef.current) === normalizeMarkdown(authoredMarkdown);

    const draftWasOnlyServerValue =
      normalizeMarkdown(draftRef.current) === normalizeMarkdown(previousAuthored);

    if (
      !queuedRef.current &&
      !inFlightRef.current &&
      (draftWasOnlyServerValue || draftMatchesAuthored)
    ) {
      cancelScheduledFlush();
      draftRef.current = authoredMarkdown;
      setDraft(authoredMarkdown);
      clearPersistedEditorDraft(vaultKey, operationID);
      lastEmittedMarkdownRef.current = authoredMarkdown;
      queuedRef.current = null;
    }
  }, [authoredMarkdown, cancelScheduledFlush, operationID, vaultKey]);

  const changeDraft = (markdown: string) => {
    if (!onStageOpsRef.current) return;
    draftRef.current = markdown;
    setDraft(markdown);
    writePersistedEditorDraft(vaultKey, operationID, markdown);
    scheduleFlush();
  };

  if (!authoredMarkdown.trim() && context.mode !== "edit") return null;

  // Marked while typing and, once staged, until the session saves or drops it.
  const hasChanges =
    draft !== authoredMarkdown || Boolean(editSession?.ops?.some((op) => op.id === operationID));

  return (
    <div className={`body-narrative body-narrative--editor${hasChanges ? " is-dirty" : ""}`}>
      <MarkdownEditor
        className="body-narrative__editor"
        documentId={operationID}
        editorRef={editorRef}
        value={draft}
        onChange={changeDraft}
        onBlur={() => void flush().catch(() => undefined)}
        onCompositionEnd={onCompositionEnd}
        readOnly={!context.onStageOps}
        ariaLabel="Narrative markdown"
        links={links}
      />
    </div>
  );
}

function buildNarrativeOp(
  pending: PendingNarrative,
  operationID: string,
  nodePath: string,
  node: BodyRendererProps["node"],
  block: BodyRendererProps["block"],
  sourceRevision: BodyRendererProps["context"]["workspace"]["sourceRevision"],
): OntologyEditOp {
  const expected = sourceRevision
    ? {
        sourceHash: sourceRevision.contentFingerprint,
        sourceContent: sourceRevision.content,
      }
    : undefined;

  return {
    id: operationID,
    kind: "setNarrative",
    path: nodePath,
    nodeId: node.ref.nodeId,
    markdown: pending.markdown,
    previousMarkdown: pending.previousMarkdown,
    rangeStart: block.range.start,
    rangeEnd: block.range.end,
    expected,
  };
}
