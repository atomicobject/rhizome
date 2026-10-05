import {
  type FocusEvent,
  type KeyboardEvent,
  type ReactNode,
  useEffect,
  useRef,
  useState,
} from "react";

import { listPublicViewFieldCandidates } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import { useStagedQuery } from "../staging/stagedQuery";
import type { OntologyEditSessionResponse, ViewFieldCapability } from "../api/types";
import { type JsonValue } from "../api/parse";
import type { CellEditValue, EditCandidate } from "./ConfiguredTableCellHelpers";
import {
  EMPTY_EDIT_CANDIDATES,
  candidateForInput,
  candidateInputValue,
  capabilityLabel,
  cellValues,
} from "./ConfiguredTableCellHelpers";
import { relationDisplayValue } from "./ConfiguredTableCellReadOnly";
import { useReturnFocusOnClose } from "./focusManagement";

/**
 * Shows the relation as links and edits it on request. A list relation edits
 * as removable items plus an add picker, because a native multi-select
 * replaces the whole selection on a plain click.
 */
export function NodeEditableCell({
  viewID,
  field,
  capability,
  edit,
  value,
  list,
  dirty,
  editSession,
  resolved = true,
  display,
  onStage,
}: {
  viewID?: string;
  field: string;
  capability: ViewFieldCapability | undefined;
  edit: NonNullable<ViewFieldCapability["edit"]>;
  value: JsonValue | undefined;
  list: boolean;
  dirty: boolean;
  editSession?: OntologyEditSessionResponse | null;
  /** False when the row has no titled relation values, so display needs candidates. */
  resolved?: boolean;
  display?: (candidates: EditCandidate[]) => ReactNode;
  onStage: (value: CellEditValue, list?: boolean) => Promise<void> | void;
}) {
  const [editing, setEditing] = useState(false);
  const editorRef = useRef<HTMLSpanElement>(null);
  const triggerRef = useReturnFocusOnClose<HTMLButtonElement>(editing);
  const embeddedCandidates = edit.candidates ?? EMPTY_EDIT_CANDIDATES;
  const label = capabilityLabel(capability);

  const candidateQuery = useStagedQuery({
    subject: queryKeys.views.fieldCandidates(viewID ?? "", field),
    session: editSession ?? null,
    queryFn: ({ signal, editSession: session }) =>
      listPublicViewFieldCandidates(viewID ?? "", field, {
        limit: 50,
        editSession: session,
        signal,
      }),
    enabled:
      (editing || (!resolved && cellValues(value).length > 0)) &&
      Boolean(viewID) &&
      embeddedCandidates.length === 0,
  });

  // Only the first load blocks the control; refetches keep the current list.
  const loading = candidateQuery.isLoading;

  useEffect(() => {
    if (editing && !loading) editorRef.current?.querySelector("select")?.focus();
  }, [editing, loading]);

  const candidates =
    embeddedCandidates.length > 0
      ? embeddedCandidates
      : (candidateQuery.data?.candidates ?? EMPTY_EDIT_CANDIDATES);

  if (!editing) {
    return (
      <span className={`configured-view__relation-cell${dirty ? " is-dirty" : ""}`}>
        <span className="configured-view__relation-display">{display?.(candidates)}</span>
        <button
          ref={triggerRef}
          type="button"
          className="configured-view__relation-edit"
          aria-label={`Edit ${label}`}
          title={`Edit ${label}`}
          onClick={() => setEditing(true)}
        >
          ✎
        </button>
      </span>
    );
  }

  const selectedValues = cellValues(value).map(
    (currentValue) => candidateForInput(candidates, currentValue)?.value ?? currentValue,
  );

  const labelFor = (item: string) => {
    const candidate = candidateForInput(candidates, item);

    return candidate ? candidateInputValue(candidates, candidate) : relationDisplayValue(item);
  };

  const close = () => setEditing(false);

  const editorProps = {
    ref: editorRef,
    onKeyDown: (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    },
    onBlur: (event: FocusEvent) => {
      const next = event.relatedTarget;

      if (!(next instanceof Node) || !event.currentTarget.contains(next)) close();
    },
  };

  if (!list) {
    return (
      <span className="configured-view__relation-editor" {...editorProps}>
        <select
          className={`configured-view__cell-editor configured-view__cell-editor--enum${dirty ? " is-dirty" : ""}`}
          aria-label={`Edit ${label}`}
          value={selectedValues[0] ?? ""}
          disabled={loading}
          onChange={(event) => {
            void onStage(event.currentTarget.value);
            close();
          }}
        >
          {loading ? (
            <option value="" disabled>
              Loading…
            </option>
          ) : null}
          <option value="">Unset</option>
          {selectedValues[0] && !candidateForInput(candidates, selectedValues[0]) ? (
            <option value={selectedValues[0]}>{labelFor(selectedValues[0])}</option>
          ) : null}
          {candidates.map((candidate) => (
            <option key={candidate.value} value={candidate.value}>
              {candidateInputValue(candidates, candidate)}
            </option>
          ))}
        </select>
      </span>
    );
  }

  return (
    <ListValueEditor
      label={label}
      values={selectedValues}
      options={candidates.map((candidate) => ({
        value: candidate.value,
        label: candidateInputValue(candidates, candidate),
      }))}
      labelFor={labelFor}
      dirty={dirty}
      loading={loading}
      onChange={(next) => void onStage(next, true)}
      onClose={close}
    />
  );
}

/**
 * Edits a list value as removable items plus an add picker, because a native
 * multi-select replaces the whole selection on a plain click.
 */
export function ListValueEditor({
  label,
  values,
  options,
  labelFor,
  dirty,
  loading = false,
  onChange,
  onClose,
}: {
  label: string;
  values: string[];
  options: Array<{ value: string; label: string }>;
  labelFor: (value: string) => string;
  dirty: boolean;
  loading?: boolean;
  onChange: (next: string[]) => void;
  onClose: () => void;
}) {
  // Edits reach the row only after the view refetches; keep the ones made
  // since opening so consecutive adds and removes compound. `known` holds the
  // lists this editor started from or staged; any other incoming value (a
  // discard, another client) replaces the draft.
  const [draft, setDraft] = useState<{ known: string[][]; next: string[] } | null>(null);
  const editorRef = useRef<HTMLSpanElement>(null);
  const draftApplies = draft?.known.some((list) => sameList(list, values)) ?? false;
  const current = draft && draftApplies ? draft.next : values;

  useEffect(() => {
    if (!loading) editorRef.current?.querySelector("select")?.focus();
  }, [loading]);

  const change = (next: string[]) => {
    // Keep focus inside the editor before a removed item's button unmounts.
    editorRef.current?.querySelector("select")?.focus();
    setDraft({ known: [...(draft && draftApplies ? draft.known : [values]), next], next });
    onChange(next);
  };

  return (
    <span
      ref={editorRef}
      className="configured-view__relation-editor configured-view__relation-editor--list"
      onKeyDown={(event) => {
        if (event.key === "Escape") onClose();
      }}
      onBlur={(event) => {
        const next = event.relatedTarget;

        if (!(next instanceof Node) || !event.currentTarget.contains(next)) onClose();
      }}
    >
      {current.map((item, index) => (
        <span key={`${item}:${index}`} className="configured-view__relation-chip">
          <span>{labelFor(item)}</span>
          <button
            type="button"
            aria-label={`Remove ${labelFor(item)}`}
            onClick={() => change(current.filter((_, other) => other !== index))}
          >
            ×
          </button>
        </span>
      ))}
      <select
        className={`configured-view__cell-editor configured-view__cell-editor--enum${dirty ? " is-dirty" : ""}`}
        aria-label={`Add ${label}`}
        value=""
        disabled={loading}
        onChange={(event) => {
          if (event.currentTarget.value) change([...current, event.currentTarget.value]);
        }}
      >
        <option value="">{loading ? "Loading…" : "Add…"}</option>
        {options
          .filter((option) => !current.includes(option.value))
          .map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
      </select>
    </span>
  );
}

function sameList(left: string[], right: string[]) {
  return left.length === right.length && left.every((item, index) => item === right[index]);
}
