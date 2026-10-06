import { isJsonObject } from "../api/parse";
import { useEffect, useMemo } from "react";
import type {
  OntologySummaryResponse,
  OntologyEditOp,
  OntologyEditSessionResponse,
  ValidationScope,
  ViewChoice,
  ViewTarget,
} from "../api/types";
import { changeSummary } from "../staging/stagedState";
import type { EditReadLifecycle } from "../staging/stagedQuery";
import { BuiltinOverview } from "../views/BuiltinOverview";
import { ViewHost, type ViewServices } from "../views/ViewHost";
import { ViewSelector } from "../views/ViewSelector";
import { useViewSelection } from "../views/useViewSelection";
import type { ViewContext } from "../views/context";
import type { OpenMode } from "./useNoteTabs";
import { NotesIssuesHome } from "./NotesIssuesHome";
import { NotesModifiedHome } from "./NotesModifiedHome";
import {
  isPseudoType,
  type NotesLocation,
  PSEUDO_TYPE_ISSUES,
  PSEUDO_TYPE_MODIFIED,
} from "./notesRoute";
import { TypeWorkspaceHeader } from "./TypeWorkspaceHeader";
import {
  useOntologySummaryQuery,
  useOntologyTypeQuery,
  useValidationQuery,
  useViewCatalogQuery,
} from "./useNotesQueries";
import { useValidationRepair } from "./useValidationRepair";
import { useValidationScopeSummaries, validationScopeKey } from "./useValidationScopeSummaries";

type Props = {
  active?: boolean;
  summary: OntologySummaryResponse | null;
  selection: NotesLocation["selection"];
  selectedType: string | null;
  location: NotesLocation;
  editSession: {
    session: OntologyEditSessionResponse | null;
    readLifecycle?: EditReadLifecycle;
    replaceOps: (ops: OntologyEditOp[]) => Promise<void>;
    vaultKey?: string | null;
    busy?: boolean;
    unacknowledged?: boolean;
  };
  onOpenNote: (path: string, mode?: OpenMode) => void;
  onSelectCollection: (typeName: string) => void;
  onOpenIssues?: (scope?: ValidationScope) => void;
  onSelectIssue?: (issueKey: string | null) => void;
  onStageOps: (ops: OntologyEditOp[]) => Promise<void>;
  onPresentation?: (id: string | null) => void;
  onOpenNode?: ViewServices["onOpenNode"];
  onOpenView?: ViewServices["onOpenView"];
  onOpenSearch?: ViewServices["onOpenSearch"];
  /** Reports whether a type or interface collection view (not Overview) is showing. */
  onCollectionViewChange?: (showing: boolean) => void;
};

const OVERVIEW: ViewChoice = { id: "builtin:overview", name: "Overview", renderer: "overview" };

// A group's built-in navigation list, offered only while the catalog cannot load.
const TYPES: ViewChoice = { ...OVERVIEW, name: "Types" };

const RENDERERS = { overview: BuiltinOverview };

export function HomeTab({
  active = true,
  summary: providedSummary,
  selectedType,
  selection,
  location,
  editSession,
  onOpenNote,
  onSelectCollection,
  onOpenIssues,
  onSelectIssue,
  onStageOps,
  onPresentation,
  onOpenNode,
  onOpenView,
  onOpenSearch,
  onCollectionViewChange,
}: Props) {
  const summaryQuery = useOntologySummaryQuery(active);
  const summary = providedSummary ?? summaryQuery.data ?? null;
  const group = selection.kind === "group" ? selection.group : null;
  const realType = selectedType && !isPseudoType(selectedType) ? selectedType : null;
  const kind = summary?.interfaces?.some((item) => item.name === realType) ? "interface" : "type";

  const context: ViewContext = group
    ? { kind: "group", group }
    : realType
      ? kind === "interface"
        ? { kind: "interface", interface: realType }
        : { kind: "type", type: realType }
      : { kind: "workspace" };

  const catalogQuery = useViewCatalogQuery(active);
  const targetKind = group ? "group" : realType ? kind : "workspace";
  const targetName = group ?? realType ?? "";

  const target =
    catalogQuery.data?.targets?.find(
      (target) => target.kind === targetKind && target.name === targetName,
    ) ?? null;

  // All notes has no built-in view; without the catalog it shows an error instead.
  const builtin = targetKind === "workspace" ? null : group ? TYPES : OVERVIEW;

  const fallback: ViewTarget | null = useMemo(
    () =>
      builtin && {
        kind: targetKind,
        name: targetName,
        choices: [builtin],
        defaultChoiceId: builtin.id,
      },
    [builtin, targetKind, targetName],
  );

  const shownTarget = target ?? fallback;

  const selectionState = useViewSelection({
    // Until the summary loads, an interface still looks like a type, so wait to
    // fall back (and migrate legacy preferences) until the kind is known.
    target: target ?? (catalogQuery.data && (summary || !realType) ? fallback : null),
    views: catalogQuery.data?.views,
    explicit: location.presentation,
    vaultKey: editSession.vaultKey ?? null,
    onSelect: onPresentation,
  });

  const choice = selectionState.choice ?? builtin;
  const collectionView = Boolean(realType) && choice?.renderer !== "overview";

  useEffect(() => {
    if (active) onCollectionViewChange?.(collectionView);
  }, [active, collectionView, onCollectionViewChange]);
  const definition = catalogQuery.data?.views.find((view) => view.id === choice?.viewId) ?? null;

  // Overview fetches this same query key itself. Disabled here, the header still
  // reads Overview's cached result without a second mount-time refetch.
  const typeQuery = useOntologyTypeQuery(
    realType,
    editSession.session,
    active && Boolean(realType) && choice?.renderer !== "overview",
  );

  const validationQuery = useValidationQuery(active);
  const validation = validationQuery.data ?? null;

  const repair = useValidationRepair(
    validation?.snapshot?.generation ?? null,
    validation?.snapshot?.repairPlanFingerprint,
  );

  const openIssues = onOpenIssues ?? (() => onSelectCollection(PSEUDO_TYPE_ISSUES));

  const scopes = useMemo(
    () => (realType ? [{ kind, key: realType } as const] : []),
    [realType, kind],
  );

  const summaries = useValidationScopeSummaries(
    active ? (validation?.snapshot?.generation ?? null) : null,
    scopes,
  );

  if (selectedType === PSEUDO_TYPE_ISSUES)
    return (
      <NotesIssuesHome
        validationEnvelope={validation}
        loading={validationQuery.isLoading || validationQuery.isFetching}
        error={validationQuery.isError ? validationQuery.error : null}
        onOpenNote={onOpenNote}
        pendingChangeCount={changeSummary(editSession.session).opCount}
        onReviewChanges={() => onSelectCollection(PSEUDO_TYPE_MODIFIED)}
        onStageOps={onStageOps}
        scope={location.issueScope}
        selectedIssueKey={location.issueKey}
        onSelectIssue={onSelectIssue}
        repairController={repair}
      />
    );

  if (selectedType === PSEUDO_TYPE_MODIFIED)
    return (
      <NotesModifiedHome
        session={editSession.session}
        onOpenNote={onOpenNote}
        onReplaceOps={(ops) => void editSession.replaceOps(ops).catch(() => undefined)}
      />
    );
  const notes = typeQuery.data?.notes ?? [];

  const mean = notes.length
    ? Math.round(
        (notes.reduce((sum, note) => sum + (note.relationCount || 0), 0) / notes.length) * 10,
      ) / 10
    : 0;

  const services: ViewServices = {
    ...editSession,
    onOpenNote,
    onSelectCollection,
    onOpenNode,
    onOpenView,
    onOpenSearch,
    onStageOps,
    onOpenIssues: openIssues,
  };

  return (
    <div className={realType || group ? "type-workspace" : "home-workspace"}>
      <div className="home-workspace__header-slot">
        {!summary && summaryQuery.isError && (
          <div className="ontology-home__unavailable" role="alert">
            <span>
              Workspace data is unavailable. {summaryQuery.error.message} Check that the Rhizome
              server is reachable, then retry.
            </span>
            <button type="button" onClick={() => void summaryQuery.refetch()}>
              Retry connection
            </button>
          </div>
        )}
        {(realType || group || summary) && (
          <TypeWorkspaceHeader
            typeName={realType ?? ""}
            label={
              group ??
              (realType
                ? (typeQuery.data?.type?.pluralLabel ?? typeQuery.data?.type?.label ?? realType)
                : "All notes")
            }
            eyebrow={
              group
                ? "Display group"
                : realType
                  ? kind === "interface"
                    ? "Interface"
                    : "Type"
                  : ""
            }
            description={realType ? typeQuery.data?.type?.description : null}
            totalNotes={realType ? (typeQuery.data?.count ?? null) : null}
            meanRelations={group || !typeQuery.data ? null : mean}
            issueCount={
              realType
                ? summaries.summaries.get(validationScopeKey({ kind, key: realType }))?.issueCount
                : undefined
            }
            validationHealth={validation?.health}
            onOpenIssues={() => openIssues(realType ? { kind, key: realType } : undefined)}
            showStats={Boolean(realType) && !collectionView}
            viewSelector={
              shownTarget &&
              choice && (
                <ViewSelector
                  target={shownTarget}
                  selectedId={choice.id}
                  onSelect={selectionState.select}
                  status={selectionState}
                />
              )
            }
          />
        )}
      </div>
      <div className="home-workspace__body">
        {(!catalogQuery.data && !catalogQuery.isError) ||
        ((realType || group) && !summary && !summaryQuery.isError) ||
        (selectionState.loading && !location.presentation) ? (
          <p role="status">Loading views…</p>
        ) : !choice && catalogQuery.isError ? (
          <div className="ontology-home__unavailable" role="alert">
            <span>All notes views could not be loaded.</span>
            <button type="button" onClick={() => void catalogQuery.refetch()}>
              Retry
            </button>
          </div>
        ) : !choice ? (
          <p className="ontology-empty">No views are available for All notes.</p>
        ) : (
          <ViewHost
            view={{ id: choice.viewId ?? choice.id, name: choice.name }}
            context={context}
            choice={choice}
            definition={definition}
            configuration={
              isJsonObject(definition?.configuration) ? definition.configuration : undefined
            }
            active={active}
            services={services}
            renderers={RENDERERS}
            embedded
          />
        )}
        {Boolean(catalogQuery.data?.issues?.length) && (
          <details className="configured-view__status configured-view__status--error">
            <summary>View configuration issues</summary>
            <ul>
              {catalogQuery.data?.issues?.map((issue, index) => (
                <li key={`${issue.path}:${issue.code}:${index}`}>
                  {issue.view ? `${issue.view}: ` : ""}
                  {issue.message}
                </li>
              ))}
            </ul>
          </details>
        )}
        {catalogQuery.isError && builtin && (
          <p role="alert">
            View configuration could not be loaded. {builtin.name} remains available.
          </p>
        )}
      </div>
    </div>
  );
}
