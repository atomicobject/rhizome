// Briefing's side column: the group's members, the connections matrix, and
// the group's views and guide.
import { openNote, openView, orderedEnumValues, typeLabel } from "@rhizome/kit";
import { useMemo } from "react";

import { Block, memberOf, plural } from "./briefing-parts.tsx";
import { LifecycleCounts, MemberHeading, openMemberIssues } from "./components.tsx";
import { connections, type LinkGraph } from "./graph.ts";
import {
  fieldLabel,
  firstSentence,
  lifecycleValue,
  memberLabel,
  type GroupModel,
  type Member,
} from "./model.ts";

/** A member's lifecycle distribution as a bar; LifecycleCounts carries the labels. */
function DistributionBar({ member }: { member: Member }) {
  if (!member.lifecycle) return null;
  const { values } = member.lifecycle;

  const segments = orderedEnumValues(values).flatMap((value) => {
    const count = member.records.filter(
      (record) => lifecycleValue(member, record) === value.name,
    ).length;

    return count ? [{ value, count }] : [];
  });

  if (!segments.length) return null;

  return (
    <div className="gv-dist" aria-hidden="true">
      {segments.map(({ value, count }) => (
        <i key={value.name} data-tone={value.tone ?? "neutral"} style={{ flexGrow: count }} />
      ))}
    </div>
  );
}

/** Records of an interface member by implementing type, most first. */
export function Implementors({ model, member }: { model: GroupModel; member: Member }) {
  if (member.kind !== "interface") return null;
  const counts = new Map<string, number>();

  for (const record of member.records) counts.set(record.type, (counts.get(record.type) ?? 0) + 1);

  const entries = [...counts].sort((a, b) => b[1] - a[1]);

  if (!entries.length) return null;

  return (
    <p className="gv-kinds">
      {entries.map(([type, count]) => {
        const labels = model.typeLabels.get(type);

        return (
          <span key={type}>
            {labels ? typeLabel(labels, { prefix: model.labelPrefix }) : fieldLabel(type)}{" "}
            <span className="gv-num">{count}</span>
          </span>
        );
      })}
    </p>
  );
}

export function InThisGroup({ model }: { model: GroupModel }) {
  return (
    <Block title="In this group">
      <ul className="gv-list">
        {model.members.map((member) => (
          <li key={member.name} className="gv-typ">
            <div className="gv-typ-top">
              <MemberHeading model={model} member={member} />
              {member.kind === "interface" && <span className="gv-sub">interface</span>}
              {member.issueCount > 0 && (
                <button
                  type="button"
                  className="gv-issues"
                  onClick={() => openMemberIssues(member)}
                >
                  {member.issueCount} {plural(member.issueCount, "issue", "issues")}
                </button>
              )}
              <span className="gv-num gv-typ-count">{member.count}</span>
            </div>
            {member.description && (
              <p className="gv-typ-desc">{firstSentence(member.description)}</p>
            )}
            <DistributionBar member={member} />
            <LifecycleCounts member={member} />
            <Implementors model={model} member={member} />
          </li>
        ))}
      </ul>
    </Block>
  );
}

export function Connections({ model, graph }: { model: GroupModel; graph: LinkGraph }) {
  const rows = useMemo(() => connections(model, graph), [model, graph]);

  const label = (name: string) => {
    const member = memberOf(model, name);

    return member ? memberLabel(model, member, { plural: true }) : name;
  };

  return (
    <Block
      title="Connections"
      caption="Record links from row to column. Hatched: the schema allows the link, but no record uses it."
    >
      <table className="gv-mx">
        <thead>
          <tr>
            <td />
            {model.members.map((member) => (
              <th key={member.name} scope="col">
                <span>{label(member.name)}</span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => (
            <tr key={model.members[index]?.name}>
              <th scope="row">{label(model.members[index]?.name ?? "")}</th>
              {row.map((cell) => {
                if (cell.from === cell.to)
                  return (
                    <td key={cell.to} data-cell="self">
                      <span className="sr-only">Same type</span>
                    </td>
                  );

                if (!cell.allowed)
                  return (
                    <td key={cell.to}>
                      <span className="sr-only">No link field</span>
                    </td>
                  );

                return (
                  <td key={cell.to} data-cell={cell.count ? "used" : "unused"}>
                    {cell.count}
                    {cell.count === 0 && <span className="sr-only"> (allowed, unused)</span>}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </Block>
  );
}

export function ViewsAndGuide({ model }: { model: GroupModel }) {
  const { guide, views } = model;

  return (
    <>
      {views.length > 0 && (
        <Block title="Views">
          <ul className="gv-list">
            {views.map((view) => (
              <li key={view.id} className="gv-res">
                <button
                  type="button"
                  className="gv-link"
                  onClick={() => openView(view.id, { kind: "group", group: model.name })}
                >
                  {view.name}
                </button>
                {view.description && <span className="gv-sub">{view.description}</span>}
              </li>
            ))}
          </ul>
        </Block>
      )}
      {guide && (
        <Block title="Guide">
          <p className="gv-res">
            <button type="button" className="gv-link" onClick={() => openNote(guide.path)}>
              {guide.title}
            </button>
            <span className="gv-sub">
              shared by {guide.sharedBy} of {model.members.length}{" "}
              {plural(model.members.length, "type", "types")}
            </span>
          </p>
          {guide.summary && <p className="gv-typ-desc">{guide.summary}</p>}
        </Block>
      )}
    </>
  );
}
