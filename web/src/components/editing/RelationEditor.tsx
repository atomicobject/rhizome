import { useEffect, useId, useRef, useState } from "react";

import type { NodeFieldLink, OntologyEditFieldValue } from "../../api/types";
import { FieldLinkValues, fieldLinksForValues } from "../FieldLinkValues";
import { useActiveEditSession } from "../useOntologyEditSession";
import { useOntologyTypeQuery } from "../useNotesQueries";
import {
  clearPersistedEditorDraft,
  readPersistedEditorDraft,
  type OntologyEditExpected,
  writePersistedEditorDraft,
} from "./draftStorage";

export function RelationEditor({
  values,
  links = [],
  targetType,
  list,
  editing,
  dirty,
  required = false,
  vaultKey = null,
  operationTarget = "",
  ariaLabel,
  onDraft,
  onStage,
}: {
  values: string[];
  /** Resolved targets for the committed values, used for read-mode titles. */
  links?: NodeFieldLink[];
  targetType?: string;
  list: boolean;
  editing: boolean;
  dirty: boolean;
  required?: boolean;
  vaultKey?: string | null;
  operationTarget?: string;
  ariaLabel: string;
  onDraft?: (value: OntologyEditFieldValue) => OntologyEditExpected | undefined;
  onStage: (value: OntologyEditFieldValue) => boolean | void;
}) {
  const errorID = useId();
  const requiredErrorID = `${errorID}-required`;
  const loadErrorID = `${errorID}-load`;

  const [draftValues, setDraftValues] = useState(() =>
    relationDraftValues(readPersistedEditorDraft(vaultKey, operationTarget), values, list),
  );

  const lastStagedValues = useRef<string | null>(null);
  const [requiredError, setRequiredError] = useState("");
  const candidateQuery = useOntologyTypeQuery(targetType, useActiveEditSession(), editing);
  const loadError = candidateQuery.isError;

  // Candidates stage as wikilinks, the form the vault authors and the table
  // editor writes; a bare path would land in frontmatter as plain text.
  const candidates = (candidateQuery.data?.notes || []).map((note) => {
    const target = `${(note.ref?.notePath || note.path).replace(/\.md$/i, "")}${
      note.ref?.fragment ? `#${note.ref.fragment}` : ""
    }`;

    return { value: `[[${target}]]`, target, label: note.title || note.path };
  });

  // An authored link names a note by file name or path, not by candidate value.
  const candidateFor = (value: string) => {
    const target = linkTarget(value);

    const exact = candidates.find(
      (candidate) => candidate.value === value || candidate.target === target,
    );

    if (exact) return exact;

    // A file-name link resolves only when one candidate has that file name.
    const byFileName = candidates.filter(
      (candidate) => candidate.target.replace(/^.*\//, "") === target,
    );

    return byFileName.length === 1 ? byFileName[0] : undefined;
  };

  const labelFor = (value: string) =>
    candidateFor(value)?.label ||
    links.find((link) => link.value === value)?.title ||
    linkTarget(value);

  useEffect(() => {
    const persisted = readPersistedEditorDraft(vaultKey, operationTarget);
    const serialized = JSON.stringify(values);

    if (persisted !== null) {
      setDraftValues(relationDraftValues(persisted, values, list));
    } else if (lastStagedValues.current === null || lastStagedValues.current === serialized) {
      setDraftValues(values);
      lastStagedValues.current = null;
    }
  }, [list, operationTarget, values, vaultKey]);
  useEffect(() => {
    const discard = () => {
      clearPersistedEditorDraft(vaultKey, operationTarget);
      setDraftValues(values);
      setRequiredError("");
      lastStagedValues.current = null;
    };

    window.addEventListener("rhizome:discard-editor-drafts", discard);

    return () => window.removeEventListener("rhizome:discard-editor-drafts", discard);
  }, [operationTarget, values, vaultKey]);

  const stage = (next: OntologyEditFieldValue) => {
    const nextValues = relationValuesFromEdit(next);

    if (required && isEmptyRelationValue(next)) {
      setDraftValues(nextValues);
      writePersistedEditorDraft(
        vaultKey,
        operationTarget,
        encodeRelationDraft(nextValues, list),
        onDraft?.(next),
      );
      setRequiredError(`${ariaLabel} is required.`);

      return false;
    }

    setRequiredError("");
    lastStagedValues.current = JSON.stringify(nextValues);
    setDraftValues(nextValues);
    const expected = onDraft?.(next);

    if (JSON.stringify(nextValues) === JSON.stringify(values)) {
      clearPersistedEditorDraft(vaultKey, operationTarget);
    } else {
      writePersistedEditorDraft(
        vaultKey,
        operationTarget,
        encodeRelationDraft(nextValues, list),
        expected,
      );
    }

    if (onStage(next) !== false) {
      clearPersistedEditorDraft(vaultKey, operationTarget);
    }

    return true;
  };

  if (!editing) {
    return (
      <span className={dirty ? "field-editor is-dirty" : "field-editor"}>
        {draftValues.length > 0 ? (
          <FieldLinkValues links={fieldLinksForValues(draftValues, links)} />
        ) : (
          displayValues(draftValues)
        )}
      </span>
    );
  }

  if (list) {
    const remaining = candidates.filter(
      (candidate) => !draftValues.some((value) => candidateFor(value) === candidate),
    );

    const itemKeyCounts = new Map<string, number>();

    return (
      <span className="field-editor__relation field-editor__relation--list">
        <span className="field-editor__relation-items">
          {draftValues.length === 0 ? <span className="field-editor__empty">Empty</span> : null}
          {draftValues.map((value, index) => {
            const occurrence = itemKeyCounts.get(value) || 0;
            itemKeyCounts.set(value, occurrence + 1);

            return (
              <span key={`${value}:${occurrence}`} className="field-editor__relation-item">
                <span>{labelFor(value)}</span>
                <button
                  type="button"
                  aria-label={`Move ${labelFor(value)} up`}
                  disabled={index === 0}
                  onClick={() => {
                    const next = [...draftValues];
                    [next[index - 1], next[index]] = [next[index], next[index - 1]];
                    stage({ kind: "list", items: next });
                  }}
                >
                  ↑
                </button>
                <button
                  type="button"
                  aria-label={`Move ${labelFor(value)} down`}
                  disabled={index === draftValues.length - 1}
                  onClick={() => {
                    const next = [...draftValues];
                    [next[index], next[index + 1]] = [next[index + 1], next[index]];
                    stage({ kind: "list", items: next });
                  }}
                >
                  ↓
                </button>
                <button
                  type="button"
                  aria-label={`Remove ${labelFor(value)}`}
                  onClick={() =>
                    stage({ kind: "list", items: draftValues.filter((_, item) => item !== index) })
                  }
                >
                  ×
                </button>
              </span>
            );
          })}
        </span>
        <select
          className="field-editor__select"
          aria-label={`Add ${ariaLabel}`}
          aria-invalid={Boolean(requiredError)}
          aria-describedby={
            [loadError ? loadErrorID : "", requiredError ? requiredErrorID : ""]
              .filter(Boolean)
              .join(" ") || undefined
          }
          defaultValue=""
          onChange={(event) => {
            if (!event.currentTarget.value) return;
            stage({ kind: "list", items: [...draftValues, event.currentTarget.value] });
            event.currentTarget.value = "";
          }}
        >
          <option value="">Add relation…</option>
          {remaining.map((candidate) => (
            <option key={candidate.value} value={candidate.value}>
              {candidate.label}
            </option>
          ))}
        </select>
        {loadError ? (
          <small id={loadErrorID} className="field-editor__reason" role="alert">
            Relation choices unavailable. Your current values are preserved.
          </small>
        ) : null}
        {requiredError ? (
          <small id={requiredErrorID} className="field-editor__reason" role="alert">
            {requiredError}
          </small>
        ) : null}
      </span>
    );
  }

  const current = draftValues[0] || "";
  const currentCandidate = current ? candidateFor(current) : undefined;

  return (
    <span className="field-editor__relation">
      <select
        value={currentCandidate?.value ?? current}
        className={dirty ? "field-editor__select is-dirty" : "field-editor__select"}
        aria-label={ariaLabel}
        aria-invalid={Boolean(requiredError)}
        aria-describedby={
          [loadError ? loadErrorID : "", requiredError ? requiredErrorID : ""]
            .filter(Boolean)
            .join(" ") || undefined
        }
        onChange={(event) => {
          const next = event.currentTarget.value ? [event.currentTarget.value] : [];
          stage(fieldValueFromScalar(next[0] || ""));
        }}
      >
        <option value="">Empty</option>
        {current && !currentCandidate ? <option value={current}>{labelFor(current)}</option> : null}
        {candidates.map((candidate) => (
          <option key={candidate.value} value={candidate.value}>
            {candidate.label}
          </option>
        ))}
      </select>
      {loadError ? (
        <small id={loadErrorID} className="field-editor__reason" role="alert">
          Relation choices unavailable. Your current value is preserved.
        </small>
      ) : null}
      {requiredError ? (
        <small id={requiredErrorID} className="field-editor__reason" role="alert">
          {requiredError}
        </small>
      ) : null}
    </span>
  );
}

/** The note a link names: `[[Notes/Ann|Ann]]` and `Notes/Ann.md` both give `Notes/Ann`. */
function linkTarget(value: string) {
  const link = value.trim().match(/^\[\[([^|\]]+)(?:\|[^\]]*)?\]\]$/);

  return (link ? link[1] : value).trim().replace(/\.md$/i, "");
}

export function displayValues(values: string[]) {
  return values.length > 0 ? values.join(", ") : <span className="field-editor__empty">Empty</span>;
}

function relationDraftValues(
  persisted: string | null,
  fallback: string[],
  list: boolean,
): string[] {
  if (persisted === null) return fallback;

  if (!list) return persisted ? [persisted] : [];

  try {
    const parsed: unknown = JSON.parse(persisted);

    if (Array.isArray(parsed) && parsed.every((item) => item === String(item))) {
      return parsed;
    }
  } catch {
    // Keep the authored values when a list draft is not valid JSON.
  }

  return fallback;
}

function relationValuesFromEdit(value: OntologyEditFieldValue): string[] {
  if (value.kind === "list") return value.items;

  return value.kind === "scalar" && value.scalar ? [value.scalar] : [];
}

function encodeRelationDraft(values: string[], list: boolean): string {
  return list ? JSON.stringify(values) : values[0] || "";
}

function isEmptyRelationValue(value: OntologyEditFieldValue): boolean {
  if (value.kind === "unset") return true;

  if (value.kind === "scalar") return value.scalar.trim() === "";

  return value.items.length === 0 || value.items.every((item) => item.trim() === "");
}

export function fieldValueFromScalar(value: string): OntologyEditFieldValue {
  return value === "" ? { kind: "unset" } : { kind: "scalar", scalar: value };
}
