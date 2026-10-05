import {
  type CompositionEvent,
  type KeyboardEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import {
  clearPersistedEditorDraft,
  readPersistedEditorDraft,
  type OntologyEditExpected,
  writePersistedEditorDraft,
} from "../editing/draftStorage";
import type { OntologyEditFieldValue } from "../../api/types";

type Props = {
  /** Current value as it lives on disk. */
  value: string;
  /** True when the surrounding session is in edit mode. */
  editing: boolean;
  /** True when this field has unsaved changes in the active edit session. */
  dirty?: boolean;
  /** Whether the field is missing on disk. Drives the empty-state display. */
  present?: boolean;
  /** Placeholder for missing/empty values. */
  placeholder?: string;
  /** Stage a setField op. Wired by the parent panel. */
  onStage?: (next: string) => void;
  /** Optional aria-label for the input control in edit mode. Defaults to no label. */
  ariaLabel?: string;
  /** Vault owner for the raw draft, supplied by the editing session. */
  vaultKey?: string | null;
  /** Canonical operation id for this field. */
  operationTarget?: string;
  /** Capture the source and field witness when a draft first changes. */
  onDraft?: (
    value: OntologyEditFieldValue,
    preserveWhenOriginal?: boolean,
  ) => OntologyEditExpected | undefined;
};

/**
 * Compact inline text field. In browse mode it renders as a static value; in
 * edit mode it presents an input that stages on blur (or Enter). Escape
 * cancels and reverts to the on-disk value. The widget never owns persistence
 * directly — it emits a single staged value to the parent panel.
 */
export function InlineTextWidget({
  value,
  editing,
  dirty = false,
  present = true,
  placeholder = "—",
  onStage,
  ariaLabel,
  vaultKey = null,
  operationTarget = "",
  onDraft,
}: Props) {
  const [draft, setDraft] = useState(
    () => readPersistedEditorDraft(vaultKey, operationTarget) ?? value,
  );

  const inputRef = useRef<HTMLInputElement | null>(null);
  const draftRef = useRef(draft);
  const valueRef = useRef(value);
  const onStageRef = useRef(onStage);
  const lastStagedRef = useRef<string | null>(null);
  const suppressBlurRef = useRef(false);
  const composingRef = useRef(false);

  useEffect(() => {
    draftRef.current = draft;
    valueRef.current = value;
    onStageRef.current = onStage;
  }, [draft, onStage, value]);

  // Reset the draft whenever the on-disk value changes (e.g. session refresh
  // after a commit) — but don't clobber an in-progress edit, indicated by the
  // input being focused.
  useEffect(() => {
    if (document.activeElement !== inputRef.current) {
      const persisted = readPersistedEditorDraft(vaultKey, operationTarget);
      const next = persisted ?? value;
      setDraft(next);
      draftRef.current = next;
    }

    if (lastStagedRef.current === value) {
      const persisted = readPersistedEditorDraft(vaultKey, operationTarget);

      if (persisted === null || persisted === value) {
        clearPersistedEditorDraft(vaultKey, operationTarget);
      }

      lastStagedRef.current = null;
    }
  }, [operationTarget, value, vaultKey]);

  useEffect(() => {
    const discard = () => {
      clearPersistedEditorDraft(vaultKey, operationTarget);
      draftRef.current = valueRef.current;
      setDraft(valueRef.current);
      lastStagedRef.current = null;
    };

    window.addEventListener("rhizome:discard-editor-drafts", discard);

    return () => window.removeEventListener("rhizome:discard-editor-drafts", discard);
  }, [operationTarget, vaultKey]);

  // Route changes can remove the input before blur runs. Flush the latest
  // dirty draft while the callback is still available to the component.
  useEffect(() => {
    return () => {
      if (composingRef.current) return;
      const next = draftRef.current;
      const current = valueRef.current;

      if (
        !onStageRef.current ||
        lastStagedRef.current === next ||
        (next === current && lastStagedRef.current === null)
      ) {
        return;
      }

      lastStagedRef.current = next;
      onStageRef.current(next);
    };
  }, []);

  const commit = useCallback(() => {
    if (
      composingRef.current ||
      !onStage ||
      lastStagedRef.current === draft ||
      (draft === value && lastStagedRef.current === null)
    ) {
      return;
    }

    lastStagedRef.current = draft;
    onStage(draft);
  }, [draft, onStage, value]);

  const handleKeyDown = useCallback(
    (event: KeyboardEvent<HTMLInputElement>) => {
      if (event.key === "Enter") {
        if (composingRef.current || event.nativeEvent.isComposing || event.keyCode === 229) {
          return;
        }

        event.preventDefault();
        suppressBlurRef.current = true;
        commit();
        inputRef.current?.blur();
      } else if (event.key === "Escape") {
        event.preventDefault();
        const next = lastStagedRef.current ?? value;
        setDraft(next);
        draftRef.current = next;
        clearPersistedEditorDraft(vaultKey, operationTarget);
        suppressBlurRef.current = true;
        inputRef.current?.blur();
      }
    },
    [commit, operationTarget, value, vaultKey],
  );

  const handleBlur = useCallback(() => {
    if (suppressBlurRef.current) {
      suppressBlurRef.current = false;

      return;
    }

    commit();
  }, [commit]);

  const handleCompositionStart = useCallback((_event: CompositionEvent<HTMLInputElement>) => {
    composingRef.current = true;
  }, []);

  const handleCompositionEnd = useCallback((_event: CompositionEvent<HTMLInputElement>) => {
    composingRef.current = false;
  }, []);

  if (!editing) {
    if (!present || value.trim() === "") {
      return <span className="widget-text widget-text--empty">{placeholder}</span>;
    }

    return <span className={`widget-text${dirty ? " is-dirty" : ""}`}>{value}</span>;
  }

  return (
    <input
      ref={inputRef}
      type="text"
      className={`widget-text widget-text--input${dirty ? " is-dirty" : ""}`}
      value={draft}
      placeholder={placeholder}
      aria-label={ariaLabel}
      onChange={(event) => {
        const next = event.target.value;
        draftRef.current = next;
        setDraft(next);

        const expected = onDraft?.(
          next === "" ? { kind: "unset" } : { kind: "scalar", scalar: next },
          next === value && lastStagedRef.current !== null,
        );

        if (next === value && lastStagedRef.current === null) {
          clearPersistedEditorDraft(vaultKey, operationTarget);
        } else {
          writePersistedEditorDraft(vaultKey, operationTarget, next, expected);
        }
      }}
      onFocus={() => {
        suppressBlurRef.current = false;
      }}
      onCompositionStart={handleCompositionStart}
      onCompositionEnd={handleCompositionEnd}
      onBlur={handleBlur}
      onKeyDown={handleKeyDown}
    />
  );
}
