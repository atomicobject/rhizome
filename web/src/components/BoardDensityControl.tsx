import type { ViewExecuteResponse } from "../api/types";

export type BoardDensity = "full" | "compact";

export function defaultBoardDensity(board: ViewExecuteResponse["board"]): BoardDensity {
  return board?.columns.some((column) => column.count > 24) ? "compact" : "full";
}

export function BoardDensityControl({
  density,
  onChange,
}: {
  density: BoardDensity;
  onChange: (value: BoardDensity) => void;
}) {
  return (
    <div className="configured-view__select">
      <span>Cards</span>
      <span className="configured-view__segmented" role="group" aria-label="Card density">
        {(["full", "compact"] as const).map((value) => (
          <button
            key={value}
            type="button"
            aria-pressed={density === value}
            onClick={() => onChange(value)}
          >
            {value === "full" ? "Full" : "Compact"}
          </button>
        ))}
      </span>
    </div>
  );
}
