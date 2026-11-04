import type { ReactNode } from "react";

import { isString } from "../src/api/parse";
import { normalizeStatusTone, type StatusTone } from "../src/components/StatusMark";
import { cn } from "./lib/utils";
import type { EnumValueDoc } from "./types";

/** Lifecycle values in `@view` order; values without an order keep declaration order, last. */
export function orderedEnumValues(values: readonly EnumValueDoc[]): EnumValueDoc[] {
  return values
    .map((value, index) => ({ value, index }))
    .sort(
      (a, b) =>
        (a.value.order ?? Number.POSITIVE_INFINITY) - (b.value.order ?? Number.POSITIVE_INFINITY) ||
        a.index - b.index,
    )
    .map(({ value }) => value);
}

/** Values in the `done` or `dropped` stage end a lifecycle; tone only colors them. */
export function isTerminalValue(value: EnumValueDoc) {
  return value.stage === "done" || value.stage === "dropped";
}

export type StatusPosition =
  | { kind: "active"; value: EnumValueDoc; fill: number }
  | { kind: "done"; value: EnumValueDoc }
  | { kind: "closed"; value: EnumValueDoc }
  | { kind: "unknown"; name: string };

/**
 * Where `name` sits in its lifecycle. An active value's fill runs from 0 for
 * the first non-terminal value to 1 for the last; terminal values are done
 * (`done` stage) or closed (`dropped` stage).
 */
export function statusPosition(values: readonly EnumValueDoc[], name: string): StatusPosition {
  const ordered = orderedEnumValues(values);
  const value = ordered.find((candidate) => candidate.name === name);

  if (!value) return { kind: "unknown", name };

  if (value.stage === "done") return { kind: "done", value };

  if (isTerminalValue(value)) return { kind: "closed", value };

  const active = ordered.filter((candidate) => !isTerminalValue(candidate));
  const index = active.indexOf(value);

  return { kind: "active", value, fill: active.length > 1 ? index / (active.length - 1) : 1 };
}

function toneOf(tone: string | undefined): StatusTone {
  return tone === "danger" ? "risk" : normalizeStatusTone(tone);
}

/**
 * The value's tone, or one its lifecycle position implies when the schema
 * names none: a done value succeeds, a dropped one is muted, an active one
 * past the first is in progress, and the first is neutral.
 */
function markTone(tone: string | undefined, position: StatusPosition): StatusTone {
  if (tone) return toneOf(tone);

  if (position.kind === "done") return "success";

  if (position.kind === "closed") return "muted";

  return position.kind === "active" && position.fill > 0 ? "progress" : "neutral";
}

const TONE_COLOR: Record<StatusTone, string> = {
  neutral: "var(--text-faint)",
  info: "var(--info)",
  progress: "var(--info)",
  success: "var(--success)",
  warning: "var(--warning-ink)",
  risk: "var(--accent)",
  muted: "var(--text-faint)",
};

// The host's 9px shape set (DESIGN.md, Status Marks): a hollow ring, a
// half-filled ring for progress, a filled dot for success, a triangle for a
// warning, a disc with an exclamation cut out for risk (never the brand
// diamond), and a dashed ring for a muted value.
function ToneMark({ tone }: { tone: StatusTone }) {
  switch (tone) {
    case "warning":
      return <path d="M4.5 0.5L8.8 8.3H0.2Z" fill="currentColor" />;
    case "risk":
      // A disc with an exclamation cut out, as the host draws it (base.css).
      return (
        <path
          transform="scale(0.75)"
          fillRule="evenodd"
          d="M6 0a6 6 0 1 0 0 12A6 6 0 0 0 6 0ZM5.25 2.5h1.5v4.75h-1.5ZM5.25 8.25h1.5v1.5h-1.5Z"
          fill="currentColor"
        />
      );
    case "success":
      return <circle cx="4.5" cy="4.5" r="4.5" fill="currentColor" />;
    case "info":
    case "progress":
      return (
        <>
          <circle cx="4.5" cy="4.5" r="3.75" fill="none" stroke="currentColor" strokeWidth="1.5" />
          <path d="M4.5 0.75A3.75 3.75 0 0 0 4.5 8.25Z" fill="currentColor" />
        </>
      );
    default:
      return (
        <circle
          cx="4.5"
          cy="4.5"
          r="3.75"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeDasharray={tone === "muted" ? "1.8 1.4" : undefined}
        />
      );
  }
}

type StatusMarkProps = {
  /** The record's enum value name; nothing renders when it is empty. */
  value: string | null | undefined;
  /** Every value of the lifecycle enum, from type documentation. */
  values: readonly EnumValueDoc[];
  /** Replaces the value's `@view` label. */
  label?: ReactNode;
  /** Keep the label for assistive technology and the tooltip only, as in record chips. */
  hideLabel?: boolean;
  className?: string;
};

/**
 * A lifecycle value as the host draws it: the tone's shape, never colour
 * alone, and the label always beside it or named for assistive technology.
 */
export function StatusMark({ value, values, label, hideLabel, className }: StatusMarkProps) {
  if (!value) return null;
  const position = statusPosition(values, value);
  const doc = position.kind === "unknown" ? undefined : position.value;
  const text = label ?? doc?.label ?? value;
  const tone = markTone(doc?.tone, position);

  return (
    <span
      data-status={position.kind}
      data-tone={tone}
      title={hideLabel && isString(text) ? text : undefined}
      className={cn(
        "inline-flex min-w-0 items-center gap-1.5 leading-tight whitespace-nowrap",
        className,
      )}
    >
      <svg
        aria-hidden="true"
        viewBox="0 0 9 9"
        className="size-[9px] shrink-0"
        style={{ color: TONE_COLOR[tone] }}
      >
        <ToneMark tone={tone} />
      </svg>
      <span className={hideLabel ? "sr-only" : "truncate"}>{text}</span>
    </span>
  );
}
