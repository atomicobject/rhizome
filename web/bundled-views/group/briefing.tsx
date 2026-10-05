// Briefing (SPEC-0111): what needs attention, what is in motion, what changed,
// which notes outside the group touch it, and what the group holds, in three
// columns that fold to two and one in narrower frames.
import { HighlightProvider, openCollection, type ViewModuleProps } from "@rhizome/kit";
import { useMemo } from "react";

import { boundary, inMotion, needsAttention } from "./activity.ts";
import { MotionRow, OutsideNotes, RecentChanges, StageHints } from "./briefing-activity.tsx";
import { NeedsAttention } from "./briefing-attention.tsx";
import { Connections, InThisGroup, ViewsAndGuide } from "./briefing-contents.tsx";
import { Block, heldLabels, lower, memberOf } from "./briefing-parts.tsx";
import { GroupFacts, GroupPage } from "./components.tsx";
import { linkGraph, type LinkGraph } from "./graph.ts";
import { useGroupModel } from "./load.ts";
import { memberLabel, type GroupModel } from "./model.ts";

/** In-motion records listed per member before the rest link to the collection. */
export const MOTION_RECORDS = 4;

function InMotion({ model }: { model: GroupModel }) {
  const motion = useMemo(() => inMotion(model), [model]);
  const total = motion.groups.reduce((sum, group) => sum + group.entries.length, 0);

  return (
    <Block
      title="In motion"
      count={total}
      caption="Records whose lifecycle value is in the active stage, newest change first."
    >
      {motion.groups.length === 0 && <p className="gv-quiet">Nothing in an active stage.</p>}
      {motion.groups.map((group) => {
        const member = memberOf(model, group.member);

        if (!member) return null;
        const records = group.entries.map((entry) => entry.record);
        const hidden = group.entries.length - MOTION_RECORDS;

        return (
          <div key={group.member} className="gv-mo-group">
            <h3 className="gv-mo-type">
              {memberLabel(model, member, { plural: true })} · {heldLabels(member, records)}{" "}
              <span className="gv-num">{group.entries.length}</span>
            </h3>
            <ul className="gv-list">
              {group.entries.slice(0, MOTION_RECORDS).map((entry) => (
                <MotionRow key={entry.record.key} member={member} entry={entry} />
              ))}
            </ul>
            {hidden > 0 && (
              <button
                type="button"
                className="gv-link gv-more gv-mo-more"
                onClick={() => openCollection(member.name)}
              >
                +{hidden} more in {lower(memberLabel(model, member, { plural: true }))}
              </button>
            )}
          </div>
        );
      })}
      <StageHints
        model={model}
        withoutActiveStage={motion.withoutActiveStage}
        withoutLifecycle={motion.withoutLifecycle}
      />
    </Block>
  );
}

function GroupOutside({ model }: { model: GroupModel }) {
  const outside = useMemo(() => boundary(model), [model]);

  return (
    <OutsideNotes
      title="Outside the group"
      outside={outside}
      model={model}
      caption={`Notes that link to or from ${model.name} records, most connected first.`}
      empty="No notes outside the group link to or from its records."
    />
  );
}

function GroupAttention({ model, graph }: { model: GroupModel; graph: LinkGraph }) {
  const signals = useMemo(() => needsAttention(model, graph, Date.now()), [model, graph]);

  return (
    <NeedsAttention
      model={model}
      signals={signals}
      caption="Validation issues and declared warning values."
    />
  );
}

function BriefingBody({ model }: { model: GroupModel }) {
  const graph = useMemo(() => linkGraph(model), [model]);

  return (
    <HighlightProvider>
      <GroupFacts model={model} />
      <div className="gv-brief">
        <div className="gv-brief-col">
          <GroupAttention model={model} graph={graph} />
          <InMotion model={model} />
        </div>
        <div className="gv-brief-col">
          <RecentChanges model={model} />
          <GroupOutside model={model} />
        </div>
        <div className="gv-brief-col gv-brief-side">
          <InThisGroup model={model} />
          {graph.linked && model.members.length > 1 && <Connections model={model} graph={graph} />}
          <ViewsAndGuide model={model} />
        </div>
      </div>
    </HighlightProvider>
  );
}

export default function Briefing(_props: ViewModuleProps) {
  const state = useGroupModel();

  return <GroupPage state={state}>{(model) => <BriefingBody model={model} />}</GroupPage>;
}
