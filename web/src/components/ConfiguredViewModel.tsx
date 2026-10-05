import { isJsonObject, isString } from "../api/parse";
import { enumLabel } from "../lib/labels";
import type {
  OntologyEditSessionResponse,
  ViewCatalogEntry,
  ViewExecuteRequest,
  ViewExecuteResponse,
  ViewFieldCapability,
  ViewTableColumn,
  ViewTableRow,
} from "../api/types";
import { configuredTableRowKey } from "./ConfiguredTableCell";
import { humanizeLabel, stagedTargetForRow } from "./ConfiguredTableCellHelpers";
import { changedFields } from "../staging/stagedState";
import { StatusMark } from "./StatusMark";
import { viewFieldControl } from "./viewFieldControls";

export type ConfiguredViewVariant = "table" | "kanban" | "card";

type ViewFilter = NonNullable<ViewExecuteRequest["filters"]>[number];

export type FilterPreset = {
  id: string;
  label: string;
  filters: ViewFilter[];
};

const VARIANTS: ConfiguredViewVariant[] = ["table", "kanban", "card"];

export function availableViewVariants(view: ViewCatalogEntry) {
  return (view.availableVariants ?? []).flatMap((variant) =>
    isConfiguredViewVariant(variant) ? [variant] : [],
  );
}

export function activeViewVariant(
  view: ViewCatalogEntry,
  requested: string | undefined,
  executed: string | undefined,
): ConfiguredViewVariant {
  const available = availableViewVariants(view);

  for (const candidate of [requested, executed, view.defaults.variant]) {
    if (candidate && isConfiguredViewVariant(candidate) && available.includes(candidate)) {
      return candidate;
    }
  }

  return available[0] ?? "table";
}

export function isConfiguredViewVariant(value: string): value is ConfiguredViewVariant {
  return VARIANTS.some((variant) => variant === value);
}

export function filterPresets(view: ViewCatalogEntry): FilterPreset[] {
  const raw = view.definition.filterPresets;

  if (!Array.isArray(raw)) return [];

  return raw.flatMap((item) => {
    if (!isJsonObject(item) || !isString(item.id) || !item.id) return [];

    return [
      {
        id: item.id,
        label: isString(item.label) && item.label ? item.label : item.id,
        filters: Array.isArray(item.filters)
          ? item.filters.flatMap((filter) => (isViewFilter(filter) ? [filter] : []))
          : [],
      },
    ];
  });
}

function isViewFilter(value: unknown): value is ViewFilter {
  return isJsonObject(value) && isString(value.field) && isString(value.op);
}

export function configuredColumns(
  view: ViewCatalogEntry,
  execution: ViewExecuteResponse | null,
  issueCounts: ReadonlyMap<string, number> | undefined,
): ViewTableColumn[] {
  const columns = execution?.columns ?? view.variants.table?.columns ?? [];

  // A configured hasIssues column already shows the validation count.
  return issueCounts && !columns.some((column) => isIssueColumn(column.field, issueCounts))
    ? [...columns, { field: "validationIssues", label: "Issues" }]
    : columns;
}

/**
 * The server's hasIssues flag is an index-time signal that can disagree with
 * the validation snapshot, so once validation counts are known the column
 * shows those counts instead.
 */
export function isIssueColumn(field: string, issueCounts: ReadonlyMap<string, number> | undefined) {
  return field === "validationIssues" || (field === "hasIssues" && issueCounts !== undefined);
}

/** A group or board column label; boolean values read as "Done: yes", not "true". */
export function groupValueLabel(
  capability: ViewFieldCapability | undefined,
  value: string,
  label?: string,
) {
  if (capability && viewFieldControl(capability).kind === "boolean") {
    if (value === "true") return `${capabilityLabel(capability)}: yes`;

    if (value === "false") return `${capabilityLabel(capability)}: no`;
  }

  // Enum values without a schema label read like the rest of the UI ("active" → "Active").
  if (value && capability && viewFieldControl(capability).kind === "enum")
    return enumLabel(value, label);

  return label || value || "Empty";
}

/** A group's name; a link or month group names a note or a period, not a status, so it has no status marker. */
export function GroupValueMark({
  capability,
  tone,
  label,
}: {
  capability: ViewFieldCapability | undefined;
  tone: string | undefined;
  label: string;
}) {
  if (capability && ["relation", "date", "datetime"].includes(viewFieldControl(capability).kind))
    return <span className="configured-view__group-label">{label}</span>;

  return <StatusMark tone={tone} label={label} />;
}

export function availableViewColumns(
  columns: ViewTableColumn[],
  capabilities: ViewFieldCapability[],
): ViewTableColumn[] {
  const out = [...columns];
  const seen = new Set(columns.map((column) => canonicalControlKey(column.field)));

  for (const capability of dedupeCapabilitiesForControls(capabilities)) {
    const canonical = canonicalControlKey(capability.key);

    if (!canonical || seen.has(canonical)) continue;
    seen.add(canonical);
    out.push({ field: capability.key, label: capability.label });
  }

  return out;
}

export function dedupeCapabilitiesForControls(capabilities: ViewFieldCapability[]) {
  const byCanonical = new Map<string, ViewFieldCapability>();

  for (const capability of capabilities) {
    if (!capability.key || capability.key.startsWith("inline.")) continue;

    const canonical = canonicalControlKey(capability.key);
    const current = byCanonical.get(canonical);

    if (!current || capabilityControlRank(capability) > capabilityControlRank(current)) {
      byCanonical.set(canonical, capability);
    }
  }

  return [...byCanonical.values()].sort((left, right) =>
    capabilityLabel(left).localeCompare(capabilityLabel(right)),
  );
}

export function stagedChangeCount(
  rows: ViewTableRow[],
  editSession: OntologyEditSessionResponse | null | undefined,
) {
  // A row grouped by a list field repeats once per value it holds.
  const seen = new Set<string>();

  return rows.reduce((count, row) => {
    const key = configuredTableRowKey(row);

    if (seen.has(key)) return count;
    seen.add(key);

    return count + changedFields(editSession, stagedTargetForRow(row)).size;
  }, 0);
}

export function capabilityFor(capabilities: ViewFieldCapability[], field: string) {
  return capabilities.find((capability) => capability.key === field);
}

/** A schema field's capability, by key or by the canonical field it reads. */
export function fieldCapability(capabilities: ViewFieldCapability[], field: string | undefined) {
  if (!field) return undefined;

  return (
    capabilityFor(capabilities, field) ??
    capabilities.find((capability) => capability.canonicalField === field)
  );
}

export function capabilityLabel(capability: ViewFieldCapability) {
  return humanizeLabel(capability.label || capability.key);
}

export function columnLabel(column: ViewTableColumn, capabilities: ViewFieldCapability[]) {
  return column.label || capabilityFor(capabilities, column.field)?.label || column.field;
}

function canonicalControlKey(field: string) {
  return field
    .replace(/^(frontmatter|inline)\./, "")
    .replace(/[^a-zA-Z0-9]/g, "")
    .toLowerCase();
}

function capabilityControlRank(capability: ViewFieldCapability) {
  if (!capability.key.includes(".")) return 100;

  if (capability.source === "builtin") return 80;

  if (capability.key.startsWith("frontmatter.")) return 60;

  return 50;
}
