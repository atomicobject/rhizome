// Sections (SPEC-0111): each member of the group as a compact table of its most
// advanced, then newest, records. Hierarchical members render as tree tables.
import { Info } from "lucide-react";
import { RelativeTime, openCollection, typeLabel, type ViewModuleProps } from "@rhizome/kit";
import { useMemo } from "react";

import {
  Dash,
  GroupFacts,
  GroupPage,
  IssueCount,
  LifecycleCounts,
  LifecycleLabel,
  LinkedTitles,
  MemberHeading,
  RecordMark,
  RecordTitle,
  openMemberIssues,
} from "./components.tsx";
import {
  linkGraph,
  sectionColumns,
  sectionOrder,
  sectionRows,
  type SectionColumn,
  type TreeNode,
} from "./graph.ts";
import { MoreToggle, lower } from "./briefing-parts.tsx";
import { useGroupModel } from "./load.ts";
import { useRememberedOpen } from "./remembered.ts";
import {
  fieldLabel,
  fieldValue,
  firstSentence,
  isEmptyValue,
  lifecycleValue,
  linkTargets,
  memberLabel,
  valueText,
  type GroupModel,
  type GroupRecord,
  type Member,
  type MemberField,
} from "./model.ts";

import "./trace-sections.css";

/** True when one concrete type holds more than 90% of the member's records. */
function oneTypeDominates(member: Member) {
  const counts = new Map<string, number>();

  for (const record of member.records) counts.set(record.type, (counts.get(record.type) ?? 0) + 1);

  return Math.max(0, ...counts.values()) > member.records.length * 0.9;
}

/** Rows shown per member before "Show N more". */
export const SECTION_ROWS = 8;

function columnLabel(member: Member, column: SectionColumn) {
  if (column.kind === "type") return "Type";

  // Every lifecycle reads as "Status", whatever its field is called ("Spec status").
  return column.kind === "lifecycle" ? "Status" : column.label;
}

function FieldCell({ record, field }: { record: GroupRecord; field: MemberField }) {
  if (field.kind === "link") {
    return (
      <LinkedTitles
        notes={linkTargets(record, { field: field.name, declaredBy: field.declaredBy })}
      />
    );
  }

  const value = fieldValue(record, field);

  if (isEmptyValue(value)) return <Dash />;

  const label = field.values.find((entry) => entry.name === value)?.label;

  return <span className="gv-clamp">{label ?? valueText(value)}</span>;
}

function Cell({
  model,
  member,
  record,
  column,
}: {
  model: GroupModel;
  member: Member;
  record: GroupRecord;
  column: SectionColumn;
}) {
  if (column.kind === "type") {
    const labels = model.typeLabels.get(record.type);

    return <span>{labels ? typeLabel(labels) : fieldLabel(record.type)}</span>;
  }

  if (column.kind === "lifecycle") return <LifecycleLabel member={member} record={record} />;

  return <FieldCell record={record} field={column.field} />;
}

function Row({
  model,
  member,
  node,
  columns,
  marked,
}: {
  model: GroupModel;
  member: Member;
  node: TreeNode;
  columns: readonly SectionColumn[];
  /** False when the section's rows share one status, which the heading already counts. */
  marked: boolean;
}) {
  const { record, depth, parent } = node;
  const lifecycleShown = !marked || columns.some((column) => column.kind === "lifecycle");

  return (
    <tr data-record-key={record.key} data-depth={depth || undefined}>
      <td>
        <div className="gv-title" style={{ paddingLeft: depth * 16 }}>
          {depth > 0 && <span className="gv-branch" aria-hidden="true" />}
          {/* The Status column already names the lifecycle value. */}
          {!lifecycleShown && <RecordMark member={member} record={record} />}
          <RecordTitle record={record} />
          {parent && <span className="sr-only">, under {parent.title}</span>}
          <IssueCount record={record} />
        </div>
        {record.summary && (
          <div
            className="gv-summary"
            style={{ marginLeft: depth * 16 + (lifecycleShown ? 0 : 16) }}
          >
            {record.summary}
          </div>
        )}
      </td>
      {columns.map((column) => (
        <td
          key={column.kind === "field" ? column.id : column.kind}
          className={
            column.kind === "lifecycle"
              ? "gv-col-status"
              : column.kind === "type"
                ? "gv-col-type"
                : undefined
          }
        >
          <Cell model={model} member={member} record={record} column={column} />
        </td>
      ))}
      <td className="gv-age gv-right">
        {record.updatedAt === null ? <Dash /> : <RelativeTime value={record.updatedAt} />}
      </td>
    </tr>
  );
}

function Section({ model, member }: { model: GroupModel; member: Member }) {
  const rows = useMemo(() => sectionRows(member), [member]);
  const expanded = useRememberedOpen("sections.expanded");
  const open = expanded.isOpen(member.name, false);
  const shown = open ? rows : rows.slice(0, SECTION_ROWS);

  // A section whose records all share one status omits it: the heading's
  // counts already say it. Expanding a mixed section restores the column.
  const oneStatus =
    Boolean(member.lifecycle) &&
    new Set(shown.map((node) => lifecycleValue(member, node.record))).size <= 1;

  // An interface's Type column says nothing when one type holds nearly every record.
  const columns = sectionColumns(
    member,
    shown.map((node) => node.record),
  ).filter(
    (column) =>
      (column.kind !== "type" || !oneTypeDominates(member)) &&
      (column.kind !== "lifecycle" || !oneStatus),
  );

  const label = memberLabel(model, member, { plural: true });
  const total = Math.max(member.count, rows.length);

  return (
    <section className="gv-section" id={sectionId(member)} aria-label={label}>
      <header className="gv-section-head">
        <h2>
          <MemberHeading model={model} member={member} />
        </h2>
        <span className="gv-num">{member.count}</span>
        {member.description && <span className="gv-desc">{firstSentence(member.description)}</span>}
        <span
          className="rz-info"
          tabIndex={0}
          title={member.lifecycle ? "Most advanced first, then newest" : "Newest first"}
          aria-label={member.lifecycle ? "Most advanced first, then newest" : "Newest first"}
        >
          <Info size={14} aria-hidden="true" />
        </span>
        {member.issueCount > 0 && (
          <button type="button" className="gv-issues" onClick={() => openMemberIssues(member)}>
            {member.issueCount} {member.issueCount === 1 ? "issue" : "issues"}
          </button>
        )}
        <LifecycleCounts member={member} />
      </header>
      {rows.length === 0 ? (
        <p className="gv-none">None recorded yet.</p>
      ) : (
        <table className="gv-table">
          <thead>
            <tr>
              <th scope="col">Title</th>
              {columns.map((column) => (
                <th
                  scope="col"
                  key={column.kind === "field" ? column.id : column.kind}
                  className={
                    column.kind === "lifecycle"
                      ? "gv-col-status"
                      : column.kind === "type"
                        ? "gv-col-type"
                        : undefined
                  }
                >
                  {columnLabel(member, column)}
                </th>
              ))}
              <th scope="col" className="gv-right gv-changed">
                Changed
              </th>
            </tr>
          </thead>
          <tbody>
            {shown.map((node) => (
              <Row
                key={node.record.key}
                model={model}
                member={member}
                node={node}
                columns={columns}
                marked={!oneStatus}
              />
            ))}
          </tbody>
        </table>
      )}
      {rows.length > SECTION_ROWS && (
        <p className="gv-foot">
          <MoreToggle
            hidden={rows.length - SECTION_ROWS}
            open={open}
            onToggle={() => expanded.setOpen(member.name, !open)}
          />
        </p>
      )}
      {open && total > rows.length && (
        <p className="gv-foot">
          <button
            type="button"
            className="gv-link gv-more"
            onClick={() => openCollection(member.name)}
          >
            Open {lower(label)}
          </button>
        </p>
      )}
    </section>
  );
}

function SectionsBody({ model }: { model: GroupModel }) {
  const graph = useMemo(() => linkGraph(model), [model]);
  const members = useMemo(() => sectionOrder(model, graph), [model, graph]);

  return (
    <>
      <GroupFacts model={model} />
      {members.length > 1 && (
        <nav className="gv-jump" aria-label="Jump to section">
          <span className="gv-jump-label">Jump to</span>
          {members.map((member) => (
            <button
              key={member.name}
              type="button"
              className="gv-link"
              disabled={member.count === 0}
              data-empty={member.count === 0 || undefined}
              aria-label={`${memberLabel(model, member, { plural: true })} ${member.count}`}
              title={`${memberLabel(model, member, { plural: true })} ${member.count}`}
              onClick={() =>
                document
                  .getElementById(sectionId(member))
                  ?.scrollIntoView({ behavior: "smooth", block: "start" })
              }
            >
              {memberLabel(model, member, { plural: true })}{" "}
              <span className="num">{member.count}</span>
            </button>
          ))}
        </nav>
      )}
      {members.map((member) => (
        <Section key={member.name} model={model} member={member} />
      ))}
    </>
  );
}

const sectionId = (member: Member) => `section-${member.name}`;

export default function Sections(_props: ViewModuleProps) {
  const state = useGroupModel();

  return <GroupPage state={state}>{(model) => <SectionsBody model={model} />}</GroupPage>;
}
