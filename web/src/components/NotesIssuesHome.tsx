import { useEffect, useMemo, useRef, useState } from "react";

import type {
  OntologyEditOp,
  ValidateEnvelope,
  ValidationDiagnostic,
  ValidationIssueGroup,
  ValidationScope,
} from "../api/types";
import { useValidationDiagnostics, type DiagnosticFilters } from "./useValidationDiagnostics";
import { useValidationRefresh } from "./useValidationRefresh";
import { useValidationRepair, type ValidationRepairController } from "./useValidationRepair";
import {
  useValidationIssueGroups,
  useValidationScopeSummaries,
  validationScopeKey,
} from "./useValidationScopeSummaries";
import {
  IssueKindList,
  kindRows,
  kindSelectionKey,
  type KindSelection,
} from "./validation/IssueKindList";
import {
  DiagnosticDetail,
  diagnosticPath,
  FileGroupSection,
  FindingsHeader,
  fileGroups,
  IssueRow,
  PathLabel,
} from "./validation/ProblemsBrowser";
import { availableChecks, ProblemsToolbar, type GroupMode } from "./validation/ProblemsToolbar";
import { RepairActionBar, RepairNotices } from "./validation/ProblemsRepairStatus";
import { TypeChoice } from "./validation/TypeChoice";
import { ValidationStatusStrip } from "./validation/ValidationStatusStrip";
import { ValidationRepairReviewPanel } from "./validation/ValidationRepairReviewPanel";
import { candidateTypes, issueLabel } from "./validation/issueLabels";

type Props = {
  validationEnvelope?: ValidateEnvelope | null;
  loading?: boolean;
  error?: Error | null;
  onOpenNote: (path: string) => void;
  pendingChangeCount?: number;
  onReviewChanges?: () => void;
  /** Stages edit-session operations, such as a type chosen for an ambiguous case. */
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void>;
  scope?: ValidationScope | null;
  repairController?: ValidationRepairController;
  selectedIssueKey?: string | null;
  onSelectIssue?: (issueKey: string | null) => void;
};

const EMPTY_ACTIONS = new Set<string>();

const EMPTY_FILTERS: DiagnosticFilters = { check: "", code: "", repairAvailability: "any" };

export function NotesIssuesHome(props: Props) {
  if (props.repairController) {
    return <NotesIssuesHomeContent {...props} repairController={props.repairController} />;
  }

  return <StandaloneNotesIssuesHome {...props} />;
}

function StandaloneNotesIssuesHome(props: Props) {
  const repairController = useValidationRepair(
    props.validationEnvelope?.snapshot?.generation ?? null,
    props.validationEnvelope?.snapshot?.repairPlanFingerprint,
  );

  return <NotesIssuesHomeContent {...props} repairController={repairController} />;
}

function NotesIssuesHomeContent({
  validationEnvelope,
  loading = false,
  error = null,
  onOpenNote,
  pendingChangeCount = 0,
  onReviewChanges,
  onStageOps,
  scope = null,
  repairController,
  selectedIssueKey = null,
  onSelectIssue,
}: Props & { repairController: ValidationRepairController }) {
  const envelope = useMemo(
    () =>
      validationEnvelope ??
      (error
        ? {
            status: "error" as const,
            health: "failed" as const,
            generation: 0,
            publishedGeneration: 0,
            error: error.message,
          }
        : null),
    [validationEnvelope, error],
  );

  const validationRefresh = useValidationRefresh();
  const refreshPending = Boolean(envelope?.refreshPending);
  const generation = envelope?.snapshot?.generation ?? null;
  const snapshotReady = Boolean(envelope?.snapshot);
  const [filters, setFilters] = useState<DiagnosticFilters>(EMPTY_FILTERS);
  const [textFilter, setTextFilter] = useState("");
  const [groupMode, setGroupMode] = useState<GroupMode>("kind");
  const [kindSelection, setKindSelection] = useState<KindSelection>(null);
  const [selectedKey, setSelectedKey] = useState<string | null>(selectedIssueKey);
  const [detailOnly, setDetailOnly] = useState(false);
  // The issue Next started from while the following page loads.
  const [pendingNext, setPendingNext] = useState<string | null>(null);
  const planFingerprint = envelope?.snapshot?.repairPlanFingerprint;

  const [actionSelection, setActionSelection] = useState({
    generation,
    planFingerprint,
    ids: new Set<string>(),
  });

  const selectedActions =
    actionSelection.generation === generation && actionSelection.planFingerprint === planFingerprint
      ? actionSelection.ids
      : EMPTY_ACTIONS;

  const [reviewedSelectionCount, setReviewedSelectionCount] = useState(0);
  const requestedActionCount = useRef(0);

  const filtersActive = Boolean(
    filters.check || filters.code || filters.repairAvailability !== "any" || textFilter.trim(),
  );

  const activeScope = useMemo(() => scope ?? { kind: "global" as const }, [scope]);

  const filter = useMemo(
    () => ({
      check: filters.check || undefined,
      code: filters.code || undefined,
      text: textFilter || undefined,
      repairAvailability:
        filters.repairAvailability === "any" ? undefined : filters.repairAvailability,
    }),
    [filters, textFilter],
  );

  const baselineSummary = useValidationScopeSummaries(
    generation,
    scope?.kind !== undefined && scope.kind !== "global" ? [scope] : [],
  );

  const scopedSummary = useValidationScopeSummaries(
    generation,
    filtersActive ? [activeScope] : [],
    filter,
  );

  const scopeCounts = scopedSummary.summaries.get(validationScopeKey(activeScope));
  const baselineCounts = baselineSummary.summaries.get(validationScopeKey(activeScope));
  const baselineIssueCount = baselineCounts?.issueCount ?? envelope?.snapshot?.issueCount ?? 0;
  const hasAnyIssues = baselineIssueCount > 0;
  const issueGroups = useValidationIssueGroups(generation, activeScope, filter, hasAnyIssues);

  const totalIssues = filtersActive
    ? (scopeCounts?.issueCount ?? sumIssues(issueGroups.data?.groups))
    : baselineIssueCount;

  const affectedFiles =
    (filtersActive ? scopeCounts : baselineCounts)?.affectedFileCount ??
    envelope?.snapshot?.affectedFileCount ??
    0;

  const kinds = useMemo(
    () => kindRows(issueGroups.data?.groups ?? [], totalIssues),
    [issueGroups.data, totalIssues],
  );

  // A kind the current filters no longer contain falls back to every issue.
  const selectedRow = kinds.find((row) => row.key === kindSelectionKey(kindSelection));

  const selection =
    selectedRow || !issueGroups.data || issueGroups.isPlaceholderData ? kindSelection : null;

  const serverFilters = useMemo(() => {
    const next: DiagnosticFilters = {
      ...filters,
      text: textFilter,
      scopeKind: scope?.kind === "global" ? undefined : scope?.kind,
      scopeKey: scope?.key,
      sort: groupMode === "file" ? "file" : "diagnostic_order",
    };

    if (selection) {
      next.check = selection.check;
      next.code = selection.code ?? "";
      next.variant = selection.variant;
    }

    return next;
  }, [filters, groupMode, scope?.key, scope?.kind, selection, textFilter]);

  const query = useValidationDiagnostics(generation, serverFilters, snapshotReady);
  const actions = useMemo(() => envelope?.snapshot?.actions ?? [], [envelope]);
  const actionMap = useMemo(() => new Map(actions.map((action) => [action.id, action])), [actions]);

  const safeActionIds = useMemo(
    () => actions.filter((action) => action.safety === "safe").map((action) => action.id),
    [actions],
  );

  const files = useMemo(
    () => (groupMode === "file" ? fileGroups(query.diagnostics, query.fileTotals) : []),
    [groupMode, query.diagnostics, query.fileTotals],
  );

  // Visual order, so list keys and Previous/Next agree with what the user sees.
  const diagnostics = useMemo(
    () => (groupMode === "file" ? files.flatMap((group) => group.diagnostics) : query.diagnostics),
    [files, groupMode, query.diagnostics],
  );

  const fixableActionIds = useMemo(
    () => [
      ...new Set(
        diagnostics.flatMap((item) =>
          (item.actionIds ?? []).filter((id) => {
            const safety = actionMap.get(id)?.safety;

            return safety !== undefined && safety !== "agent_required" && !selectedActions.has(id);
          }),
        ),
      ),
    ],
    [actionMap, diagnostics, selectedActions],
  );

  const selectedInResults = diagnostics.find((item) => item.issueKey === selectedKey) ?? null;

  const retainedSelection = useRef<{
    generation: number | null;
    diagnostic: ValidationDiagnostic;
    position: number;
    total: number;
  } | null>(null);

  if (selectedInResults) {
    retainedSelection.current = {
      generation,
      diagnostic: selectedInResults,
      position: diagnostics.indexOf(selectedInResults),
      total: diagnostics.length,
    };
  }

  const awaitingReplacement =
    query.data === undefined && retainedSelection.current?.generation !== generation;

  const selected =
    selectedInResults ??
    (awaitingReplacement && retainedSelection.current?.diagnostic.issueKey === selectedKey
      ? retainedSelection.current.diagnostic
      : null);

  const repair = repairController;
  const firstFinding = query.diagnostics[0];
  const ambiguousTypes = selection?.variant && firstFinding ? candidateTypes(firstFinding) : [];

  useEffect(() => {
    if (query.data === undefined) return;

    if (selectedKey && diagnostics.some((item) => item.issueKey === selectedKey)) return;
    setSelectedKey(diagnostics[0]?.issueKey ?? null);

    if (diagnostics.length === 0) setDetailOnly(false);
  }, [diagnostics, query.data, selectedKey]);

  useEffect(() => onSelectIssue?.(selectedKey), [onSelectIssue, selectedKey]);

  useEffect(() => {
    if (pendingNext === null || query.isFetchingNextPage) return;

    // Pages extend the list in server order, so the next issue follows the current one.
    const next = diagnostics[diagnostics.findIndex((item) => item.issueKey === pendingNext) + 1];

    if (next) setSelectedKey(next.issueKey);
    setPendingNext(null);
  }, [diagnostics, pendingNext, query.isFetchingNextPage]);

  useEffect(() => {
    const known = new Set(actions.map((action) => action.id));
    setActionSelection((current) => {
      if (current.generation !== generation || current.planFingerprint !== planFingerprint) {
        return { generation, planFingerprint, ids: new Set<string>() };
      }

      const next = new Set([...current.ids].filter((id) => known.has(id)));

      return next.size === current.ids.size ? current : { ...current, ids: next };
    });
  }, [actions, generation, planFingerprint]);

  /** Stage `actionIds` for review; a connected-transaction expansion keeps the requested count. */
  const stageActions = async (actionIds: string[]) => {
    try {
      const review = await repair.stage(actionIds);

      if (!review) return;
      setReviewedSelectionCount(requestedActionCount.current);
      setActionSelection({ generation, planFingerprint, ids: new Set<string>() });
    } catch {
      // The hook retains the typed failure and the selection remains available for retry.
    }
  };

  const requestStage = (actionIds: string[]) => {
    requestedActionCount.current = actionIds.length;
    void stageActions(actionIds);
  };

  if (repair.review) {
    return (
      <ValidationRepairReviewPanel
        review={repair.review}
        requestedActionCount={reviewedSelectionCount}
        applying={repair.busy === "apply"}
        restoring={repair.busy === "restore"}
        onRetryRecovery={repair.recoveryFailed ? repair.retryRecovery : undefined}
        error={repair.error}
        result={repair.result}
        onApply={(confirmations) => void repair.apply(confirmations).catch(() => undefined)}
        onClose={repair.close}
        onReviewDrafts={onReviewChanges}
      />
    );
  }

  const clearFilters = () => {
    setFilters(EMPTY_FILTERS);
    setTextFilter("");
  };

  const selectRow = (issueKey: string) => {
    setSelectedKey(issueKey);
    window.requestAnimationFrame(() =>
      document
        .querySelector<HTMLButtonElement>(`[data-issue-key="${CSS.escape(issueKey)}"]`)
        ?.focus(),
    );
  };

  const selectedIndex = selected ? diagnostics.indexOf(selected) : -1;

  const goNext = () => {
    if (selectedIndex < 0) return;

    if (selectedIndex < diagnostics.length - 1) {
      setSelectedKey(diagnostics[selectedIndex + 1].issueKey);
    } else if (query.hasNextPage && selected) {
      const from = selected.issueKey;
      void query.fetchNextPage().then(() => setPendingNext(from));
    }
  };

  const rowProps = {
    selectedKey,
    tabbableKey:
      selected && diagnostics.includes(selected) ? selected.issueKey : diagnostics[0]?.issueKey,
    onSelect: (diagnostic: ValidationDiagnostic) => {
      setSelectedKey(diagnostic.issueKey);
      setDetailOnly(true);
    },
  };

  return (
    <section className="problems-workspace" aria-label="Problems">
      <header className="problems-workspace__header">
        <div>
          <p className="problems-workspace__meta">
            validation
            {scope?.kind !== "global" && scope?.key && (
              <>
                {" · "}
                <span className="problems-workspace__scope">
                  {scope.kind} · {scope.key}
                </span>
              </>
            )}
          </p>
          <h2>
            {hasAnyIssues
              ? `${totalIssues} ${totalIssues === 1 ? "issue" : "issues"} in ${affectedFiles} ${affectedFiles === 1 ? "file" : "files"}`
              : "Problems"}
          </h2>
        </div>
        {pendingChangeCount > 0 && onReviewChanges && (
          <button type="button" className="problems-workspace__review" onClick={onReviewChanges}>
            Review {pendingChangeCount} staged {pendingChangeCount === 1 ? "change" : "changes"}
          </button>
        )}
      </header>

      <ValidationStatusStrip
        envelope={envelope}
        busy={loading || query.isFetching || validationRefresh.busy || refreshPending}
        onRefresh={validationRefresh.refresh}
        refreshing={validationRefresh.busy || refreshPending || envelope?.status === "running"}
        refreshError={validationRefresh.error}
      />

      {hasAnyIssues && (
        <ProblemsToolbar
          groupMode={groupMode}
          onGroupMode={setGroupMode}
          filters={filters}
          onFilters={(update) => {
            const next = update(filters);

            // A new check or kind filter supersedes the selected case.
            if (next.check !== filters.check || next.code !== filters.code) setKindSelection(null);
            setFilters(next);
          }}
          textFilter={textFilter}
          onTextFilter={setTextFilter}
          checks={availableChecks(envelope, query.diagnostics)}
          issueCodes={envelope?.snapshot?.issueCodes ?? []}
          onClear={filtersActive && diagnostics.length > 0 ? clearFilters : undefined}
          safeFixCount={planFingerprint ? safeActionIds.length : 0}
          staging={repair.busy === "stage"}
          onStageSafeFixes={() => requestStage(safeActionIds)}
        />
      )}

      {query.isError && envelope?.snapshot && (
        <div className="problems-workspace__notice problems-workspace__notice--error" role="alert">
          Could not load issue details. {query.error.message}
        </div>
      )}
      <RepairNotices
        repair={repair}
        onIncludeConnected={(ids) => void stageActions(ids)}
        onReviewChanges={onReviewChanges}
      />

      {hasAnyIssues && (
        <div className={`problems-browser${detailOnly ? " is-detail" : ""}`}>
          <IssueKindList
            rows={kinds}
            selectedKey={selectedRow?.key ?? "all"}
            onSelect={(row) => setKindSelection(row.selection)}
          />
          <div className="problems-browser__list">
            {(selectedRow || !selection) && (
              <FindingsHeader
                row={selectedRow ?? kinds[0]}
                fixableActionIds={fixableActionIds}
                onSelectActions={(ids) =>
                  setActionSelection({
                    generation,
                    planFingerprint,
                    ids: new Set([...selectedActions, ...ids]),
                  })
                }
              >
                {selectedRow &&
                  !issueGroups.isPlaceholderData &&
                  ambiguousTypes.length > 0 &&
                  onStageOps &&
                  generation !== null && (
                    <TypeChoice
                      key={selectedRow.key}
                      generation={generation}
                      filters={serverFilters}
                      types={ambiguousTypes}
                      noteCount={selectedRow.count}
                      onStageOps={onStageOps}
                      onReviewChanges={onReviewChanges}
                    />
                  )}
              </FindingsHeader>
            )}
            <div
              className="problems-findings"
              role="group"
              aria-label="Issue list"
              onKeyDown={(event) => {
                if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;

                const rows = [
                  ...event.currentTarget.querySelectorAll<HTMLButtonElement>("[data-issue-key]"),
                ];

                const current = rows.findIndex((row) => row === document.activeElement);

                if (current < 0) return;
                event.preventDefault();

                const next = rows[current + (event.key === "ArrowDown" ? 1 : -1)];

                if (next?.dataset.issueKey) selectRow(next.dataset.issueKey);
              }}
            >
              {groupMode === "file"
                ? files.map((group) => (
                    <FileGroupSection key={group.key} group={group} {...rowProps} />
                  ))
                : diagnostics.map((diagnostic) => (
                    <IssueRow key={diagnostic.issueKey} diagnostic={diagnostic} {...rowProps}>
                      <PathLabel path={diagnosticPath(diagnostic)} />
                      {!selection && (
                        <span className="problems-row__kind">{issueLabel(diagnostic.code)}</span>
                      )}
                    </IssueRow>
                  ))}
              {query.hasNextPage && (
                <button
                  type="button"
                  className="problems-browser__more"
                  disabled={query.isFetchingNextPage}
                  onClick={() => void query.fetchNextPage()}
                >
                  {query.isFetchingNextPage
                    ? "Loading…"
                    : `Load more · ${query.total - query.diagnostics.length} remaining`}
                </button>
              )}
            </div>
            {query.isLoading && !selected && (
              <p className="problems-workspace__loading">Loading issues…</p>
            )}
            {diagnostics.length === 0 && !query.isLoading && !query.isError && (
              <div className="problems-filter-empty">
                <strong>No issues match these filters.</strong>
                <span>
                  {baselineIssueCount} {baselineIssueCount === 1 ? "issue exists" : "issues exist"}{" "}
                  in the current validation result.
                </span>
                {filtersActive && (
                  <button type="button" onClick={clearFilters}>
                    Clear filters
                  </button>
                )}
              </div>
            )}
          </div>
          <div className="problems-browser__detail">
            {selected && (
              <DiagnosticDetail
                diagnostic={selected}
                actions={
                  awaitingReplacement
                    ? []
                    : (selected.actionIds?.flatMap((id) => actionMap.get(id) ?? []) ?? [])
                }
                actionsPending={awaitingReplacement}
                selectedActions={selectedActions}
                onToggleAction={(id) =>
                  setActionSelection({
                    generation,
                    planFingerprint,
                    ids: toggleSet(selectedActions, id),
                  })
                }
                onOpenNote={onOpenNote}
                onBack={() => {
                  setDetailOnly(false);
                  selectRow(selected.issueKey);
                }}
                position={
                  selectedIndex >= 0 ? selectedIndex : (retainedSelection.current?.position ?? 0)
                }
                total={query.total || retainedSelection.current?.total || 1}
                onPrevious={() => {
                  if (selectedIndex > 0) setSelectedKey(diagnostics[selectedIndex - 1].issueKey);
                }}
                onNext={goNext}
              />
            )}
          </div>
        </div>
      )}

      <RepairActionBar
        selectedCount={selectedActions.size}
        hasActions={actions.length > 0}
        canReview={Boolean(planFingerprint)}
        staging={repair.busy === "stage"}
        onClear={() => setActionSelection({ generation, planFingerprint, ids: new Set() })}
        onStage={() => requestStage([...selectedActions])}
      />
    </section>
  );
}

function sumIssues(groups: ValidationIssueGroup[] = []) {
  return groups.reduce((sum, group) => sum + group.issueCount, 0);
}

function toggleSet(current: Set<string>, key: string) {
  const next = new Set(current);

  if (next.has(key)) next.delete(key);
  else next.add(key);

  return next;
}
