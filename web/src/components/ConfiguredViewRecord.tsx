import { TypeChip } from "./TypeChip";
import { Copy } from "lucide-react";
import { useLayoutEffect, useMemo, useRef, useState } from "react";

import type {
  OntologyEditOp,
  OntologyEditSessionResponse,
  TypeProfile,
  ViewCatalogEntry,
  ViewFieldCapability,
  ViewTableColumn,
  ViewTableRow,
} from "../api/types";
import { displayTitle, enumLabel } from "../lib/labels";
import { getNodeWorkspace, publicWorkspaceRef } from "../api/client";
import { ConfiguredTableCellContent, configuredTableRowKey } from "./ConfiguredTableCell";
import {
  readableDate,
  relationDisplayValue,
  relationTargetRow,
} from "./ConfiguredTableCellReadOnly";
import {
  cellValues,
  fieldValue,
  recordDisplayTitle,
  rawFieldValue,
} from "./ConfiguredTableCellHelpers";
import {
  capabilityFor,
  capabilityLabel,
  columnLabel,
  fieldCapability,
} from "./ConfiguredViewModel";
import { DisclosureChevron } from "./DisclosureChevron";
import { StatusMark } from "./StatusMark";
import { useTypeLabel } from "./typeLabels";
import {
  useOntologySummaryQuery,
  useOntologyTypeQuery,
  useViewExecutionQuery,
} from "./useNotesQueries";
import { queryKeys } from "../api/queryKeys";
import { useStagedQuery } from "../staging/stagedQuery";
import { NotesSidebarInfo } from "./NotesSidebarInfo";
import type { OpenMode } from "./useNoteTabs";

type ViewSourceSpec = ViewCatalogEntry["source"];

/** Reverse records listed per field before the rail says how many more there are. */
const REVERSE_LIMIT = 8;

type Props = {
  row: ViewTableRow;
  profile?: TypeProfile;
  capabilities: ViewFieldCapability[];
  /** Table columns too narrow to show; the record lists them after its own fields. */
  movedColumns?: ViewTableColumn[];
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
  movedColumns = [],
  source,
  viewID,
  vaultKey,
  editSession,
  onStageOps,
  onOpenRow,
}: Props) {
  const typeLabel = useTypeLabel();
  const title = recordDisplayTitle(row, displayTitle(row.title)) || row.path || row.ref.notePath;
  const summary = profile?.summaryField ? fieldValue(row, profile.summaryField) : "";
  const path = row.path || row.ref.notePath;
  const identifier = recordIdentifier(row, capabilities);

  const fields = useMemo(() => recordFields(profile, capabilities), [capabilities, profile]);

  const moved = movedColumns.filter(
    (column) =>
      !fields.some((field) => field.key === capabilityFor(capabilities, column.field)?.key),
  );

  return (
    <article className="view-record" aria-label={`Selected record ${title}`}>
      <header className="view-record__head">
        <TypeChip name={row.resolvedType || row.ref.typeName || ""}>
          {typeLabel(row.resolvedType || row.ref.typeName)}
        </TypeChip>
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
      {/* The stable ID tells records apart; the file path, rarely needed, sits
          behind a copy control that names it on hover. */}
      <div className="view-record__id">
        {identifier && <code>{identifier}</code>}
        <CopyPath path={path} />
      </div>
      {summary && <RecordSummary key={configuredTableRowKey(row)} text={summary} />}
      {fields.length + moved.length > 0 && (
        <dl className="view-record__fields">
          {fields.map((capability) => (
            <div key={capability.key}>
              <dt>{capabilityLabel(capability)}</dt>
              <dd
                title={!fieldValue(row, capability.key) ? "—" : undefined}
                aria-label={!fieldValue(row, capability.key) ? "—" : undefined}
              >
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
          {moved.map((column) => (
            <div key={column.field}>
              <dt>{columnLabel(column, capabilities)}</dt>
              <dd
                title={!fieldValue(row, column.field) ? "—" : undefined}
                aria-label={!fieldValue(row, column.field) ? "—" : undefined}
              >
                {column.field === "updatedAt" &&
                /^\d{4}-\d{2}-\d{2}/.test(fieldValue(row, column.field)) ? (
                  <time dateTime={fieldValue(row, column.field)}>
                    {readableDate(fieldValue(row, column.field))}
                  </time>
                ) : (
                  <ConfiguredTableCellContent
                    viewID={viewID}
                    vaultKey={vaultKey}
                    row={row}
                    field={column.field}
                    columnIndex={1}
                    capability={capabilityFor(capabilities, column.field)}
                    editSession={editSession}
                    onStageOps={onStageOps}
                    onOpenRelation={onOpenRow}
                  />
                )}
              </dd>
            </div>
          ))}
        </dl>
      )}
      {path && <RecordNoteContext path={path} editSession={editSession} onOpenRow={onOpenRow} />}
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

function RecordNoteContext({
  path,
  editSession,
  onOpenRow,
}: Pick<Props, "editSession" | "onOpenRow"> & { path: string }) {
  const query = useStagedQuery({
    subject: [...queryKeys.all, "nodes", "workspace", path],
    session: editSession ?? null,
    queryFn: ({ signal, editSession }) => getNodeWorkspace(path, { signal, editSession }),
  });

  if (query.isError) return <p role="status">Could not load note context.</p>;

  if (!query.displayData) return <p role="status">Loading note context…</p>;

  return (
    <div className="view-record__context">
      <NotesSidebarInfo
        workspace={query.displayData}
        onOpen={(target, mode) =>
          onOpenRow(relationTargetRow(target), mode === "beside" ? "beside" : "activate")
        }
      />
    </div>
  );
}

/** The record's stable identifier, such as an effort or spec ID, when its type declares one. */
function recordIdentifier(row: ViewTableRow, capabilities: ViewFieldCapability[]) {
  const field = capabilities.find(
    (capability) =>
      capability.semanticRole === "identifier" && capability.sourceKind !== "frontmatter",
  );

  return field ? fieldValue(row, field.key) : "";
}

/** Copies the record's file path; the path itself shows on hover and to assistive technology. */
function CopyPath({ path }: { path: string }) {
  const [copied, setCopied] = useState(false);

  return (
    <button
      type="button"
      className="view-record__copy"
      title={copied ? "Copied" : path}
      aria-label={`Copy path ${path}`}
      onClick={() => {
        void navigator.clipboard?.writeText(path).then(
          () => setCopied(true),
          () => {},
        );
      }}
    >
      <Copy size={13} aria-hidden />
      {copied ? "Copied" : "Copy path"}
    </button>
  );
}

/** The summary clamped to six lines, with a disclosure to read the rest in place. */
function RecordSummary({ text }: { text: string }) {
  const ref = useRef<HTMLParagraphElement>(null);
  const [open, setOpen] = useState(false);
  const [clamped, setClamped] = useState(false);

  useLayoutEffect(() => {
    const element = ref.current;

    if (!element || open) return;

    const measure = () => setClamped(element.scrollHeight > element.clientHeight + 1);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);

    return () => observer.disconnect();
  }, [open, text]);

  return (
    <div className="view-record__summary-wrap">
      <p ref={ref} className={`view-record__summary${open ? "" : " is-clamped"}`}>
        {text}
      </p>
      {(clamped || open) && (
        <button
          type="button"
          className="view-record__more-toggle list-more"
          aria-expanded={open}
          onClick={() => setOpen(!open)}
        >
          <DisclosureChevron open={open} />
          {open ? "Show less summary" : "Show full summary"}
        </button>
      )}
    </div>
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
                  className="view-record__link link-secondary"
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
