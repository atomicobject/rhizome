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

const TONE_COLOR: Record<StatusTone, string> = {
  neutral: "var(--text-faint)",
  info: "var(--info)",
  progress: "var(--info)",
  success: "var(--ao-teal-ink)",
  warning: "var(--warning-ink)",
  risk: "var(--accent-active)",
  muted: "var(--ao-gray-400)",
};

const R = 4.6;

const C = 5.5;

function wedge(fill: number) {
  const angle = fill * 2 * Math.PI;
  const x = (C + R * Math.sin(angle)).toFixed(2);
  const y = (C - R * Math.cos(angle)).toFixed(2);

  return `M${C} ${C}L${C} ${C - R}A${R} ${R} 0 ${fill > 0.5 ? 1 : 0} 1 ${x} ${y}Z`;
}

function Glyph({ position }: { position: StatusPosition }) {
  if (position.kind === "unknown") return null;

  if (position.kind === "done") {
    return (
      <>
        <circle cx={C} cy={C} r={R} fill="currentColor" />
        <path d="M3.4 5.6l1.5 1.5 2.8-3" stroke="var(--ao-white)" strokeWidth="1.3" fill="none" />
      </>
    );
  }

  if (position.kind === "closed") {
    return <path d="M2.6 5.5h5.8" stroke="currentColor" strokeWidth="1.4" />;
  }

  if (position.fill >= 1) return <circle cx={C} cy={C} r={R} fill="currentColor" />;

  return position.fill > 0 ? <path d={wedge(position.fill)} fill="currentColor" /> : null;
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
 * A lifecycle value: the mark's fill shows how far along the value is, its
 * color shows the value's tone, and the label always accompanies it.
 */
export function StatusMark({ value, values, label, hideLabel, className }: StatusMarkProps) {
  if (!value) return null;
  const position = statusPosition(values, value);
  const doc = position.kind === "unknown" ? undefined : position.value;
  const text = label ?? doc?.label ?? value;

  return (
    <span
      data-status={position.kind}
      title={hideLabel && isString(text) ? text : undefined}
      className={cn(
        "inline-flex min-w-0 items-center gap-1.5 text-[12px] leading-tight whitespace-nowrap",
        className,
      )}
    >
      <svg
        aria-hidden="true"
        viewBox="0 0 11 11"
        className="size-[11px] shrink-0"
        style={{ color: TONE_COLOR[toneOf(doc?.tone)] }}
      >
        <circle cx={C} cy={C} r={R} fill="none" stroke="currentColor" strokeWidth="1.2" />
        <Glyph position={position} />
      </svg>
      <span className={hideLabel ? "sr-only" : "truncate"}>{text}</span>
    </span>
  );
}
