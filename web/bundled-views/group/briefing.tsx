// Briefing (SPEC-0111, SPEC-0117 US6): what needs attention, what is in
// motion, and what changed in a display group, in two columns that fold to
// one in narrower frames. The group's structure is on Overview. The frame and
// every block's heading paint at once. The facts strip and each block read
// only the record fields they show, each in its own request, so each block
// fills in when its read arrives and fails on its own with a retry.
import { HighlightProvider, type ViewModuleProps } from "@rhizome/kit";
import { useMemo } from "react";

import { MotionCount } from "./briefing-motion.tsx";
import { inMotion, needsAttention } from "./activity.ts";
import { useMotionMetadata, MotionList, RecentChanges, StageHints } from "./briefing-activity.tsx";
import { NeedsAttention } from "./briefing-attention.tsx";
import { Block, ModelBlock } from "./briefing-parts.tsx";
import { GroupFrame } from "./components.tsx";
import { linkGraph } from "./graph.ts";
import { useGroupModel, type GroupRead } from "./load.ts";
import { type GroupModel } from "./model.ts";

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

  const entries = motion.groups
    .flatMap((group) => group.entries)
    .sort((a, b) => (b.record.updatedAt ?? 0) - (a.record.updatedAt ?? 0));

  const metadata = useMotionMetadata(model, entries);

  const title =
    motion.groups.length === 1 &&
    model.memberIndex.get(motion.groups[0].member)?.pluralLabel.toLowerCase() === "efforts"
      ? "Active efforts"
      : "In motion";

  return (
    <Block
      title={title}
      className="gv-motion"
      count={<MotionCount count={entries.length} metadata={metadata} />}
      info={{
        text: "Records whose lifecycle value is in the active stage, newest change first.",
        display: "title",
      }}
      controls={<span className="gv-motion-age-label">Last changed</span>}
    >
      {motion.groups.length === 0 && <p className="gv-quiet">Nothing in an active stage.</p>}
      <MotionList model={model} entries={entries} />
      <StageHints
        model={model}
        withoutActiveStage={motion.withoutActiveStage}
        withoutLifecycle={motion.withoutLifecycle}
      />
    </Block>
  );
}

function GroupAttention({
  model,
  part,
}: {
  model: GroupModel;
  part: "attention" | "review" | "coverage";
}) {
  const signals = useMemo(() => needsAttention(model, linkGraph(model), Date.now()), [model]);

  return (
    <NeedsAttention
      model={model}
      signals={signals}
      info="Validation issues and declared warning values."
      part={part}
    />
  );
}

/** Needs attention leads, then the optional review signals, then work in motion. */
function AttentionBlock({ part }: { part: "attention" | "review" | "coverage" }) {
  const state = useGroupModel({ views: false, read: ATTENTION_READ });

  if (part !== "attention" && state.status !== "ready") return null;

  return (
    <ModelBlock state={state} title="Needs attention" what="what needs attention">
      {(model) => <GroupAttention model={model} part={part} />}
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
            <AttentionBlock part="attention" />
            <AttentionBlock part="review" />
            <MotionBlock />
            <AttentionBlock part="coverage" />
          </div>
          <div className="gv-brief-col">
            <RecentBlock />
          </div>
        </div>
      </HighlightProvider>
    </GroupFrame>
  );
}
