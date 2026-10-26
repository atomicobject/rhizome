import { type ReactNode, useState } from "react";

import type {
  TypeProfile,
  ViewExecuteRequest,
  ViewExecutionStats,
  ViewFieldCapability,
} from "../api/types";
import { isString } from "../api/parse";
import { enumLabel } from "../lib/labels";
import { capabilityLabel, fieldCapability } from "./ConfiguredViewModel";
import { StatusMark } from "./StatusMark";
import { useTypeLabel } from "./typeLabels";
import { STALE_DAYS } from "./viewProfile";

type Filter = NonNullable<ViewExecuteRequest["filters"]>[number];

/** The server's stale rule, the one `stats.staleCount` counts by. */
const STALE_FILTER: Filter = { field: "stale", op: "eq", value: "true" };

type Props = {
  stats: ViewExecutionStats;
  profile?: TypeProfile;
  capabilities: ViewFieldCapability[];
  filters: Filter[];
  /** The filters these statistics were computed under, as the execution reports them. */
  executedFilters?: Filter[];
  onFiltersChange: (filters: Filter[]) => void;
};

/**
 * The collection's quick filters in the Filter menu (SPEC-0112, moved there
 * by SPEC-0126): every count comes from the execution statistics over all
 * matching rows, not the loaded page, and each facet toggles the filter it
 * describes.
 */
export function ConfiguredViewFacets({
  stats,
  profile,
  capabilities,
  filters,
  executedFilters = [],
  onFiltersChange,
}: Props) {
  const typeLabel = useTypeLabel();
  const lifecycle = fieldCapability(capabilities, profile?.lifecycleField);
  const lifecycleKey = lifecycle?.key ?? profile?.lifecycleField ?? "";
  const lifecycleCounts = new Map(stats.lifecycle?.map((item) => [item.value, item.count]));
  const kindCounts = new Map(stats.kinds?.map((item) => [item.value, item.count]));
  const lifecycleShown = useFacetValues(lifecycleKey, stats.lifecycle, executedFilters, filters);
  const kindsShown = useFacetValues("resolvedType", stats.kinds, executedFilters, filters);
  const filled = new Map(stats.fields?.map((item) => [item.field, item.filled]));

  const toggle = (filter: Filter) => onFiltersChange(toggleFilter(filters, filter));

  const gaps = (profile?.gapFields ?? []).flatMap((field) => {
    const capability = fieldCapability(capabilities, field);
    const count = filled.get(capability?.key ?? field) ?? filled.get(field);

    const key = capability?.key ?? field;
    const pressed = filters.some((filter) => filter.field === key && filter.op === "missing");

    if (count === undefined || (count >= stats.total && !pressed)) return [];

    return [{ key, capability, empty: stats.total - count, pressed }];
  });

  // A facet the reader has applied stays, even when its count falls to zero.
  const staleActive = hasFilter(filters, STALE_FILTER);
  const issuesActive = filters.some((filter) => filter.field === "hasIssues");

  const empty =
    kindsShown.length === 0 &&
    !(lifecycle && lifecycleShown.length > 0) &&
    gaps.length === 0 &&
    !(stats.staleCount > 0 || staleActive) &&
    !(stats.issueCount > 0 || issuesActive);

  if (empty) return null;

  return (
    <div
      className="view-facets configured-view__options-section"
      role="group"
      aria-label="Quick filters"
    >
      <h4>Quick filters</h4>
      {kindsShown.length > 0 && (
        <span className="view-facets__set">
          {kindsShown.map((kind) => (
            <FacetButton
              key={kind}
              plain
              label={typeLabel(kind)}
              count={kindCounts.get(kind) ?? 0}
              pressed={hasValue(filters, "resolvedType", kind)}
              onClick={() => toggle({ field: "resolvedType", op: "in", values: [kind] })}
            />
          ))}
        </span>
      )}
      {lifecycle && lifecycleShown.length > 0 && (
        <span className="view-facets__set">
          {(lifecycle.enumValues ?? []).flatMap((value) =>
            lifecycleShown.includes(value.value)
              ? [
                  <FacetButton
                    key={value.value}
                    label={
                      <StatusMark tone={value.tone} label={enumLabel(value.value, value.label)} />
                    }
                    name={enumLabel(value.value, value.label)}
                    count={lifecycleCounts.get(value.value) ?? 0}
                    pressed={hasValue(filters, lifecycleKey, value.value)}
                    onClick={() => toggle({ field: lifecycleKey, op: "in", values: [value.value] })}
                  />,
                ]
              : [],
          )}
        </span>
      )}
      {gaps.map((gap) => {
        const label = gap.capability ? capabilityLabel(gap.capability) : gap.key;

        return (
          <FacetButton
            key={gap.key}
            label={`${label} empty`}
            count={`${gap.empty}/${stats.total}`}
            title={gap.capability?.policyReason}
            pressed={gap.pressed}
            onClick={() => toggle({ field: gap.key, op: "missing" })}
          />
        );
      })}
      {(stats.staleCount > 0 || staleActive) && (
        <FacetButton
          label={`Unchanged ${STALE_DAYS}d`}
          count={stats.staleCount}
          title={`In an active stage and unchanged for ${STALE_DAYS} days`}
          pressed={staleActive}
          onClick={() => toggle(STALE_FILTER)}
        />
      )}
      {/* Counts records the ontology flags, not validation issues, so it never
          claims the collection is clean: the header owns that number. */}
      {(stats.issueCount > 0 || issuesActive) && (
        <FacetButton
          tone="risk"
          label={stats.issueCount === 1 ? "Record with issues" : "Records with issues"}
          count={stats.issueCount}
          title="Show only records that have issues"
          pressed={issuesActive}
          onClick={() => toggle({ field: "hasIssues", op: "eq", value: "true" })}
        />
      )}
    </div>
  );
}

function FacetButton({
  label,
  name,
  count,
  pressed,
  title,
  tone,
  plain = false,
  onClick,
}: {
  label: ReactNode;
  /** Accessible name; defaults to the label when it is plain text. */
  name?: string;
  count: number | string;
  pressed: boolean;
  title?: string;
  tone?: "risk";
  plain?: boolean;
  onClick: () => void;
}) {
  const text = name ?? (isString(label) ? label : "");

  return (
    <button
      type="button"
      className={`configured-view__menu-toggle view-facets__facet${plain ? " is-plain" : ""}${tone ? ` is-${tone}` : ""}`}
      aria-pressed={pressed}
      aria-label={`${text} ${count}`}
      title={title}
      onClick={onClick}
    >
      {label}
      <b>{count}</b>
    </button>
  );
}

/**
 * The values a facet keeps offering: those populated when the executed
 * filters last left its field alone, so a collection filtered to one value
 * still offers the others for multi-select, plus any value the reader chose.
 */
function useFacetValues(
  field: string,
  counts: ViewExecutionStats["lifecycle"],
  executedFilters: Filter[],
  filters: Filter[],
) {
  const populated = (counts ?? []).flatMap((item) => (item.count > 0 ? [item.value] : []));
  const [kept, setKept] = useState(populated);
  const unfiltered = !executedFilters.some((filter) => filter.field === field);

  if (unfiltered && kept.join("\n") !== populated.join("\n")) setKept(populated);

  const chosen = filters.flatMap((filter) => (filter.field === field ? filterValues(filter) : []));

  return [...new Set([...kept, ...populated, ...chosen])];
}

/**
 * Adds a facet's filter, or takes it away when already applied. The field's
 * existing `eq` and `in` filters, such as a saved view's default, merge with
 * the value into one `in` filter, so facets of one field combine and every
 * pressed value can be released. The server requires every filter to match,
 * so the merge keeps only values all of them allow and never broadens results.
 */
export function toggleFilter(filters: Filter[], filter: Filter): Filter[] {
  const value = filter.values?.[0];

  if (filter.op === "in" && value !== undefined) {
    const isValueFilter = (item: Filter) =>
      item.field === filter.field && (item.op === "in" || item.op === "eq");

    const index = filters.findIndex(isValueFilter);

    if (index < 0) return [...filters, filter];

    const current = allowedValues(filters.filter(isValueFilter));
    const text = String(value);

    const values = current.includes(text)
      ? current.filter((item) => item !== text)
      : [...current, text];

    return filters.flatMap((item, other) =>
      other === index && values.length
        ? [{ field: filter.field, op: "in", values }]
        : isValueFilter(item)
          ? []
          : [item],
    );
  }

  return hasFilter(filters, filter)
    ? filters.filter((item) => !sameFilter(item, filter))
    : [...filters, filter];
}

function hasValue(filters: Filter[], field: string, value: string) {
  const valueFilters = filters.filter(
    (filter) => filter.field === field && (filter.op === "in" || filter.op === "eq"),
  );

  return valueFilters.length > 0 && allowedValues(valueFilters).includes(value);
}

/** Values every filter allows: the server ANDs filters on one field. */
function allowedValues(valueFilters: Filter[]) {
  const [first = [], ...rest] = valueFilters.map(filterValues);

  return [...new Set(first)].filter((value) => rest.every((values) => values.includes(value)));
}

function filterValues(filter: Filter) {
  return filter.op === "in"
    ? (filter.values ?? []).map(String)
    : filter.value === undefined
      ? []
      : [String(filter.value)];
}

function hasFilter(filters: Filter[], filter: Filter) {
  return filters.some((item) => sameFilter(item, filter));
}

function sameFilter(left: Filter, right: Filter | undefined) {
  return (
    right !== undefined &&
    left.field === right.field &&
    left.op === right.op &&
    String(left.value ?? "") === String(right.value ?? "") &&
    JSON.stringify(left.values ?? []) === JSON.stringify(right.values ?? [])
  );
}
