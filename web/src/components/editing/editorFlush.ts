import type { RefObject } from "react";
import { useCallback, useEffect, useRef } from "react";
import type { MarkdownEditorHandle } from "../markdownEditor/types";

export const EDITOR_FLUSH_EVENT = "rhizome:flush-editors";

const DISCARD_EDITOR_DRAFTS_EVENT = "rhizome:discard-editor-drafts";

type PendingCompositionFlush = {
  promise: Promise<void>;
  resolve: () => void;
  reject: (reason: Error) => void;
};

function createPendingCompositionFlush(): PendingCompositionFlush {
  let resolve: () => void = () => undefined;
  let reject: (reason: Error) => void = () => undefined;

  const promise = new Promise<void>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });

  return { promise, resolve, reject };
}

type EditorFlushDetail = {
  waitUntil: (pending: Promise<void>) => void;
};

export async function flushEditors(): Promise<void> {
  const pending: Promise<void>[] = [];
  window.dispatchEvent(
    new CustomEvent<EditorFlushDetail>(EDITOR_FLUSH_EVENT, {
      detail: { waitUntil: (work) => pending.push(work) },
    }),
  );
  await Promise.all(pending);
}

// CodeMirror normalizes CR and CRLF to LF when it builds a document, so a
// flush-time read must not treat that normalization as an authored edit.
export function sameIgnoringLineEndings(a: string, b: string): boolean {
  return a === b || a.replace(/\r\n?/g, "\n") === b.replace(/\r\n?/g, "\n");
}

function registerEditorFlush(event: Event, pending: Promise<void>): void {
  // SAFETY: EDITOR_FLUSH_EVENT is dispatched only by flushEditors above, which
  // supplies this exact detail contract to every registered editor.
  const detail = (event as CustomEvent<EditorFlushDetail>).detail;

  if (detail) {
    detail.waitUntil(pending);

    return;
  }

  void pending.catch(() => undefined);
}

export function useCompositionAwareFlush({
  editorRef,
  stage,
  onDiscard,
  debounceMs = 300,
}: {
  editorRef: RefObject<MarkdownEditorHandle | null>;
  stage: () => Promise<void>;
  onDiscard: () => void;
  debounceMs?: number;
}) {
  const stageRef = useRef(stage);
  const onDiscardRef = useRef(onDiscard);
  useEffect(() => {
    stageRef.current = stage;
    onDiscardRef.current = onDiscard;
  }, [onDiscard, stage]);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const compositionEndTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const compositionFlushRequestedRef = useRef(false);
  const pendingCompositionFlushRef = useRef<PendingCompositionFlush | null>(null);
  const discardedRef = useRef(false);

  const cancelScheduledFlush = useCallback(() => {
    if (timerRef.current === null) return;
    clearTimeout(timerRef.current);
    timerRef.current = null;
  }, []);

  const clearCompositionEndTimer = useCallback(() => {
    if (compositionEndTimerRef.current === null) return;
    clearTimeout(compositionEndTimerRef.current);
    compositionEndTimerRef.current = null;
  }, []);

  const flush = useCallback(
    (force = false): Promise<void> => {
      cancelScheduledFlush();

      if (discardedRef.current) {
        pendingCompositionFlushRef.current?.resolve();
        pendingCompositionFlushRef.current = null;

        return Promise.resolve();
      }

      if (!force && editorRef.current?.isComposing()) {
        compositionFlushRequestedRef.current = true;
        const pending = pendingCompositionFlushRef.current ?? createPendingCompositionFlush();
        pendingCompositionFlushRef.current = pending;

        return pending.promise;
      }

      compositionFlushRequestedRef.current = false;
      const staged = stageRef.current();
      const pending = pendingCompositionFlushRef.current;

      if (!pending) return staged;
      pendingCompositionFlushRef.current = null;

      return staged.then(
        () => pending.resolve(),
        (stageError: Error) => {
          pending.reject(stageError);
          throw stageError;
        },
      );
    },
    [cancelScheduledFlush, editorRef],
  );

  const scheduleFlush = useCallback(() => {
    discardedRef.current = false;
    cancelScheduledFlush();

    if (editorRef.current?.isComposing()) {
      compositionFlushRequestedRef.current = true;

      return;
    }

    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      void flush().catch(() => undefined);
    }, debounceMs);
  }, [cancelScheduledFlush, debounceMs, editorRef, flush]);

  const onCompositionEnd = useCallback(() => {
    clearCompositionEndTimer();
    compositionEndTimerRef.current = setTimeout(() => {
      compositionEndTimerRef.current = null;

      if (discardedRef.current) return;

      if (pendingCompositionFlushRef.current) {
        void flush().catch(() => undefined);
      } else if (compositionFlushRequestedRef.current) {
        scheduleFlush();
      }
    }, 0);
  }, [clearCompositionEndTimer, flush, scheduleFlush]);

  useEffect(() => {
    const flushEvent = (event: Event) => registerEditorFlush(event, flush());

    const discard = () => {
      discardedRef.current = true;
      cancelScheduledFlush();
      clearCompositionEndTimer();
      compositionFlushRequestedRef.current = false;
      pendingCompositionFlushRef.current?.resolve();
      pendingCompositionFlushRef.current = null;
      onDiscardRef.current();
    };

    window.addEventListener(EDITOR_FLUSH_EVENT, flushEvent);
    window.addEventListener(DISCARD_EDITOR_DRAFTS_EVENT, discard);

    return () => {
      window.removeEventListener(EDITOR_FLUSH_EVENT, flushEvent);
      window.removeEventListener(DISCARD_EDITOR_DRAFTS_EVENT, discard);
      cancelScheduledFlush();
      clearCompositionEndTimer();

      if (
        !discardedRef.current &&
        !compositionFlushRequestedRef.current &&
        !editorRef.current?.isComposing()
      ) {
        void flush(true).catch(() => undefined);
      } else {
        pendingCompositionFlushRef.current?.resolve();
        pendingCompositionFlushRef.current = null;
      }
    };
  }, [cancelScheduledFlush, clearCompositionEndTimer, editorRef, flush]);

  return { cancelScheduledFlush, flush, onCompositionEnd, scheduleFlush };
}
