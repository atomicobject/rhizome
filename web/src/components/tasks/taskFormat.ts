import type { StatusTone } from "../StatusMark";
import type { TaskItemState } from "../../api/tasks";

const dateTime = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
});

const fullDateTime = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "short",
});

export function formatDateTime(value?: string): string {
  return value ? dateTime.format(new Date(value)) : "";
}

export function formatFullDateTime(value?: string): string {
  return value ? fullDateTime.format(new Date(value)) : "";
}

function span(ms: number): string {
  const minutes = Math.round(Math.abs(ms) / 60_000);

  if (minutes < 1) return "<1m";

  if (minutes < 60) return `${minutes}m`;
  const hours = Math.round(minutes / 60);

  if (hours < 48) return `${hours}h`;

  return `${Math.round(hours / 24)}d`;
}

/** "3h ago" or "in 3h". */
export function formatRelative(value: string | undefined, now = Date.now()): string {
  if (!value) return "";
  const delta = new Date(value).getTime() - now;

  if (Math.abs(delta) < 60_000) return "now";

  return delta > 0 ? `in ${span(delta)}` : `${span(delta)} ago`;
}

/** A due time: "in 3h", or "due now" once it has passed. */
export function formatDue(nextDue: string | undefined, due: boolean, now = Date.now()): string {
  if (due) return "Due now";

  if (!nextDue) return "Not scheduled";

  return new Date(nextDue).getTime() <= now ? "Due now" : formatRelative(nextDue, now);
}

export function formatDuration(from?: string, to?: string): string {
  if (!from || !to) return "";
  const ms = new Date(to).getTime() - new Date(from).getTime();

  if (!Number.isFinite(ms) || ms < 0) return "";

  if (ms < 1000) return `${ms}ms`;
  const seconds = ms / 1000;

  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 1 : 0)}s`;
  const whole = Math.round(seconds);

  return `${Math.floor(whole / 60)}m ${String(whole % 60).padStart(2, "0")}s`;
}

const integer = new Intl.NumberFormat();

export function formatTokens(tokens?: number): string {
  return tokens ? integer.format(tokens) : "";
}

export function shortCommit(commit?: string): string {
  return commit ? commit.slice(0, 8) : "";
}

export type StatusPresentation = { label: string; tone: StatusTone };

const OUTCOMES = {
  ready: ["Ready", "success"],
  "no-changes": ["No changes", "neutral"],
  "not-needed": ["Not needed", "muted"],
  notified: ["Notified", "info"],
  started: ["Started", "progress"],
  skipped: ["Skipped", "muted"],
  failed: ["Failed", "risk"],
  interrupted: ["Interrupted", "warning"],
  running: ["Running", "progress"],
} satisfies Record<string, [string, StatusTone]>;

export function outcomePresentation(outcome: string): StatusPresentation {
  // SAFETY: `in` just confirmed the key belongs to OUTCOMES.
  const [label, tone]: [string, StatusTone] =
    outcome in OUTCOMES ? OUTCOMES[outcome as keyof typeof OUTCOMES] : [outcome, "neutral"];

  return { label, tone };
}

const ITEM_STATES = {
  due: ["Due", "info"],
  running: ["Running", "progress"],
  ready: ["Ready for review", "success"],
  started: ["Started", "progress"],
  "needs-attention": ["Needs attention", "risk"],
  done: ["Done", "muted"],
  dismissed: ["Dismissed", "muted"],
} satisfies Record<TaskItemState, [string, StatusTone]>;

export function itemStatePresentation(state: TaskItemState): StatusPresentation {
  const [label, tone]: [string, StatusTone] = ITEM_STATES[state] ?? [state, "neutral"];

  return { label, tone };
}

export const MODE_LABELS = { auto: "Auto", notify: "Notify", off: "Off" } as const;
