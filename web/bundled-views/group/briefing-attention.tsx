// The Briefings' Needs attention block: one row per signal, each with a count,
// its records, and an action.
import { RelativeTime, openCollection } from "@rhizome/kit";
import { Fragment, createContext, useContext, type ReactNode } from "react";

import { STALE_DAYS, type AttentionSignal, type MemberRecords } from "./activity.ts";
import { openMemberIssues, openRecord } from "./components.tsx";
import { useRememberedOpen } from "./remembered.ts";
import {
  Block,
  MoreToggle,
  countedByMember,
  heldLabels,
  lower,
  memberOf,
  plural,
} from "./briefing-parts.tsx";
import {
  fieldLabel,
  memberLabel,
  type GroupModel,
  type GroupRecord,
  type Member,
} from "./model.ts";

/** Records named under an attention signal before "+N more". */
export const SIGNAL_RECORDS = 3;

/** What a signal offers for its records' member: by default, opening its collection. */
export type MemberAction = { label: (member: Member) => string; run: (member: Member) => void };

const OPEN_COLLECTION: MemberAction = {
  label: (member) => `Open ${lower(member.pluralLabel)}`,
  run: (member) => openCollection(member.name),
};

const MemberActionContext = createContext<MemberAction>(OPEN_COLLECTION);

/**
 * The records behind a signal, grouped by member when several members hold
 * them. A member's label runs `onMember`; titles open the records.
 */
function SignalRecords({
  id,
  model,
  groups,
  onMember,
  withTime = false,
}: {
  /** The signal's stable key, under which its expansion is remembered. */
  id: string;
  model: GroupModel;
  groups: readonly MemberRecords[];
  onMember: (member: Member) => void;
  withTime?: boolean;
}) {
  const expanded = useRememberedOpen("briefing.expanded");
  const open = expanded.isOpen(`signal:${id}`, false);
  const setOpen = (next: boolean) => expanded.setOpen(`signal:${id}`, next);
  const total = groups.reduce((sum, group) => sum + group.records.length, 0);
  let budget = open ? total : SIGNAL_RECORDS;

  const shown = groups.flatMap((group) => {
    const records = group.records.slice(0, Math.max(0, budget));
    budget -= records.length;

    return records.length ? [{ member: group.member, records }] : [];
  });

  const hidden = total - Math.min(total, SIGNAL_RECORDS);

  return (
    <span className="gv-sig-records">
      {shown.map((group, index) => {
        const member = memberOf(model, group.member);

        return (
          <span key={group.member}>
            {index > 0 && <span className="gv-sep"> · </span>}
            {groups.length > 1 && member && (
              <>
                <button type="button" className="gv-link" onClick={() => onMember(member)}>
                  {memberLabel(model, member, { plural: true })}
                </button>
                :{" "}
              </>
            )}
            {group.records.map((record, at) => (
              <Fragment key={record.key}>
                {at > 0 && ", "}
                <button type="button" className="gv-inline" onClick={() => openRecord(record)}>
                  {record.title}
                </button>
                {withTime && record.updatedAt !== null && (
                  <>
                    {" "}
                    <RelativeTime value={record.updatedAt} className="gv-num" />
                  </>
                )}
              </Fragment>
            ))}
          </span>
        );
      })}
      {hidden > 0 && (
        <>
          {" "}
          <MoreToggle hidden={hidden} open={open} onToggle={() => setOpen(!open)} />
        </>
      )}
    </span>
  );
}

type SignalAction = { label: string; run: () => void };

function SignalRow({
  count,
  severity,
  text,
  action,
  children,
}: {
  count: ReactNode;
  severity: "risk" | "warning" | "ok" | "context";
  text: ReactNode;
  action?: SignalAction | null;
  children?: ReactNode;
}) {
  return (
    <li className="gv-sig" data-severity={severity}>
      <span className="gv-sig-count">{count}</span>
      <span className="gv-sig-text">{text}</span>
      {action ? (
        <button type="button" className="gv-link gv-sig-act" onClick={action.run}>
          {action.label}
        </button>
      ) : (
        <span />
      )}
      {children && <span className="gv-sig-detail">{children}</span>}
    </li>
  );
}

/** The member action, offered when every record behind a signal belongs to one member. */
function useMemberAction(model: GroupModel, groups: readonly MemberRecords[]) {
  const action = useContext(MemberActionContext);
  const member = groups.length === 1 ? memberOf(model, groups[0].member) : undefined;

  if (!member) return null;

  return action === OPEN_COLLECTION
    ? {
        label: `Open ${lower(memberLabel(model, member, { plural: true }))}`,
        run: () => openCollection(member.name),
      }
    : { label: action.label(member), run: () => action.run(member) };
}

function groupByMember(model: GroupModel, records: readonly GroupRecord[]): MemberRecords[] {
  return model.members.flatMap((member) => {
    const own = records.filter((record) => record.member === member.name);

    return own.length ? [{ member: member.name, records: own }] : [];
  });
}

// Presence in a profile makes a field useful to describe, not an obligation.
function isAttention(signal: AttentionSignal) {
  return signal.kind === "issues" || signal.kind === "tone";
}

function Signal({ model, signal }: { model: GroupModel; signal: AttentionSignal }) {
  const openMember = useContext(MemberActionContext).run;
  const groups = signalGroups(model, signal);
  const action = useMemberAction(model, groups);

  switch (signal.kind) {
    case "issues": {
      const only = groups.length === 1 ? memberOf(model, groups[0].member) : undefined;

      return (
        <SignalRow
          count={signal.records.length}
          severity="risk"
          text={
            <>
              <b>Validation issues</b> on {signal.records.length}{" "}
              {plural(signal.records.length, "record", "records")}
            </>
          }
          action={only ? { label: "Open issues", run: () => openMemberIssues(only) } : null}
        >
          <SignalRecords
            id={signalKey(signal)}
            model={model}
            groups={groups}
            onMember={openMemberIssues}
          />
        </SignalRow>
      );
    }

    case "tone":
      return (
        <SignalRow
          count={signal.records.length}
          severity={signal.tone === "warning" ? "warning" : "risk"}
          text={
            <>
              <b>{signal.value.label ?? signal.value.name}</b>{" "}
              <span className="gv-sub">{lower(fieldLabel(signal.field))}</span>:{" "}
              {countedByMember(model, signal.byMember)}
            </>
          }
          action={action}
        >
          <SignalRecords
            id={signalKey(signal)}
            model={model}
            groups={signal.byMember}
            onMember={openMember}
          />
        </SignalRow>
      );

    case "stale": {
      const member = memberOf(model, signal.member);

      return (
        <SignalRow
          count={signal.records.length}
          severity={isAttention(signal) ? "warning" : "context"}
          text={
            <>
              {member ? memberLabel(model, member, { count: signal.records.length }) : ""}{" "}
              <b>{member ? lower(heldLabels(member, signal.records)) : ""}</b> but unchanged for{" "}
              {STALE_DAYS}+ days
            </>
          }
          action={action}
        >
          <SignalRecords
            id={signalKey(signal)}
            model={model}
            groups={groups}
            onMember={openMember}
            withTime
          />
        </SignalRow>
      );
    }

    case "gap": {
      const empty = signal.records.length;
      const member = memberOf(model, signal.member);
      const label = member ? lower(memberLabel(model, member, { count: signal.total })) : "";

      const scope =
        empty === signal.total
          ? signal.total === 1
            ? `the only ${label}`
            : `all ${signal.total} ${label}`
          : `${empty} of ${signal.total} ${label}`;

      return (
        <SignalRow
          count={`${empty}/${signal.total}`}
          severity={isAttention(signal) ? "warning" : "context"}
          text={
            <>
              <b>{fieldLabel(signal.field)}</b> empty on {scope}
            </>
          }
          action={action}
        >
          {signal.policyReason && <span className="gv-why">{signal.policyReason} </span>}
          <SignalRecords
            id={signalKey(signal)}
            model={model}
            groups={groups}
            onMember={openMember}
          />
        </SignalRow>
      );
    }

    case "reverse": {
      const member = memberOf(model, signal.member);
      const missing = signal.records.length;
      const linking = model.typeLabels.get(signal.targetType);

      return (
        <SignalRow
          count={`${missing}/${signal.total}`}
          severity={isAttention(signal) ? "warning" : "context"}
          text={
            <>
              No <b>{lower(linking?.pluralLabel ?? fieldLabel(signal.field))}</b> link to {missing}{" "}
              of {signal.total}{" "}
              {member ? lower(memberLabel(model, member, { count: signal.total })) : ""}
            </>
          }
          action={action}
        >
          <SignalRecords
            id={signalKey(signal)}
            model={model}
            groups={groups}
            onMember={openMember}
          />
        </SignalRow>
      );
    }

    case "unlinked":
      return (
        <SignalRow
          count={signal.records.length}
          severity={isAttention(signal) ? "warning" : "context"}
          text={
            <>
              <b>Not linked</b> to anything else in {model.name}:{" "}
              {countedByMember(model, signal.byMember)}
            </>
          }
          action={action}
        >
          <SignalRecords
            id={signalKey(signal)}
            model={model}
            groups={signal.byMember}
            onMember={openMember}
          />
        </SignalRow>
      );
  }
}

/** The records behind a signal, by member. */
function signalGroups(model: GroupModel, signal: AttentionSignal): readonly MemberRecords[] {
  switch (signal.kind) {
    case "tone":
    case "unlinked":
      return signal.byMember;
    case "stale":
    case "gap":
    case "reverse":
      return [{ member: signal.member, records: signal.records }];
    default:
      return groupByMember(model, signal.records);
  }
}

function signalKey(signal: AttentionSignal) {
  switch (signal.kind) {
    case "tone":
      return `tone:${signal.field}:${signal.value.name}`;
    case "stale":
      return `stale:${signal.member}`;
    case "gap":
    case "reverse":
      return `${signal.kind}:${signal.member}:${signal.field}`;
    default:
      return signal.kind;
  }
}

/**
 * The block for `signals`, worded by `caption`. A signal whose records all
 * belong to one member offers `memberAction`, which by default opens that
 * member's collection.
 */
export function NeedsAttention({
  model,
  signals,
  caption,
  memberAction = OPEN_COLLECTION,
}: {
  model: GroupModel;
  signals: readonly AttentionSignal[];
  caption: string;
  memberAction?: MemberAction;
}) {
  const attention = signals.filter(isAttention);
  const context = signals.filter((signal) => !isAttention(signal));
  const clean = !signals.some((signal) => signal.kind === "issues");

  return (
    <MemberActionContext.Provider value={memberAction}>
      <Block title="Needs attention" count={attention.length} caption={caption}>
        <ul className="gv-list">
          {attention.map((signal) => (
            <Signal key={signalKey(signal)} model={model} signal={signal} />
          ))}
          {clean && (
            <SignalRow
              count={<span aria-hidden="true">✓</span>}
              severity="ok"
              text="No validation issues"
            />
          )}
        </ul>
      </Block>
      {context.length > 0 && (
        <Block
          title="Context"
          count={context.length}
          caption="Optional fields, connections, and update age. These observations do not imply required work."
        >
          <ul className="gv-list">
            {context.map((signal) => (
              <Signal key={signalKey(signal)} model={model} signal={signal} />
            ))}
          </ul>
        </Block>
      )}
    </MemberActionContext.Provider>
  );
}
