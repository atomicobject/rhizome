import { type KeyboardEvent, type PointerEvent, type RefObject, useRef } from "react";

import { clampWidth, type PanelRange } from "./shellPanes";

const STEP = 16;

// Per axis: the keys that move the handle toward the end edge and back, and
// the pointer coordinate a drag follows.
const AXES = {
  x: { forward: "ArrowRight", back: "ArrowLeft", orientation: "vertical", client: "clientX" },
  y: { forward: "ArrowDown", back: "ArrowUp", orientation: "horizontal", client: "clientY" },
} as const;

/**
 * A keyboard-operable drag handle on a panel's inner edge. `axis` is the
 * dimension it resizes: "x" for a side panel's width, "y" for a stacked
 * panel's height. `edge` says which side of the panel it sits on: "end" (the
 * sidebar's right edge, where dragging right widens it) or "start" (the right
 * pane's left edge, or a bottom panel's top edge, where dragging up grows it).
 * While dragging it writes `cssVar` straight onto `target`, so the shell does
 * not re-render on every pointer move, and reports the size on release.
 */
export function PanelResizeHandle({
  axis = "x",
  label,
  range,
  value,
  edge,
  cssVar,
  target,
  onResize,
}: {
  axis?: keyof typeof AXES;
  label: string;
  range: PanelRange;
  value: number;
  edge: "start" | "end";
  cssVar: string;
  target: RefObject<HTMLElement | null>;
  onResize: (size: number) => void;
}) {
  const drag = useRef<{ at: number; start: number; size: number } | null>(null);
  const direction = edge === "end" ? 1 : -1;
  const keys = AXES[axis];

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const next =
      event.key === keys.forward
        ? value + STEP * direction
        : event.key === keys.back
          ? value - STEP * direction
          : event.key === "Home"
            ? range.min
            : event.key === "End"
              ? range.max
              : null;

    if (next === null) return;
    event.preventDefault();
    onResize(clampWidth(next, range));
  };

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture?.(event.pointerId);
    event.currentTarget.focus();
    drag.current = { at: event[keys.client], start: value, size: value };
  };

  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const current = drag.current;

    if (!current) return;
    current.size = clampWidth(current.start + (event[keys.client] - current.at) * direction, range);
    target.current?.style.setProperty(cssVar, `${current.size}px`);
  };

  const endDrag = () => {
    const current = drag.current;

    if (!current) return;
    drag.current = null;
    onResize(current.size);
  };

  return (
    <div
      role="separator"
      aria-orientation={keys.orientation}
      aria-label={label}
      aria-valuenow={value}
      aria-valuemin={range.min}
      aria-valuemax={range.max}
      tabIndex={0}
      className={`panel-resize panel-resize--${axis === "y" ? "y-" : ""}${edge}`}
      onKeyDown={onKeyDown}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
    />
  );
}
