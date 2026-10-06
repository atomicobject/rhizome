// The group Overview's "Guide and views" block, the guide note the most
// members share and the authored views the group's switcher offers, and the
// type Briefing's implementing types of an interface.
import { openNote, openView, typeLabel } from "@rhizome/kit";

import { Block, plural } from "./briefing-parts.tsx";
import { fieldLabel, type GroupModel, type Member } from "./model.ts";

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

export function GuideAndViews({ model }: { model: GroupModel }) {
  const { guide, views } = model;

  return (
    <Block title="Guide and views">
      {!guide && !views.length && <p className="gv-quiet">No guide note or authored views.</p>}
      {guide && (
        <>
          <p className="gv-res">
            <button type="button" className="gv-link" onClick={() => openNote(guide.path)}>
              {guide.title}
            </button>
            <span className="gv-sub">
              guide shared by {guide.sharedBy} of {model.members.length}{" "}
              {plural(model.members.length, "type", "types")}
            </span>
          </p>
          {guide.summary && <p className="gv-typ-desc">{guide.summary}</p>}
        </>
      )}
      {views.length > 0 && (
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
      )}
    </Block>
  );
}
