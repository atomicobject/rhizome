import type { NodeFieldEnumOption } from "../../api/types";
import { enumLabel } from "../../lib/labels";
import { StatusMark } from "../StatusMark";

type Props = {
  /** Current value — must be one of `options` for the pill to highlight. */
  value: string;
  /** Admitted enum values, in declared order. */
  options: string[];
  /** Schema label and tone per value; values without one are humanized. */
  optionLabels?: NodeFieldEnumOption[];
  /** True when the surrounding session is in edit mode. */
  editing: boolean;
  /** True when this field has unsaved changes in the active edit session. */
  dirty?: boolean;
  /** Stage a setField op for the chosen value. */
  onStage?: (next: string) => void;
  /** Optional aria-label override. */
  ariaLabel?: string;
  /** Allow an optional enum field to be removed. */
  clearable?: boolean;
};

/**
 * Segmented pill control for ontology enums. In browse mode it renders as a
 * single highlighted badge for the active value; in edit mode the full set of
 * admitted values renders as clickable segments. We never write a value that
 * is not in `options` — selection is constrained by the schema.
 */
export function EnumPillWidget({
  value,
  options,
  optionLabels,
  editing,
  dirty = false,
  onStage,
  ariaLabel,
  clearable = false,
}: Props) {
  const trimmed = value.trim();
  const known = options.includes(trimmed);
  const optionFor = (option: string) => optionLabels?.find((item) => item.value === option);
  const labelFor = (option: string) => enumLabel(option, optionFor(option)?.label);

  if (!editing) {
    if (!trimmed) {
      return <span className="widget-enum widget-enum--empty">No value</span>;
    }

    const tone = known ? optionFor(trimmed)?.tone : undefined;

    return (
      <span
        className={`widget-enum widget-enum--badge${dirty ? " is-dirty" : ""}${known ? "" : " widget-enum--unknown"}`}
        data-value={trimmed}
        title={known ? trimmed : `Value "${trimmed}" not in schema`}
      >
        {tone ? (
          <StatusMark tone={tone} label={labelFor(trimmed)} />
        ) : known ? (
          labelFor(trimmed)
        ) : (
          trimmed
        )}
      </span>
    );
  }

  return (
    <fieldset className={`widget-enum widget-enum--segmented${dirty ? " is-dirty" : ""}`}>
      {ariaLabel && <legend className="widget-enum__legend">{ariaLabel}</legend>}
      {options.map((option) => {
        const active = option === trimmed;

        return (
          <button
            key={option}
            type="button"
            aria-pressed={active}
            data-value={option}
            className={`widget-enum__segment${active ? " is-active" : ""}`}
            onClick={() => {
              if (!active) onStage?.(option);
            }}
          >
            {labelFor(option)}
          </button>
        );
      })}
      {clearable && trimmed ? (
        <button
          type="button"
          className="widget-enum__clear"
          aria-label={`Clear ${ariaLabel || "value"}`}
          onClick={() => onStage?.("")}
        >
          Clear
        </button>
      ) : null}
      {trimmed && !known && (
        <span
          className="widget-enum__segment widget-enum__segment--unknown"
          title="Current value is not in the schema"
        >
          {trimmed}
        </span>
      )}
    </fieldset>
  );
}
