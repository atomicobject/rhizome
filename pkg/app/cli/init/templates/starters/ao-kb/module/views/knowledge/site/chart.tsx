// One interaction pattern for every site chart (SPEC-0135 R8): the chart is one tab stop with a
// roving tabindex, arrow keys move through its marks in the order the library gives, Enter (and
// Space in the matrix) activates, and the hover tooltip also shows on focus.
import { useCallback, useRef, useState } from "react";
import type { KeyboardEvent, ReactNode } from "react";
import "./charts.css";

type Tip = { text: string; x: number; y: number } | null;

export function useChart(count: number) {
  const [active, setActive] = useState(0);
  const [tip, setTip] = useState<Tip>(null);
  const [hot, setHot] = useState<number | null>(null);
  const box = useRef<HTMLDivElement>(null);
  const items = useRef<(Element | null)[]>([]);
  const at = Math.min(active, Math.max(0, count - 1));

  const show = useCallback((text: string, el: Element | null) => {
    const outer = box.current?.getBoundingClientRect(),
      r = el?.getBoundingClientRect();
    if (!outer || !r) return;
    setTip({ text, x: r.left + r.width / 2 - outer.left, y: r.top - outer.top });
  }, []);
  const move = useCallback((i: number) => {
    setActive(i);
    (items.current[i] as HTMLElement | SVGElement | null)?.focus();
  }, []);

  /** Props for the mark at position `i` of the arrow order. */
  const item = (i: number, text: string, activate?: () => void) => ({
    ref: (el: Element | null) => {
      items.current[i] = el;
    },
    tabIndex: i === at ? 0 : -1,
    "aria-label": text.replace(/\n/g, ", "),
    onFocus: (e: { currentTarget: Element }) => {
      setActive(i);
      setHot(i);
      show(text, e.currentTarget);
    },
    onBlur: () => {
      setHot(null);
      setTip(null);
    },
    onMouseEnter: (e: { currentTarget: Element }) => {
      setHot(i);
      show(text, e.currentTarget);
    },
    onMouseLeave: () => {
      setHot(null);
      setTip(null);
    },
    onClick: (e: {
      preventDefault: () => void;
      defaultPrevented: boolean;
      metaKey?: boolean;
      ctrlKey?: boolean;
    }) => {
      // A link inside the mark that already navigated, or a modified click the browser opens beside.
      if (!activate || e.defaultPrevented || e.metaKey || e.ctrlKey) return;
      e.preventDefault();
      activate();
    },
  });

  /** Arrow keys along one order; Home and End jump to its ends. */
  const onKeyDown = (e: KeyboardEvent) => {
    const step = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[e.key];
    if (step) move(Math.max(0, Math.min(count - 1, at + step)));
    else if (e.key === "Home") move(0);
    else if (e.key === "End") move(count - 1);
    else return;
    e.preventDefault();
  };

  return { box, item, onKeyDown, hot, active: at, move, tip };
}

/** The chart's frame: the tooltip floats inside it, above the focused or hovered mark. */
export function ChartBox({
  chart,
  label,
  className,
  children,
}: {
  chart: ReturnType<typeof useChart>;
  label: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div
      ref={chart.box}
      className={`ak-chart ${className ?? ""}`}
      role="group"
      aria-label={label}
      onKeyDown={chart.onKeyDown}
    >
      {children}
      {chart.tip && (
        <div className="ak-tip" role="tooltip" style={{ left: chart.tip.x, top: chart.tip.y }}>
          {chart.tip.text}
        </div>
      )}
    </div>
  );
}

/** The colour token of a kind group. */
export const kindColor = (group: string | null | undefined) => `var(--kind-${group ?? "other"})`;
