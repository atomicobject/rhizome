import { useMemo } from "react";

import type {
  OntologyEditOp,
  OntologyEditSessionResponse,
  TypeProfile,
  ViewCatalogEntry,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";
import { displayTitle, enumLabel } from "../lib/labels";
import { publicWorkspaceRef } from "../api/client";
import { ConfiguredTableCellContent, configuredTableRowKey } from "./ConfiguredTableCell";
import { relationDisplayValue, relationTargetRow } from "./ConfiguredTableCellReadOnly";
import { cellValues, fieldValue, rawFieldValue } from "./ConfiguredTableCellHelpers";
import { capabilityLabel, fieldCapability } from "./ConfiguredViewModel";
import { StatusMark } from "./StatusMark";
import { useTypeLabel } from "./typeLabels";
import {
  useOntologySummaryQuery,
  useOntologyTypeQuery,
  useViewExecutionQuery,
} from "./useNotesQueries";
import type { OpenMode } from "./useNoteTabs";

type ViewSourceSpec = ViewCatalogEntry["source"];

/** Reverse records listed per field before the rail says how many more there are. */
const REVERSE_LIMIT = 8;

type Props = {
  row: ViewTableRow;
  profile?: TypeProfile;
  capabilities: ViewFieldCapability[];
  source: ViewSourceSpec;
  viewID: string;
  vaultKey?: string | null;
  editSession?: OntologyEditSessionResponse | null;
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | undefined;
  onOpenRow: (row: ViewTableRow, mode?: OpenMode) => void;
};

/**
 * The selected row as a record in the workspace rail: its summary, its
 * lifecycle, ordered, category, people, and relation fields as the table's
 * own editors, and the records that point back at it.
 */
export function ConfiguredViewRecord({
  row,
  profile,
  capabilities,
  source,
  viewID,
  vaultKey,
  editSession,
  onStageOps,
  onOpenRow,
}: Props) {
  const typeLabel = useTypeLabel();
  const title = displayTitle(row.title) || row.path || row.ref.notePath;
  const summary = profile?.summaryField ? fieldValue(row, profile.summaryField) : "";

  const fields = useMemo(() => recordFields(profile, capabilities), [capabilities, profile]);

  return (
    <article className="view-record" aria-label={`Selected record ${title}`}>
      <header className="view-record__head">
        <span className="type-chip">{typeLabel(row.resolvedType || row.ref.typeName)}</span>
        <span className="view-record__path" title={row.path || row.ref.notePath}>
          {(row.path || row.ref.notePath).replace(/^.*\//, "")}
        </span>
        <button
          type="button"
          className="configured-view__tool-button"
          aria-label={`Open ${title}`}
          title="Open in a tab (Enter)"
          onClick={() => onOpenRow(row)}
        >
          Open ↵
        </button>
      </header>
      <h3 className="view-record__title">{title}</h3>
      {summary && <p className="view-record__summary">{summary}</p>}
      {fields.length > 0 && (
        <dl className="view-record__fields">
          {fields.map((capability) => (
            <div key={capability.key}>
              <dt>{capabilityLabel(capability)}</dt>
              <dd>
                <ConfiguredTableCellContent
                  viewID={viewID}
                  vaultKey={vaultKey}
                  row={row}
                  field={capability.key}
                  columnIndex={1}
                  capability={capability}
                  editSession={editSession}
                  onStageOps={onStageOps}
                  onOpenRelation={onOpenRow}
                />
              </dd>
            </div>
          ))}
        </dl>
      )}
      {(profile?.reverseFields ?? []).map((field) => (
        <ReverseRecords
          key={field}
          field={field}
          row={row}
          source={source}
          profile={profile}
          capability={fieldCapability(capabilities, field)}
          editSession={editSession}
          onOpenRow={onOpenRow}
        />
      ))}
    </article>
  );
}

/**
 * Records that link to this one, with their lifecycle marks. A reverse field
 * reads them through the target type's generated view, filtered by its
 * forward link. An inbound neighbor field has no forward link to filter by,
 * so it reads them from this row's own card or board execution, which lists
 * each row's linking records. The generated view offers cards with a summary
 * field and a board with a lifecycle; without either the rail shows the count.
 */
function ReverseRecords({
  field,
  row,
  source,
  profile,
  capability,
  editSession,
  onOpenRow,
}: {
  field: string;
  row: ViewTableRow;
  source: ViewSourceSpec;
  profile?: TypeProfile;
  capability: ViewFieldCapability | undefined;
  editSession?: OntologyEditSessionResponse | null;
  onOpenRow: (row: ViewTableRow, mode?: OpenMode) => void;
}) {
  const typeLabel = useTypeLabel();
  const session = editSession ?? null;
  const sourceName = source.type || source.interface;
  const typeQuery = useOntologyTypeQuery(sourceName, session);
  const summary = useOntologySummaryQuery().data;
  const doc = typeQuery.data?.type?.fields?.find((item) => item.name === field);
  const target = doc?.typeName;
  const forward = doc?.reverseField;
  const targetIsInterface = summary?.interfaces?.some((item) => item.name === target) ?? false;
  const path = row.path || row.ref.notePath;

  const viewID =
    target && forward
      ? `generated.${targetIsInterface ? "interface" : "type"}.${target}.table`
      : null;

  const ownVariant = profile?.summaryField ? "card" : profile?.lifecycleField ? "kanban" : "";

  const ownViewID =
    doc && !forward && sourceName && ownVariant
      ? `generated.${source.type ? "type" : "interface"}.${sourceName}.table`
      : null;

  const state = useMemo(
    () => ({
      filters: [{ field: forward ?? "", op: "eq", value: path }],
      page: { offset: 0, first: REVERSE_LIMIT },
    }),
    [forward, path],
  );

  const ownState = useMemo(
    () => ({ variant: ownVariant, filters: [{ field: "path", op: "eq", value: path }] }),
    [ownVariant, path],
  );

  const reverseQuery = useViewExecutionQuery(viewID, state, session, Boolean(viewID));
  const ownQuery = useViewExecutionQuery(ownViewID, ownState, session, Boolean(ownViewID));
  const query = viewID ? reverseQuery : ownQuery;
  const result = reverseQuery.displayData;
  const lifecycle = fieldCapability(result?.capabilities ?? [], result?.profile?.lifecycleField);

  const ownRow = ownQuery.displayData?.rows.find(
    (item) => configuredTableRowKey(item) === configuredTableRowKey(row),
  );

  const shown: LinkedRecord[] = viewID
    ? (result?.rows ?? []).map((item) => {
        const value = lifecycle ? cellValues(rawFieldValue(item, lifecycle.key))[0] : "";
        const option = lifecycle?.enumValues?.find((candidate) => candidate.value === value);

        return {
          key: configuredTableRowKey(item),
          title: displayTitle(item.title) || item.path || item.ref.notePath,
          open: item,
          status: value ? { value, label: option?.label, tone: option?.tone } : undefined,
        };
      })
    : (ownRow?.relationValues?.[field] ?? []).map((relation) => {
        const linked = relation.ref ? publicWorkspaceRef(relation.ref) : relation.value;

        return {
          key: linked,
          title: displayTitle(relation.title) || relationDisplayValue(relation.value),
          open: relationTargetRow(linked, relation.title, relation.ref),
          status: relation.status,
        };
      });

  const label = target ? typeLabel(target, true) : capability ? capabilityLabel(capability) : field;
  const counted = Number(fieldValue(ownRow ?? row, capability?.key ?? field) || 0);
  const total = result?.pageInfo.total ?? Math.max(counted, shown.length);

  return (
    <section className="view-record__reverse" aria-label={label}>
      <h4>
        {label} <span className="view-record__count">{total}</span>
      </h4>
      {shown.length > 0 ? (
        <ul>
          {shown.map((item) => {
            const stage = item.status ? enumLabel(item.status.value, item.status.label) : "";

            return (
              <li key={item.key}>
                {item.status ? (
                  <StatusMark tone={item.status.tone} label="" />
                ) : (
                  <span className="view-record__no-mark" aria-hidden="true" />
                )}
                <button
                  type="button"
                  className="view-record__link"
                  title={stage ? `${item.title} · ${stage}` : item.title}
                  onClick={() => onOpenRow(item.open)}
                >
                  {item.title}
                </button>
                {stage && <span className="view-record__stage">{stage}</span>}
              </li>
            );
          })}
          {total > shown.length && (
            <li className="view-record__more">{total - shown.length} more</li>
          )}
        </ul>
      ) : typeQuery.isLoading || query.isLoading ? (
        <p className="view-record__empty">Loading…</p>
      ) : total === 0 && !query.isError ? (
        <p className="view-record__empty">None yet.</p>
      ) : null}
    </section>
  );
}

type LinkedRecord = {
  key: string;
  title: string;
  open: ViewTableRow;
  status?: { value: string; label?: string; tone?: string };
};

/** The record's editable fields in profile order, or its KEY fields without a profile. */
function recordFields(profile: TypeProfile | undefined, capabilities: ViewFieldCapability[]) {
  const names = profile
    ? [
        profile.lifecycleField,
        ...(profile.orderedFields ?? []),
        ...(profile.categoryFields ?? []),
        ...(profile.keyTextFields ?? []),
        ...(profile.peopleFields ?? []),
        ...(profile.relationFields ?? []),
        profile.primaryDateField,
      ]
    : capabilities.flatMap((capability) =>
        capability.importance === "KEY" && capability.edit ? [capability.key] : [],
      );

  const seen = new Set<string>();

  return names.flatMap((name) => {
    const capability = fieldCapability(capabilities, name);

    if (!capability || seen.has(capability.key)) return [];
    seen.add(capability.key);

    return [capability];
  });
}
