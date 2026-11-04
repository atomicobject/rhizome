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
 * The records behind a signal as rows, title left and age right, grouped
 * under their member's label when several members hold them. A member's label
 * runs `onMember`; titles open the records. "Show N more" sits on its own line.
 */
function SignalRecords({
  id,
  model,
  groups,
  onMember,
  unlinked = false,
}: {
  /** The signal's stable key, under which its expansion is remembered. */
  id: string;
  unlinked?: boolean;
  model: GroupModel;
  groups: readonly MemberRecords[];
  onMember: (member: Member) => void;
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
    <>
      <ul className="gv-sig-rows">
        {shown.map((group) => {
          const member = memberOf(model, group.member);

          return (
            <Fragment key={group.member}>
              {groups.length > 1 && member && (
                <li className="gv-sig-member">
                  <button type="button" className="gv-link" onClick={() => onMember(member)}>
                    {memberLabel(model, member, { plural: true })}
                  </button>
                </li>
              )}
              {group.records.map((record) => (
                <li key={record.key} className="gv-sig-row">
                  <button type="button" className="gv-inline" onClick={() => openRecord(record)}>
                    {record.title}
                  </button>
                  {unlinked && (
                    <span className="gv-sub">
                      {model.typeLabels.get(record.type)?.label ?? member?.label}
                    </span>
                  )}
                  {record.updatedAt !== null && (
                    <RelativeTime value={record.updatedAt} className="gv-age" />
                  )}
                </li>
              ))}
            </Fragment>
          );
        })}
      </ul>
      {hidden > 0 && (
        <div className="gv-sig-more">
          {unlinked ? (
            <button
              type="button"
              className="rz-text-button rz-more"
              aria-expanded={open}
              onClick={() => setOpen(!open)}
            >
              {open ? "Show fewer" : `Show ${hidden} more unlinked notes`}
            </button>
          ) : (
            <MoreToggle hidden={hidden} open={open} onToggle={() => setOpen(!open)} />
          )}
        </div>
      )}
    </>
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
      {children && <div className="gv-sig-detail">{children}</div>}
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
          {signal.policyReason && <p className="gv-why">{signal.policyReason}</p>}
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
            unlinked
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
 * The block for `signals`, explained by `info`. A signal whose records all
 * belong to one member offers `memberAction`, which by default opens that
 * member's collection. `part` renders only Needs attention or only Review
 * suggested, so a Briefing can put work in motion between them.
 */
export function NeedsAttention({
  model,
  signals,
  info,
  memberAction = OPEN_COLLECTION,
  part = "both",
}: {
  model: GroupModel;
  signals: readonly AttentionSignal[];
  info: string;
  memberAction?: MemberAction;
  part?: "attention" | "review" | "both";
}) {
  const attention = signals.filter(isAttention);
  // Unlinked records are a coverage fact, not a prompt to act, so they get
  // their own heading instead of sharing Review suggested's urgency.
  const context = signals.filter((signal) => !isAttention(signal) && signal.kind !== "unlinked");
  const coverage = signals.filter((signal) => signal.kind === "unlinked");

  return (
    <MemberActionContext.Provider value={memberAction}>
      {/* The status strip already reads "✓ No issues"; a clean scope says it once. */}
      {part === "review" || attention.length === 0 ? null : (
        <Block title="Needs attention" count={attention.length} info={info}>
          <ul className="gv-list">
            {attention.map((signal) => (
              <Signal key={signalKey(signal)} model={model} signal={signal} />
            ))}
          </ul>
        </Block>
      )}
      {part !== "attention" && context.length > 0 && (
        <Block
          title="Review suggested"
          // The rows count records, so the heading names what it counts: checks.
          count={`${context.length} ${plural(context.length, "check", "checks")}`}
          info="Optional fields, connections, and update age. These observations do not imply required work."
        >
          <ul className="gv-list">
            {context.map((signal) => (
              <Signal key={signalKey(signal)} model={model} signal={signal} />
            ))}
          </ul>
        </Block>
      )}
      {part !== "attention" && coverage.length > 0 && (
        <Block
          title="Link coverage"
          info="Records with no link to another record in the group. Often expected for standalone notes."
        >
          <ul className="gv-list">
            {coverage.map((signal) => (
              <Signal key={signalKey(signal)} model={model} signal={signal} />
            ))}
          </ul>
        </Block>
      )}
    </MemberActionContext.Provider>
  );
}
