// Briefing (SPEC-0111, SPEC-0117 US6): what needs attention, what is in
// motion, and what changed in a display group, in two columns that fold to
// one in narrower frames. The group's structure is on Overview. The frame and
// every block's heading paint at once; each block fills in when the records
// it reads arrive and fails on its own with a retry.
import { HighlightProvider, openCollection, type ViewModuleProps } from "@rhizome/kit";
import { useMemo } from "react";

import { inMotion, needsAttention } from "./activity.ts";
import { MotionRow, RecentChanges, StageHints } from "./briefing-activity.tsx";
import { NeedsAttention } from "./briefing-attention.tsx";
import { Block, ModelBlock, heldLabels, lower, memberOf } from "./briefing-parts.tsx";
import { GroupFrame } from "./components.tsx";
import { linkGraph } from "./graph.ts";
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

function GroupAttention({ model }: { model: GroupModel }) {
  const signals = useMemo(() => needsAttention(model, linkGraph(model), Date.now()), [model]);

  return (
    <NeedsAttention
      model={model}
      signals={signals}
      caption="Validation issues and declared warning values."
    />
  );
}

export default function Briefing(_props: ViewModuleProps) {
  // Every block reads the group's records, so they share one cached read.
  const state = useGroupModel({ views: false });

  return (
    <GroupFrame state={state}>
      <HighlightProvider>
        <div className="gv-brief gv-brief-two">
          <div className="gv-brief-col">
            <ModelBlock state={state} title="Needs attention" what="what needs attention">
              {(model) => <GroupAttention model={model} />}
            </ModelBlock>
            <ModelBlock state={state} title="In motion" what="work in motion">
              {(model) => <InMotion model={model} />}
            </ModelBlock>
          </div>
          <div className="gv-brief-col">
            <ModelBlock state={state} title="Recent changes" what="recent changes">
              {(model) => <RecentChanges model={model} />}
            </ModelBlock>
          </div>
        </div>
      </HighlightProvider>
    </GroupFrame>
  );
}
