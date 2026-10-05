// Small pieces Briefing's blocks share: running-text labels, the block frame,
// and the "+N more" toggle.
import { orderedEnumValues } from "@rhizome/kit";
import { useId, type ReactNode } from "react";

import type { MemberRecords } from "./activity.ts";
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

export const memberOf = (model: GroupModel, name: string) => model.memberIndex.get(name);

/** "3 work items", with the member's label inside the group. */
export function counted(model: GroupModel, name: string, count: number) {
  const member = memberOf(model, name);

  return `${count} ${member ? lower(memberLabel(model, member, { count })) : name}`;
}

export function countedByMember(model: GroupModel, groups: readonly MemberRecords[]) {
  return groups.map((group) => counted(model, group.member, group.records.length)).join(", ");
}

/** A block of the page: a heading, an optional count and caption, and its body. */
export function Block({
  title,
  count,
  caption,
  children,
}: {
  title: string;
  count?: ReactNode;
  caption?: ReactNode;
  children: ReactNode;
}) {
  const id = useId();

  return (
    <section className="gv-blk" aria-labelledby={id}>
      <h2 className="gv-blk-head">
        <span id={id}>{title}</span>
        {count !== undefined && <span className="gv-num">{count}</span>}
      </h2>
      {caption && <p className="gv-cap">{caption}</p>}
      {children}
    </section>
  );
}

/** "+N more" while the rest is hidden, "Show fewer" once revealed in place. */
export function MoreToggle({
  hidden,
  open,
  onToggle,
}: {
  hidden: number;
  open: boolean;
  onToggle: () => void;
}) {
  return (
    <button type="button" className="gv-link gv-more" aria-expanded={open} onClick={onToggle}>
      {open ? "Show fewer" : `+${hidden} more`}
    </button>
  );
}

/** Distinct labels of the lifecycle values the records hold, in lifecycle order. */
export function heldLabels(member: Member, records: readonly GroupRecord[]) {
  const held = new Set(records.map((record) => lifecycleValue(member, record)));

  return orderedEnumValues(member.lifecycle?.values ?? [])
    .flatMap((value) => (held.has(value.name) ? [value.label ?? value.name] : []))
    .join(", ");
}
