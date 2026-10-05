import { useState } from "react";

import { listPublicViewFieldCandidates } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import type {
  OntologyEditOp,
  OntologyEditSessionResponse,
  TypeProfile,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";
import { enumLabel } from "../lib/labels";
import { useStagedQuery } from "../staging/stagedQuery";
import { stagedFieldValue } from "../staging/stagedState";
import {
  EMPTY_EDIT_CANDIDATES,
  candidateForInput,
  candidateInputValue,
  capabilityLabel,
  cellValues,
  editOpForCell,
  listEditForCell,
  rawFieldValue,
  stagedTargetForRow,
} from "./ConfiguredTableCellHelpers";
import { fieldCapability } from "./ConfiguredViewModel";

type Props = {
  rows: ViewTableRow[];
  profile?: TypeProfile;
  capabilities: ViewFieldCapability[];
  viewID: string;
  editSession?: OntologyEditSessionResponse | null;
  onStageOps: (ops: OntologyEditOp[]) => Promise<void> | undefined;
  onClear: () => void;
};

/**
 * Sets an enum field or adds a link value on every selected row. Each row
 * gets the same operation an inline cell edit would stage, and the whole
 * batch stages through the edit session in one call.
 */
export function ConfiguredViewBulkBar({
  rows,
  profile,
  capabilities,
  viewID,
  editSession,
  onStageOps,
  onClear,
}: Props) {
  const editable = bulkFields(profile, capabilities);
  const [error, setError] = useState<string | null>(null);

  // Like a board move, a failed batch says why instead of rejecting silently.
  const stage = async (ops: Array<OntologyEditOp | null>, action: string) => {
    const staged = ops.filter((op): op is OntologyEditOp => op !== null);

    if (!staged.length) return;
    setError(null);

    try {
      await Promise.resolve(onStageOps(staged));
    } catch (caught) {
      const reason = caught instanceof Error && caught.message ? `: ${caught.message}` : "";
      const count = `${staged.length} ${staged.length === 1 ? "row" : "rows"}`;
      setError(`Could not ${action} on ${count}${reason}`);
    }
  };

  return (
    <div
      className="configured-view__bulk-bar"
      role="toolbar"
      aria-label="Edit selected rows"
      onKeyDown={(event) => {
        // Escape clears the selection from the bar as it does from the rows.
        if (event.key !== "Escape") return;
        event.preventDefault();
        onClear();
      }}
    >
      <span className="configured-view__bulk-count">
        <b>{rows.length}</b> selected
      </span>
      {error && (
        <span className="configured-view__bulk-error" role="alert">
          {error}
        </span>
      )}
      {editable.enums.map((capability) => (
        <select
          key={capability.key}
          aria-label={`Set ${capabilityLabel(capability)}`}
          value=""
          onChange={(event) => {
            const value = event.currentTarget.value;

            if (value) {
              void stage(
                rows.map((row) => setOp(row, capability, value)),
                `set ${capabilityLabel(capability)}`,
              );
            }
          }}
        >
          <option value="">Set {capabilityLabel(capability)}…</option>
          {(capability.edit?.options ?? []).map((option) => (
            <option key={option} value={option}>
              {enumLabel(
                option,
                capability.enumValues?.find((value) => value.value === option)?.label,
              )}
            </option>
          ))}
        </select>
      ))}
      {editable.links.map((capability) => (
        <AddLink
          key={capability.key}
          capability={capability}
          viewID={viewID}
          editSession={editSession}
          onAdd={(value, candidates, action) =>
            void stage(
              rows.map((row) => addLinkOp(row, capability, value, candidates, editSession)),
              action,
            )
          }
        />
      ))}
      <span className="configured-view__bulk-hint">Changes stage for review</span>
      <button type="button" className="configured-view__tool-button" onClick={onClear}>
        Clear <kbd>Esc</kbd>
      </button>
    </div>
  );
}

function AddLink({
  capability,
  viewID,
  editSession,
  onAdd,
}: {
  capability: ViewFieldCapability;
  viewID: string;
  editSession?: OntologyEditSessionResponse | null;
  onAdd: (
    value: string,
    candidates: NonNullable<typeof capability.edit>["candidates"],
    action: string,
  ) => void;
}) {
  const embedded = capability.edit?.candidates ?? EMPTY_EDIT_CANDIDATES;

  const query = useStagedQuery({
    subject: queryKeys.views.fieldCandidates(viewID, capability.key),
    session: editSession ?? null,
    queryFn: ({ signal, editSession: session }) =>
      listPublicViewFieldCandidates(viewID, capability.key, {
        limit: 50,
        editSession: session,
        signal,
      }),
    enabled: embedded.length === 0,
  });

  const candidates = embedded.length ? embedded : (query.data?.candidates ?? EMPTY_EDIT_CANDIDATES);
  // A single link is replaced rather than added to.
  const verb = capability.edit?.list === false ? "Set" : "Add";
  const label = `${verb} ${capabilityLabel(capability)}`;

  return (
    <select
      aria-label={label}
      value=""
      disabled={query.isLoading}
      onChange={(event) => {
        const value = event.currentTarget.value;

        if (value) onAdd(value, candidates, `${verb.toLowerCase()} ${capabilityLabel(capability)}`);
      }}
    >
      <option value="">{query.isLoading ? "Loading…" : `${label}…`}</option>
      {candidates.map((candidate) => (
        <option key={candidate.value} value={candidate.value}>
          {candidateInputValue(candidates, candidate)}
        </option>
      ))}
    </select>
  );
}

function setOp(row: ViewTableRow, capability: ViewFieldCapability, value: string) {
  const edit = capability.edit;

  if (!edit) return null;
  const list = listEditForCell(rawFieldValue(row, capability.key), capability, edit);

  return editOpForCell(
    row,
    capability.key,
    edit.field,
    edit.operation,
    list ? [value] : value,
    list,
  );
}

/** Adds the value to a list link, or sets a single link; rows that hold it already stay out. */
function addLinkOp(
  row: ViewTableRow,
  capability: ViewFieldCapability,
  value: string,
  candidates: NonNullable<ViewFieldCapability["edit"]>["candidates"],
  editSession: OntologyEditSessionResponse | null | undefined,
) {
  const edit = capability.edit;

  if (!edit) return null;
  const raw = rawFieldValue(row, capability.key);
  const list = listEditForCell(raw, capability, edit);
  // Build on a value staged since the rows loaded: this op replaces that one.
  const staged = stagedFieldValue(editSession, stagedTargetForRow(row), edit.field);

  const current = cellValues(staged ? staged.value : raw).map(
    (item) => candidateForInput(candidates, item)?.value ?? item,
  );

  if (current.includes(value)) return null;

  return editOpForCell(
    row,
    capability.key,
    edit.field,
    edit.operation,
    list ? [...current, value] : value,
    list,
  );
}

/** Editable enum fields and link fields the profile names, else every editable one. */
function bulkFields(profile: TypeProfile | undefined, capabilities: ViewFieldCapability[]) {
  const names = profile
    ? [
        profile.lifecycleField,
        ...(profile.orderedFields ?? []),
        ...(profile.categoryFields ?? []),
        ...(profile.peopleFields ?? []),
        ...(profile.relationFields ?? []),
      ].flatMap((name) => fieldCapability(capabilities, name) ?? [])
    : capabilities;

  return {
    enums: names.filter(
      (capability) => capability.edit?.kind === "enum" && capability.edit.options?.length,
    ),
    links: names.filter((capability) => capability.edit?.kind === "node"),
  };
}
