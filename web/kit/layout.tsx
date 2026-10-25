// Page primitives for custom views: the pieces the bundled views are built
// from, so a view written with the kit's defaults reads like the rest of
// Rhizome. Styling lives in components.css (rz-* classes), on theme tokens.
import { useId, type ReactNode } from "react";

import { cn } from "./lib/utils";

/** A block's data state: loading, failed with a retry, or ready. */
export type SectionStatus =
  | { status: "loading" }
  | { status: "error"; error: Error; retry: () => void }
  | { status: "ready" };

type QueryLike = { isPending: boolean; error: Error | null; refetch: () => void };

/**
 * One state for a block from the queries it reads: failed when any failed
 * (retry refetches each failed query), loading while any is pending.
 */
export function sectionStatus(...queries: QueryLike[]): SectionStatus {
  const failed = queries.filter((query) => query.error);

  if (failed.length)
    return {
      status: "error",
      error: failed[0].error ?? new Error("Request failed"),
      retry: () => {
        for (const query of failed) query.refetch();
      },
    };

  return queries.some((query) => query.isPending) ? { status: "loading" } : { status: "ready" };
}

/**
 * A block of a page: a label-style heading with an optional mono count and
 * controls, a caption, and a body. The heading paints at once; the body shows
 * a delayed loading line, a failure with retry, or a one-line `empty` state in
 * its place. Pass `children` as a function to build it only once ready.
 */
export function Section({
  title,
  count,
  caption,
  controls,
  status,
  what,
  empty,
  className,
  children,
}: {
  title: ReactNode;
  count?: ReactNode;
  caption?: ReactNode;
  controls?: ReactNode;
  status?: SectionStatus;
  /** Names the data in state lines: "Loading {what}…". */
  what?: string;
  /** When set, the body is this one quiet line instead of `children`. */
  empty?: ReactNode;
  className?: string;
  children?: ReactNode | (() => ReactNode);
}) {
  const id = useId();

  return (
    <section className={cn("rz-section", className)} aria-labelledby={id}>
      <h2 className="rz-section-head">
        <span id={id}>{title}</span>
        {count !== undefined && count !== null && <Count>{count}</Count>}
        {controls && <span className="rz-section-controls">{controls}</span>}
      </h2>
      {caption && <p className="rz-caption">{caption}</p>}
      <SectionBody status={status} what={what} empty={empty}>
        {children}
      </SectionBody>
    </section>
  );
}

/**
 * A Section's body states on their own: a delayed loading line, a failure
 * with retry, a one-line empty state, or the content. Use inside a Section
 * whose controls need loaded data.
 */
export function SectionBody({
  status,
  what = "this section",
  empty,
  children,
}: {
  status?: SectionStatus;
  what?: string;
  empty?: ReactNode;
  children?: ReactNode | (() => ReactNode);
}) {
  if (status?.status === "loading")
    return (
      <p className="rz-loading" role="status">
        Loading {what}…
      </p>
    );

  if (status?.status === "error")
    return (
      <p className="rz-failure" role="alert">
        Could not load {what}: {status.error.message}{" "}
        <button type="button" className="rz-text-button" onClick={status.retry}>
          Retry
        </button>
      </p>
    );

  if (empty !== undefined && empty !== null && empty !== false)
    return <p className="rz-quiet">{empty}</p>;

  // oxlint-disable-next-line anti-slop/no-runtime-typeof -- React offers no other way to tell a render function from nodes.
  return <>{typeof children === "function" ? children() : children}</>;
}

/**
 * The facts strip under a page's title: an optional note on the left and
 * short facts on the right, such as `<Fact value={12}>records</Fact>`.
 */
export function Facts({
  note,
  children,
  className,
}: {
  note?: ReactNode;
  children?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("rz-facts", className)}>
      {note && <span className="rz-facts-note">{note}</span>}
      <span className="rz-facts-list">{children}</span>
    </div>
  );
}

/** One fact: a bold value and what it counts. `tone` marks risk or a clean result. */
export function Fact({
  value,
  tone,
  children,
}: {
  value?: ReactNode;
  tone?: "risk" | "ok";
  children?: ReactNode;
}) {
  return (
    <span className="rz-fact" data-tone={tone}>
      {tone === "ok" && <span aria-hidden="true">✓ </span>}
      {value !== undefined && <b>{value}</b>} {children}
    </span>
  );
}

/** A count: unboxed mono numerals. A `risk` count is red ink; say what it counts beside it. */
export function Count({
  children,
  tone,
  className,
}: {
  children: ReactNode;
  tone?: "risk";
  className?: string;
}) {
  return (
    <span className={cn("rz-count", className)} data-tone={tone}>
      {children}
    </span>
  );
}

/** An empty value: a dash on screen, "None" to assistive technology. */
export function Dash() {
  return (
    <span className="rz-dash">
      <span aria-hidden="true">—</span>
      <span className="sr-only">None</span>
    </span>
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
    <button
      type="button"
      className="rz-text-button rz-more"
      aria-expanded={open}
      onClick={onToggle}
    >
      {open ? "Show fewer" : `+${hidden} more`}
    </button>
  );
}

export type SegmentedOption<T extends string> = {
  value: T;
  label: ReactNode;
  /** A mono count after the label. */
  count?: ReactNode;
  /** Extra text for assistive technology only, such as "(default)". */
  description?: string;
};

/**
 * One mode switch: a bordered row of segments where the chosen one fills with
 * ink. Native radios underneath, so arrow keys move the choice.
 */
export function Segmented<T extends string>({
  label,
  value,
  options,
  onChange,
  showLabel = false,
  className,
}: {
  /** Names the choice for assistive technology; shown before the control with `showLabel`. */
  label: string;
  value: T;
  options: readonly SegmentedOption<T>[];
  onChange: (value: T) => void;
  showLabel?: boolean;
  className?: string;
}) {
  const id = useId();

  return (
    <span className={cn("rz-segmented-wrap", className)}>
      {showLabel && (
        <span id={`${id}-label`} className="rz-label">
          {label}
        </span>
      )}
      <span
        className="rz-segmented"
        role="radiogroup"
        aria-label={showLabel ? undefined : label}
        aria-labelledby={showLabel ? `${id}-label` : undefined}
      >
        {options.map((option) => (
          <label key={option.value} className="rz-segment">
            <input
              type="radio"
              className="sr-only"
              name={id}
              value={option.value}
              checked={option.value === value}
              onChange={() => onChange(option.value)}
            />
            {option.label}
            {option.count !== undefined && (
              <>
                {" "}
                <Count>{option.count}</Count>
              </>
            )}
            {option.description && (
              <>
                {" "}
                <span className="sr-only">{option.description}</span>
              </>
            )}
          </label>
        ))}
      </span>
    </span>
  );
}

export type BarListItem = {
  /** Stable key; defaults to the item's position. */
  key?: string;
  label: ReactNode;
  value: number;
  /** Shown instead of the value, such as "34.75 h · 21%". */
  display?: ReactNode;
  /** 1–8 picks `--series-N`; "link" picks the connection teal. Defaults to 1. */
  series?: number | "link";
  /** Makes the label a button, such as opening a note. */
  onOpen?: () => void;
};

/**
 * Proportional bars as an HTML list: label, bar, and value per row. Bars
 * scale to the largest value, or to `max` when given. Use instead of a chart
 * when the reader compares a handful of named amounts.
 */
export function BarList({
  items,
  max,
  label,
  className,
}: {
  items: readonly BarListItem[];
  max?: number;
  /** Names the list for assistive technology. */
  label: string;
  className?: string;
}) {
  const top = max ?? Math.max(0, ...items.map((item) => item.value));

  return (
    <ul className={cn("rz-bars", className)} aria-label={label}>
      {items.map((item, index) => {
        const width = top > 0 ? Math.max(0, Math.min(1, item.value / top)) : 0;
        const series = item.series === "link" ? "link" : String(item.series ?? 1);

        return (
          <li key={item.key ?? index}>
            <span className="rz-bars-label">
              {item.onOpen ? (
                <button type="button" className="rz-text-button" onClick={item.onOpen}>
                  {item.label}
                </button>
              ) : (
                item.label
              )}
            </span>
            <span className="rz-bars-track" aria-hidden="true">
              <i data-series={series} style={{ inlineSize: `${(width * 100).toFixed(2)}%` }} />
            </span>
            <Count>{item.display ?? item.value}</Count>
          </li>
        );
      })}
    </ul>
  );
}
