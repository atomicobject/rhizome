// Small pieces the Briefing and Overview blocks share: running-text labels,
// the block frame and its loading body, and the "+N more" toggle.
import { orderedEnumValues } from "@rhizome/kit";
import { useId, type ReactNode } from "react";

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

/**
 * A block of the page: a heading, an optional count and caption, controls
 * beside the heading, and its body. `className` reserves the body's space.
 */
export function Block({
  title,
  count,
  caption,
  controls,
  className,
  children,
}: {
  title: string;
  count?: ReactNode;
  caption?: ReactNode;
  controls?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  const id = useId();

  return (
    <section className={className ? `gv-blk ${className}` : "gv-blk"} aria-labelledby={id}>
      <h2 className="gv-blk-head">
        <span id={id}>{title}</span>
        {count !== undefined && <span className="gv-num">{count}</span>}
        {controls && <span className="gv-blk-controls">{controls}</span>}
      </h2>
      {caption && <p className="gv-cap">{caption}</p>}
      {children}
    </section>
  );
}

/**
 * A block's body once its reads are ready; until then a spinner in its place,
 * and on failure what failed with a retry. `what` names the data, as in
 * "Loading the map…".
 */
export function BlockBody({
  status,
  what,
  children,
}: {
  status: BlockStatus;
  what: string;
  children: () => ReactNode;
}) {
  if (status.status === "loading")
    return (
      <p className="gv-spin" role="status">
        <i aria-hidden="true" />
        Loading {what}…
      </p>
    );

  if (status.status === "error")
    return (
      <p className="gv-fail" role="alert">
        Could not load {what}: {status.error.message}{" "}
        <button type="button" className="gv-link" onClick={status.retry}>
          Retry
        </button>
      </p>
    );

  return children();
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
