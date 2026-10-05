import type { ViewExecuteRequest, ViewFieldCapability } from "../api/types";
import { enumLabel } from "../lib/labels";
import { relationDisplayValue } from "./ConfiguredTableCellReadOnly";
import { capabilityFor, capabilityLabel } from "./ConfiguredViewModel";
import { viewFieldControl } from "./viewFieldControls";

type FilterSpec = NonNullable<ViewExecuteRequest["filters"]>[number];

export type DraftFilter = {
  id: string;
  field: string;
  op: string;
  value: string;
  values: string[];
};

const OP_LABELS = new Map([
  ["eq", "is"],
  ["neq", "is not"],
  ["in", "is any of"],
  ["contains", "contains"],
  ["gt", ">"],
  ["gte", "≥"],
  ["lt", "<"],
  ["lte", "≤"],
  ["exists", "has a value"],
  ["missing", "is empty"],
]);

/** One filter being edited: its field, operator, and value or values. */
export function FilterRow({
  filter,
  capabilities,
  onFieldChange,
  onOpChange,
  onValueChange,
  onToggleValue,
  onRemove,
}: {
  filter: DraftFilter;
  capabilities: ViewFieldCapability[];
  onFieldChange: (field: string) => void;
  onOpChange: (op: string) => void;
  onValueChange: (value: string) => void;
  onToggleValue: (value: string) => void;
  onRemove: () => void;
}) {
  const selectedCapability = capabilityFor(capabilities, filter.field);
  const control = viewFieldControl(selectedCapability);

  const selectedValues = filter.op === "in" ? filter.values : [filter.value];
  const presence = filter.op === "exists" || filter.op === "missing";
  // A facet can apply an operator the field does not list; keep it choosable.
  const ops = control.ops.includes(filter.op) ? control.ops : [...control.ops, filter.op];

  const useOptions =
    !presence &&
    control.options.length > 0 &&
    (filter.op === "in" ||
      control.kind === "enum" ||
      control.kind === "boolean" ||
      (control.kind === "relation" && control.options.length <= 50));

  return (
    <div className="configured-view__filter-row" data-filter-id={filter.id}>
      <label>
        <span>Field</span>
        <select value={filter.field} onChange={(event) => onFieldChange(event.target.value)}>
          {capabilities.map((capability) => (
            <option key={capability.key} value={capability.key}>
              {capabilityLabel(capability)}
            </option>
          ))}
        </select>
      </label>
      <label>
        <span>Operator</span>
        <select value={filter.op} onChange={(event) => onOpChange(event.target.value)}>
          {ops.map((op) => (
            <option key={op} value={op}>
              {opLabel(op)}
            </option>
          ))}
        </select>
      </label>
      {useOptions ? (
        <fieldset className="configured-view__filter-values">
          <legend>Values</legend>
          {control.options.length <= 12 ? (
            <div className="configured-view__filter-pills">
              {control.options.map((value) => (
                <button
                  key={value}
                  type="button"
                  className={selectedValues.includes(value) ? "is-selected" : ""}
                  aria-pressed={selectedValues.includes(value)}
                  onClick={() => onToggleValue(value)}
                >
                  {optionLabel(selectedCapability, value)}
                </button>
              ))}
            </div>
          ) : (
            <details className="configured-view__filter-value-menu">
              <summary>
                {filterValueMenuLabel(selectedCapability, selectedValues.filter(Boolean))}
              </summary>
              <div>
                {control.options.map((value) => (
                  <label key={value}>
                    <input
                      type={filter.op === "in" ? "checkbox" : "radio"}
                      name={filter.id}
                      checked={selectedValues.includes(value)}
                      onChange={() => onToggleValue(value)}
                    />
                    <span>{optionLabel(selectedCapability, value)}</span>
                  </label>
                ))}
              </div>
            </details>
          )}
        </fieldset>
      ) : !presence ? (
        <label className="configured-view__filter-text">
          <span>Value</span>
          <input
            type={control.inputType}
            value={filter.value}
            onChange={(event) => onValueChange(event.target.value)}
            placeholder={filter.op === "contains" ? "Contains…" : ""}
          />
        </label>
      ) : null}
      <button type="button" onClick={onRemove}>
        Remove
      </button>
    </div>
  );
}

export function optionLabel(capability: ViewFieldCapability | undefined, value: string) {
  if (viewFieldControl(capability).kind === "relation")
    return capability?.facets?.labels?.[value] || relationDisplayValue(value);

  const option = capability?.enumValues?.find((item) => item.value === value);

  return option ? enumLabel(value, option.label) : value;
}

export function opLabel(op: string) {
  return OP_LABELS.get(op) ?? op;
}

function filterValueMenuLabel(capability: ViewFieldCapability | undefined, values: string[]) {
  if (values.length === 0) return "Select values";

  if (values.length <= 2) return values.map((value) => optionLabel(capability, value)).join(", ");

  return `${values.length} selected`;
}

/** The filter a draft row describes. */
export function filterSpecForDraft(draft: DraftFilter): FilterSpec {
  if (draft.op === "in") {
    return {
      field: draft.field,
      op: draft.op,
      values:
        draft.values.length > 0
          ? draft.values
          : draft.value.split(",").flatMap((value) => (value.trim() ? [value.trim()] : [])),
    };
  }

  // Presence tests take no value.
  if (draft.op === "exists" || draft.op === "missing") return { field: draft.field, op: draft.op };

  return { field: draft.field, op: draft.op, value: draft.value };
}

/** Whether a filter has what it needs to run: a field, an operator, and a value unless it tests presence. */
export function filterIsRunnable(filter: FilterSpec) {
  if (!filter.field || !filter.op) return false;

  if (filter.op === "exists" || filter.op === "missing") return true;

  if (filter.op === "in") return (filter.values ?? []).length > 0;

  return filter.value !== undefined && String(filter.value).trim() !== "";
}
