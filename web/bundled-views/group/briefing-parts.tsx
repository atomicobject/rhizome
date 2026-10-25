// Small pieces the Briefing and Overview blocks share: running-text labels,
// the block frame and its loading body, and the "+N more" toggle.
import {
  MoreToggle,
  orderedEnumValues,
  Section as Block,
  SectionBody as BlockBody,
} from "@rhizome/kit";
import type { ReactNode } from "react";

import type { MemberRecords } from "./activity.ts";
import type { GroupModelState } from "./load.ts";
import type { BlockStatus } from "./scope-load.ts";
import {
  lifecycleValue,
  memberLabel,
  type GroupModel,
  type GroupRecord,
  type Member,
} from "./model.ts";

/** A label lowercased for running text, unless it starts with an acronym. */
export const lower = (label: string) =>
  /^[A-Z][a-z]/.test(label) ? label.charAt(0).toLowerCase() + label.slice(1) : label;

export const plural = (count: number, one: string, many: string) => (count === 1 ? one : many);

/** What a scope block says instead of counts while the index rebuilds. */
export const Rebuilding = () => (
  <p className="gv-quiet" role="status">
    The index is rebuilding; counts appear when it finishes.
  </p>
);

export const memberOf = (model: GroupModel, name: string) => model.memberIndex.get(name);

/** "3 work items", with the member's label inside the group. */
export function counted(model: GroupModel, name: string, count: number) {
  const member = memberOf(model, name);

  return `${count} ${member ? lower(memberLabel(model, member, { count })) : name}`;
}

export function countedByMember(model: GroupModel, groups: readonly MemberRecords[]) {
  return groups.map((group) => counted(model, group.member, group.records.length)).join(", ");
}

// Blocks, their loading and failure bodies, and "+N more" are the kit's page
// primitives; the names stay for the views written against them.
export { Block, BlockBody, MoreToggle };

/** Distinct labels of the lifecycle values the records hold, in lifecycle order. */
export function heldLabels(member: Member, records: readonly GroupRecord[]) {
  const held = new Set(records.map((record) => lifecycleValue(member, record)));

  return orderedEnumValues(member.lifecycle?.values ?? [])
    .flatMap((value) => (held.has(value.name) ? [value.label ?? value.name] : []))
    .join(", ");
}

function modelStatus(state: GroupModelState): BlockStatus {
  if (state.status === "ready") return { status: "ready" };

  if (state.status === "error") return { status: "error", error: state.error, retry: state.retry };

  if (state.status === "missing")
    return { status: "error", error: new Error("No such display group"), retry: () => {} };

  return { status: "loading" };
}

/** A block over the group model: its heading at once, its body once the records load. */
export function ModelBlock({
  state,
  title,
  what,
  children,
}: {
  state: GroupModelState;
  title: string;
  what: string;
  children: (model: GroupModel) => ReactNode;
}) {
  if (state.status === "ready") return children(state.model);

  return (
    <Block title={title}>
      <BlockBody status={modelStatus(state)} what={what}>
        {() => null}
      </BlockBody>
    </Block>
  );
}
