import { useCallback, useId, useMemo, useState } from "react";

import { isString } from "../api/parse";
import type {
  OntologyEditOp,
  OntologyEditSessionResponse,
  ValidationScope,
  ViewCatalogEntry,
  ViewExecuteRequest,
  ViewExecuteResponse,
  ViewSaveResponse,
  ViewTableColumn,
  ViewTableRow,
} from "../api/types";
import type { ViewContext } from "../views/context";
import { nativePreferenceScope } from "../viewPreferences/native";
import type { OpenMode } from "./useNoteTabs";
import { rowsWithStagedValues } from "./ConfiguredTableCellHelpers";
import { ConfiguredViewBoard } from "./ConfiguredViewBoard";
import { ConfiguredViewCards } from "./ConfiguredViewCards";
import { ConfiguredViewFacets } from "./ConfiguredViewFacets";
import {
  activeViewVariant,
  availableViewColumns,
  configuredColumns,
  filterPresets,
  stagedChangeCount,
} from "./ConfiguredViewModel";
import { ConfiguredViewTable } from "./ConfiguredViewTable";
import { DiagnosticRegion } from "../diagnostics/DiagnosticRegion";
import { SaveViewAction, saveViewProps } from "./ConfiguredViewSave";
import { ConfiguredViewFooter, ViewErrorStatus } from "./ConfiguredViewStatus";
import { effectiveSort, nextSort } from "./ConfiguredViewTableModel";
import { ConfiguredViewToolbar } from "./ConfiguredViewToolbar";
import { useTypeLabel, viewDisplayName } from "./typeLabels";
import { growingWindow } from "./useScrollLoading";
import { useViewLayout } from "./useViewLayout";
import { ViewPreferencesStatus } from "./ViewPreferencesStatus";

const EMPTY_CAPABILITIES: NonNullable<ViewExecuteResponse["capabilities"]> = [];

const EMPTY_ROWS: ViewTableRow[] = [];

export type ViewErrorInfo = {
  message: string;
  status?: number;
  code?: string;
  details?: unknown;
};

export type ConfiguredViewProps = {
  view: ViewCatalogEntry;
  execution: ViewExecuteResponse | null;
  loading: boolean;
  error: string | ViewErrorInfo | null;
  state: ViewExecuteRequest;
  onStateChange: (state: ViewExecuteRequest) => void;
  onRefresh: () => void;
  onOpenRow: (row: ViewTableRow, mode?: OpenMode) => void;
  editSession?: OntologyEditSessionResponse | null;
  vaultKey?: string | null;
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | undefined;
  embedded?: boolean;
  issueCounts?: ReadonlyMap<string, number>;
  onOpenIssues?: (scope: ValidationScope) => void;
  /** Staged edits the server rows do not reflect yet, even when nothing is loading. */
  stagedEditsPending?: boolean;
  /** Only the active view shows its selected record in the workspace rail. */
  active?: boolean;
  /** Called after Save writes the view; a generated view comes back under a new id. */
  onViewSaved?: (response: ViewSaveResponse) => Promise<void> | void;
  /** The invocation whose personal preferences this view remembers; none keeps them per mount. */
  context?: ViewContext;
  /** Separates intentionally independent mounts of one view in one context. */
  preferenceSlot?: string;
};

export function ConfiguredView({
  view,
  execution,
  loading,
  error,
  state,
  onStateChange,
  onRefresh,
  onOpenRow,
  editSession,
  vaultKey,
  onStageOps,
  embedded = false,
  issueCounts,
  onOpenIssues,
  stagedEditsPending = false,
  active = true,
  onViewSaved,
  context,
  preferenceSlot,
}: ConfiguredViewProps) {
  const preferenceScope = nativePreferenceScope(view, context, preferenceSlot);

  const defaultColumns = useMemo(
    () => configuredColumns(view, execution, issueCounts),
    [execution, issueCounts, view],
  );

  const capabilities = execution?.capabilities ?? EMPTY_CAPABILITIES;

  const columns = useMemo(
    () => availableViewColumns(defaultColumns, capabilities),
    [capabilities, defaultColumns],
  );

  const layout = useViewLayout(
    view,
    vaultKey ?? null,
    execution,
    defaultColumns,
    columns,
    state.filters,
    context,
    preferenceSlot,
  );

  const { visibleFields, emptyFields, density } = layout;

  const [collapsedGroupCount, setCollapsedGroupCount] = useState(0);
  const [saveActive, setSaveActive] = useState(false);
  const titleID = useId();
  const typeLabel = useTypeLabel();
  const title = viewDisplayName(view, typeLabel);
  const activeVariant = activeViewVariant(view, state.variant, execution?.variant);
  const presets = filterPresets(view);
  const viewError = normalizeViewError(error);

  const executionWarnings =
    execution?.warnings
      ?.map((warning) => warning.message)
      .filter(Boolean)
      .join(" ") ?? "";

  const profile = execution?.profile;
  const stats = execution?.stats;
  const groupField = execution?.state.group?.field;
  const summaryField = profile?.summaryField;

  const updateState = useCallback(
    (patch: ViewExecuteRequest) => {
      onStateChange({ ...state, ...patch, page: { ...state.page, ...patch.page } });
    },
    [onStateChange, state],
  );

  const resetPage = useCallback(
    (patch: ViewExecuteRequest) => {
      // ponytail: keeps a scroll-grown page size; reset it here if refetching large windows hurts.
      updateState({ ...patch, page: { ...state.page, offset: 0 } });
    },
    [state.page, updateState],
  );

  // Server rows already reflect acknowledged edits; bridge only the gap until they do.
  const showStagedEdits = loading || stagedEditsPending;

  const stagedRows = useMemo(() => {
    const rows = execution?.rows ?? EMPTY_ROWS;

    return showStagedEdits ? rowsWithStagedValues(rows, capabilities, editSession) : rows;
  }, [capabilities, editSession, execution?.rows, showStagedEdits]);

  const rendererExecution = useMemo(() => {
    if (!execution || stagedRows === execution.rows) return execution;

    return { ...execution, rows: stagedRows };
  }, [execution, stagedRows]);

  const visibleColumns = columns.filter((column) => visibleFields.includes(column.field));
  const stagedCount = stagedChangeCount(execution?.rows ?? [], editSession);
  const pageInfo = execution?.pageInfo;
  const first = pageInfo?.first ?? state.page?.first ?? 25;
  const offset = pageInfo?.offset ?? state.page?.offset ?? 0;
  const paged = offset > 0 || Boolean(pageInfo?.hasMore);
  const scrolls = activeVariant === "table";

  const { loadMore, loadingMore } = growingWindow(scrolls, state, pageInfo, loading, (page) =>
    updateState({ page }),
  );

  const rendererProps = rendererExecution
    ? {
        execution: rendererExecution,
        rows: stagedRows,
        columns: visibleColumns,
        capabilities,
        state,
        viewID: view.id,
        loading: loading && !loadingMore,
        showStagedEdits,
        editSession,
        vaultKey,
        onStageOps,
        onOpenRow,
        issueCounts,
        onOpenIssues,
        onSort: (column: ViewTableColumn, additive = false) =>
          resetPage({
            sort: nextSort(effectiveSort(state, rendererExecution.state), column.field, additive),
          }),
        onCollapsedGroupCountChange: setCollapsedGroupCount,
        profile,
        density,
        onLoadMore: loadMore,
        columnWidths: layout.columnWidths ?? undefined,
        onColumnWidthsChange: layout.setColumnWidths,
        active,
        preferenceScope,
      }
    : null;

  const defaultFields = defaultColumns
    .map((column) => column.field)
    .filter((field) => field !== "validationIssues");

  const save = saveViewProps({
    view,
    execution,
    state,
    variant: activeVariant,
    densityChoice: layout.densityChoice,
    columns,
    defaultFields,
    chosenFields: layout.chosenFields,
    vaultKey: vaultKey ?? null,
    preferenceScope,
    onViewSaved,
  });

  const preferencesStatus = (
    <ViewPreferencesStatus
      scope={preferenceScope}
      vaultKey={vaultKey}
      validationError={layout.preferences.error}
    />
  );

  const viewActions = (
    <>
      <SaveViewAction {...save} onActiveChange={setSaveActive} />
      <button
        type="button"
        className="configured-view__refresh"
        aria-label="Refresh view"
        title="Refresh view"
        onClick={onRefresh}
        disabled={loading}
      >
        ↻
      </button>
    </>
  );

  return (
    <section
      className={`configured-view${embedded ? " configured-view--embedded" : ""}`}
      aria-labelledby={embedded ? undefined : titleID}
      aria-label={embedded ? `${title} view` : undefined}
      aria-busy={loading}
    >
      {/* The type page already names the type, so an embedded view keeps its
          layout controls in the toolbar instead of a header of its own. */}
      {!embedded && (
        <header className="configured-view__header">
          <div className="configured-view__identity">
            <p className="configured-view__eyebrow">
              {[
                // A generated view is titled by its type already.
                view.generated ? "" : typeLabel(view.source.type || view.source.interface),
                pageInfo ? rowCountLabel(pageInfo.total) : "",
              ]
                .filter(Boolean)
                .join(" · ")}
            </p>
            <h2 id={titleID}>{title}</h2>
          </div>
          <div className="configured-view__header-actions">{preferencesStatus}</div>
        </header>
      )}

      {stats && (
        <ConfiguredViewFacets
          // Remembered facet values belong to one view's collection.
          key={view.id}
          stats={stats}
          profile={profile}
          capabilities={capabilities}
          pluralLabel={typeLabel(view.source.type || view.source.interface || view.name, true)}
          filters={state.filters ?? []}
          executedFilters={execution?.state.filters}
          onFiltersChange={(filters) => resetPage({ filters })}
        />
      )}

      {presets.length > 0 && (
        <div className="configured-view__presets" aria-label="Filter presets">
          {presets.map((preset) => (
            <button
              key={preset.id}
              type="button"
              aria-pressed={state.filterPreset === preset.id}
              onClick={() =>
                resetPage({ filterPreset: state.filterPreset === preset.id ? "" : preset.id })
              }
            >
              {preset.label}
            </button>
          ))}
        </div>
      )}

      <ConfiguredViewToolbar
        state={state}
        capabilities={capabilities}
        columns={columns}
        visibleFields={visibleFields}
        activeVariant={activeVariant}
        boardColumnField={execution?.board?.columnField}
        boardLaneField={execution?.board?.laneField}
        profile={execution?.profile}
        // A saved "no grouping" reads as Group: None.
        executedGroupField={groupField === "none" ? "" : groupField}
        onToggleColumn={layout.toggleColumn}
        emptyFields={emptyFields}
        density={summaryField ? density : undefined}
        onDensityChange={layout.setDensity}
        onResetPage={resetPage}
        viewActions={viewActions}
        optionsDirty={save.dirty}
        optionsPinned={saveActive}
        trailing={embedded ? preferencesStatus : undefined}
      />

      {executionWarnings && (
        <div className="configured-view__status" role="status">
          {executionWarnings}
        </div>
      )}

      {loading && !execution && (
        <div className="configured-view__status" role="status">
          Loading view…
        </div>
      )}
      {viewError && <ViewErrorStatus error={viewError} />}
      {!viewError && execution && stagedRows.length === 0 && activeVariant !== "kanban" && (
        <div className="configured-view__status" role="status">
          No rows match this view.
          {state.search || state.filters?.length ? (
            <button
              type="button"
              className="configured-view__status-action"
              onClick={() => resetPage({ search: "", filters: [] })}
            >
              Clear search and filters
            </button>
          ) : null}
        </div>
      )}
      {!viewError &&
        rendererProps &&
        (stagedRows.length > 0 || activeVariant === "kanban") &&
        (activeVariant === "kanban" ? (
          <ConfiguredViewBoard {...rendererProps} />
        ) : activeVariant === "card" ? (
          <ConfiguredViewCards {...rendererProps} />
        ) : (
          <DiagnosticRegion name="view-table">
            <ConfiguredViewTable {...rendererProps} />
          </DiagnosticRegion>
        ))}

      {execution && (
        <ConfiguredViewFooter
          summary={
            paged && pageInfo
              ? `${offset + 1}–${Math.min(offset + first, pageInfo.total)} of ${rowCountLabel(pageInfo.total)}`
              : rowCountLabel(pageInfo?.total ?? execution.rows.length)
          }
          loadingMore={loadingMore}
          collapsed={
            collapsedGroupCount > 0
              ? `${collapsedGroupCount} ${activeVariant === "kanban" ? "columns" : "groups"} collapsed`
              : ""
          }
          stagedCount={stagedCount}
          pager={
            paged && !scrolls
              ? { offset, first, hasMore: pageInfo ? pageInfo.hasMore : true }
              : null
          }
          onPage={(page) => updateState({ page })}
        />
      )}
    </section>
  );
}

function rowCountLabel(count: number) {
  return `${count} ${count === 1 ? "row" : "rows"}`;
}

function normalizeViewError(error: string | ViewErrorInfo | null): ViewErrorInfo | null {
  if (!error) return null;

  return isString(error) ? { message: error } : error;
}
