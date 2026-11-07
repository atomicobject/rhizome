import type { ViewExecuteRequest, ViewFieldCapability } from "../api/types";
import { type DraftFilter, optionLabel } from "./ConfiguredViewFilterRow";
import { capabilityFor } from "./ConfiguredViewModel";
import { capabilityOptions, viewFieldControl } from "./viewFieldControls";

type FilterSpec = NonNullable<ViewExecuteRequest["filters"]>[number];

let nextFilterID = 0;

export function initialDraftFilter(capabilities: ViewFieldCapability[]): DraftFilter {
  const first = [...capabilities]
    .filter((capability) => viewFieldControl(capability).ops.length > 0)
    .sort((left, right) => draftFilterRank(right) - draftFilterRank(left))[0];

  return {
    id: newFilterID(),
    field: first?.key ?? capabilities[0]?.key ?? "",
    op: viewFieldControl(first).defaultOp,
    value: "",
    values: [],
  };
}

function draftFilterRank(capability: ViewFieldCapability) {
  let rank = capabilityOptions(capability).length > 0 ? 100 : 0;

  const names = [
    capability.key,
    capability.label,
    capability.canonicalField,
    capability.edit?.field,
  ]
    .filter(Boolean)
    .join(" ");

  if (capability.semanticRole === "status" || /status/i.test(names)) rank += 80;

  if (viewFieldControl(capability).ops.includes("in")) rank += 10;

  if (capability.semanticRole === "identifier" || capability.key === "id") rank -= 60;

  return rank;
}

export function draftsFromState(filters: FilterSpec[], capabilities: ViewFieldCapability[]) {
  return filters.map((filter) => {
    const values = (filter.values ?? []).map((value) => String(value));

    return {
      id: newFilterID(),
      field: filter.field,
      op: filter.op || viewFieldControl(capabilityFor(capabilities, filter.field)).defaultOp,
      value: String(filter.value ?? values.join(", ")),
      values,
    };
  });
}

function newFilterID() {
  nextFilterID += 1;

  return `filter-${nextFilterID}`;
}

export function filterSummary(filter: FilterSpec, capability: ViewFieldCapability | undefined) {
  if (filter.op === "in") {
    return (filter.values ?? []).map((value) => optionLabel(capability, String(value))).join(", ");
  }

  if (filter.op === "exists" || filter.op === "missing") return "";

  if (viewFieldControl(capability).kind === "relation")
    return optionLabel(capability, String(filter.value ?? ""));

  return String(filter.value ?? "");
}

export function filterCapabilitiesForDraft(
  display: ViewFieldCapability[],
  all: ViewFieldCapability[],
  field: string,
) {
  if (capabilityFor(display, field)) return display;
  const selected = capabilityFor(all, field);

  return selected ? [...display, selected] : display;
}
