import { type KeyboardEvent, type PointerEvent, type RefObject, useRef } from "react";

import { clampWidth, type PanelRange } from "./shellPanes";

const STEP = 16;

/**
 * A keyboard-operable drag handle on a side panel's inner edge. `edge` says
 * which side of the panel it sits on: "end" (the sidebar's right edge, where
 * dragging right widens it) or "start" (the right pane's left edge). While
 * dragging it writes `cssVar` straight onto `target`, so the shell does not
 * re-render on every pointer move, and reports the width on release.
 */
export function PanelResizeHandle({
  label,
  range,
  value,
  edge,
  cssVar,
  target,
  onResize,
}: {
  label: string;
  range: PanelRange;
  value: number;
  edge: "start" | "end";
  cssVar: string;
  target: RefObject<HTMLElement | null>;
  onResize: (width: number) => void;
}) {
  const drag = useRef<{ x: number; start: number; width: number } | null>(null);
  const direction = edge === "end" ? 1 : -1;

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const next =
      event.key === "ArrowRight"
        ? value + STEP * direction
        : event.key === "ArrowLeft"
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
    drag.current = { x: event.clientX, start: value, width: value };
  };

  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const current = drag.current;

    if (!current) return;
    current.width = clampWidth(current.start + (event.clientX - current.x) * direction, range);
    target.current?.style.setProperty(cssVar, `${current.width}px`);
  };

  const endDrag = () => {
    const current = drag.current;

    if (!current) return;
    drag.current = null;
    onResize(current.width);
  };

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={value}
      aria-valuemin={range.min}
      aria-valuemax={range.max}
      tabIndex={0}
      className={`panel-resize panel-resize--${edge}`}
      onKeyDown={onKeyDown}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
    />
  );
}
