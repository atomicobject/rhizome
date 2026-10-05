// Sections (SPEC-0111): each member of the group as a compact table of its most
// advanced, then newest, records. Hierarchical members render as tree tables.
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
import { useGroupModel } from "./load.ts";
import {
  fieldLabel,
  fieldValue,
  firstSentence,
  isEmptyValue,
  linkTargets,
  memberLabel,
  valueText,
  type GroupModel,
  type GroupRecord,
  type Member,
  type MemberField,
} from "./model.ts";

/** Rows shown per member before "Show all". */
export const SECTION_ROWS = 8;

function columnLabel(member: Member, column: SectionColumn) {
  if (column.kind === "type") return "Type";

  return column.kind === "lifecycle" ? fieldLabel(column.field) : column.label;
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
}: {
  model: GroupModel;
  member: Member;
  node: TreeNode;
  columns: readonly SectionColumn[];
}) {
  const { record, depth, parent } = node;

  return (
    <tr data-record-key={record.key} data-depth={depth || undefined}>
      <td>
        <div className="gv-title" style={{ paddingLeft: depth * 16 }}>
          {depth > 0 && <span className="gv-branch" aria-hidden="true" />}
          <RecordMark member={member} record={record} />
          <RecordTitle record={record} />
          {parent && <span className="sr-only">, under {parent.title}</span>}
          <IssueCount record={record} />
        </div>
        {record.summary && (
          <div className="gv-summary" style={{ marginLeft: depth * 16 + 18 }}>
            {record.summary}
          </div>
        )}
      </td>
      {columns.map((column) => (
        <td key={column.kind === "field" ? column.id : column.kind}>
          <Cell model={model} member={member} record={record} column={column} />
        </td>
      ))}
      <td className="gv-num gv-right">
        {record.updatedAt === null ? <Dash /> : <RelativeTime value={record.updatedAt} />}
      </td>
    </tr>
  );
}

function Section({ model, member }: { model: GroupModel; member: Member }) {
  const rows = useMemo(() => sectionRows(member), [member]);
  const shown = rows.slice(0, SECTION_ROWS);

  const columns = sectionColumns(
    member,
    shown.map((node) => node.record),
  );

  const label = memberLabel(model, member, { plural: true });
  const total = Math.max(member.count, rows.length);

  return (
    <section className="gv-section" aria-label={label}>
      <header className="gv-section-head">
        <h2>
          <MemberHeading model={model} member={member} />
        </h2>
        <span className="gv-num">{member.count}</span>
        {member.issueCount > 0 && (
          <button type="button" className="gv-issues" onClick={() => openMemberIssues(member)}>
            {member.issueCount} {member.issueCount === 1 ? "issue" : "issues"}
          </button>
        )}
        {member.description && <span className="gv-desc">{firstSentence(member.description)}</span>}
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
                <th scope="col" key={column.kind === "field" ? column.id : column.kind}>
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
              />
            ))}
          </tbody>
        </table>
      )}
      {total > shown.length && (
        <p className="gv-foot">
          <button type="button" className="gv-link" onClick={() => openCollection(member.name)}>
            Show all {total}
          </button>
          <span className="gv-faint">
            {" "}
            · {member.lifecycle ? "most advanced first, then newest" : "newest first"}
          </span>
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
      <GroupFacts model={model}>
        {graph.linked
          ? "Ordered like Trace columns."
          : members.length > 1
            ? "Ordered by size."
            : null}
      </GroupFacts>
      {members.map((member) => (
        <Section key={member.name} model={model} member={member} />
      ))}
    </>
  );
}

export default function Sections(_props: ViewModuleProps) {
  const state = useGroupModel();

  return <GroupPage state={state}>{(model) => <SectionsBody model={model} />}</GroupPage>;
}
