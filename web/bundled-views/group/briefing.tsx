// Briefing (SPEC-0111, SPEC-0117 US6): what needs attention, what is in
// motion, and what changed in a display group, in two columns that fold to
// one in narrower frames. The group's structure is on Overview. The frame and
// every block's heading paint at once. The facts strip and each block read
// only the record fields they show, each in its own request, so each block
// fills in when its read arrives and fails on its own with a retry.
import { HighlightProvider, openCollection, type ViewModuleProps } from "@rhizome/kit";
import { useMemo } from "react";

import { inMotion, needsAttention } from "./activity.ts";
import { MotionRow, RecentChanges, StageHints } from "./briefing-activity.tsx";
import { NeedsAttention } from "./briefing-attention.tsx";
import { Block, ModelBlock, heldLabels, lower, memberOf } from "./briefing-parts.tsx";
import { GroupFrame } from "./components.tsx";
import { linkGraph } from "./graph.ts";
import { useGroupModel, type GroupRead } from "./load.ts";
import { memberLabel, type GroupModel } from "./model.ts";

/** In-motion records listed per member before the rest link to the collection. */
export const MOTION_RECORDS = 4;

/**
 * Records of each type Recent changes reads, newest first.
 * ponytail: a burst past this many records in one minute shows this many; raise it if that matters.
 */
export const RECENT_RECORDS = 100;

/** The facts strip: each record's change time and issue count, and the guide. */
const FACTS_READ: GroupRead = { neighbors: "NONE", links: false, scalars: false };

/** Values, gap fields, and the links among records; no neighbors. */
const ATTENTION_READ: GroupRead = { neighbors: "NONE", guide: false };

/** Lifecycle values, key text, and summaries; no links. */
const MOTION_READ: GroupRead = { neighbors: "NONE", links: false, guide: false };

/** The newest records of each type, with the lifecycle values their marks show. */
const RECENT_READ: GroupRead = {
  neighbors: "NONE",
  links: false,
  guide: false,
  perType: RECENT_RECORDS,
};

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

function AttentionBlock() {
  const state = useGroupModel({ views: false, read: ATTENTION_READ });

  return (
    <ModelBlock state={state} title="Needs attention" what="what needs attention">
      {(model) => <GroupAttention model={model} />}
    </ModelBlock>
  );
}

function MotionBlock() {
  const state = useGroupModel({ views: false, read: MOTION_READ });

  return (
    <ModelBlock state={state} title="In motion" what="work in motion">
      {(model) => <InMotion model={model} />}
    </ModelBlock>
  );
}

function RecentBlock() {
  const state = useGroupModel({ views: false, read: RECENT_READ });

  return (
    <ModelBlock state={state} title="Recent changes" what="recent changes">
      {(model) => <RecentChanges model={model} />}
    </ModelBlock>
  );
}

export default function Briefing(_props: ViewModuleProps) {
  const state = useGroupModel({ views: false, read: FACTS_READ });

  return (
    <GroupFrame state={state}>
      <HighlightProvider>
        <div className="gv-brief gv-brief-two">
          <div className="gv-brief-col">
            <AttentionBlock />
            <MotionBlock />
          </div>
          <div className="gv-brief-col">
            <RecentBlock />
          </div>
        </div>
      </HighlightProvider>
    </GroupFrame>
  );
}
