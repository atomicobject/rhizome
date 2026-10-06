import { buildRailGroups, label } from "../components/noteRailGroups";
import { buildNotesLocation } from "../components/notesRoute";
import { useCallback, useMemo } from "react";
import { isBoolean, isJsonObject } from "../api/parse";
import { HomeGraphLayout } from "../components/HomeGraphLayout";
import { buildTypeColors } from "../components/graphViewShared";
import {
  useGlobalNotesGraphQuery,
  useOntologySummaryQuery,
  useOntologyTypeQuery,
  useValidationQuery,
} from "../components/useNotesQueries";
import { useValidationScopeSummaries } from "../components/useValidationScopeSummaries";
import type { GraphDisplay } from "../components/useSigmaGraph";
import { useScopedViewPreference } from "../viewPreferences/hooks";
import { hostPreferenceScope, type ViewRuntimeProps } from "./ViewHost";

type DisplayChoices = Partial<GraphDisplay>;

const DISPLAY_DEFAULTS: GraphDisplay = {
  scaleByAuthority: true,
  showEdgeLabels: true,
  showProblems: false,
};

const DISPLAY_KEYS: (keyof GraphDisplay)[] = ["scaleByAuthority", "showEdgeLabels", "showProblems"];

const NO_CHOICES: DisplayChoices = {};

const DISPLAY_PREFERENCE = {
  defaultValue: NO_CHOICES,
  validate: (value: unknown): value is DisplayChoices =>
    isJsonObject(value) &&
    Object.entries(value).every(([key, item]) => key in DISPLAY_DEFAULTS && isBoolean(item)),
};

/**
 * Overview remembers its display toggles per instance (SPEC-0114); graph
 * search and camera motion stay with the mount.
 */
export function useGraphDisplay(props: ViewRuntimeProps) {
  const preference = useScopedViewPreference(
    hostPreferenceScope(props),
    props.services.vaultKey ?? null,
    "overview.display",
    DISPLAY_PREFERENCE,
  );

  const choices = preference.value;
  const display = useMemo(() => ({ ...DISPLAY_DEFAULTS, ...choices }), [choices]);
  const durable = props.services.vaultKey !== null && props.services.vaultKey !== undefined;

  const { update } = preference;

  // A toggle back to its default drops the choice, so later default changes apply.
  const onChange = useCallback(
    (patch: DisplayChoices) =>
      void update((current) =>
        Object.fromEntries(
          Object.entries({ ...current, ...patch }).filter(([key, value]) =>
            DISPLAY_KEYS.some((name) => name === key && DISPLAY_DEFAULTS[name] !== value),
          ),
        ),
      ).catch(() => {}),
    [update],
  );

  return { display: durable ? display : undefined, onChange };
}

export function BuiltinOverview(props: ViewRuntimeProps) {
  const { context, active, services } = props;
  const graphDisplay = useGraphDisplay(props);

  const selectedType =
    context.kind === "type"
      ? context.type
      : context.kind === "interface"
        ? context.interface
        : null;

  const summary = useOntologySummaryQuery(active);

  const detail = useOntologyTypeQuery(
    selectedType,
    services.session,
    active && Boolean(selectedType),
  );

  const graph = useGlobalNotesGraphQuery(active && Boolean(selectedType));
  const validation = useValidationQuery(active);

  const scopes = useMemo(
    () => [
      ...(detail.data?.notes ?? []).map((note) => ({ kind: "note" as const, key: note.path })),
      ...(graph.data?.nodes ?? []).flatMap((node) => {
        const key = node.notePath || node.path;

        return key ? [{ kind: "note" as const, key }] : [];
      }),
    ],
    [detail.data?.notes, graph.data?.nodes],
  );

  const summaries = useValidationScopeSummaries(
    active ? (validation.data?.snapshot?.generation ?? null) : null,
    scopes,
  );

  // Key on the names alone: a new colors map rebuilds the graph renderer, and a
  // summary refresh after any edit or validation run changes only counts.
  const typeNames = (summary.data?.types ?? []).map((type) => type.name).join("\n");
  const colors = useMemo(() => buildTypeColors(typeNames.split("\n")), [typeNames]);

  const summaryError =
    summary.isError && !summary.data
      ? `${summary.error.message || "The request failed."} Check that the Rhizome server is reachable, then retry.`
      : null;

  if (context.kind === "group") {
    if (summaryError)
      return (
        <div className="ontology-empty" role="alert">
          <p>{summaryError}</p>
          <button type="button" onClick={() => void summary.refetch()}>
            Retry connection
          </button>
        </div>
      );

    const group = buildRailGroups(summary.data ?? null).find(
      (group) => group.name === context.group,
    );

    if (!group?.children.length)
      return <p className="ontology-empty">No types are in {context.group}.</p>;

    return (
      <nav className="ontology-empty" aria-label={`${context.group} types`}>
        {group.children.map((node) => {
          const item = node.kind === "type" ? node.type : node.interface;

          return (
            <p key={item.name}>
              <a
                href={buildNotesLocation({ selection: { kind: "type", typeName: item.name } })}
                onClick={(event) => {
                  if (!services.onSelectCollection || event.metaKey || event.ctrlKey) return;
                  event.preventDefault();
                  services.onSelectCollection(item.name);
                }}
              >
                {label(item)}
              </a>
            </p>
          );
        })}
      </nav>
    );
  }

  return (
    <HomeGraphLayout
      selectedType={selectedType}
      summary={summary.data ?? null}
      typeDetail={detail.data ?? null}
      // Hold the graph until the summary settles: drawing it before its type
      // colors exist means a second full renderer build when they arrive.
      graph={summary.isPending ? null : (graph.data ?? null)}
      graphLoading={graph.isFetching}
      graphError={graph.isError ? graph.error.message : null}
      onRetryGraph={() => void graph.refetch()}
      typeColors={colors}
      loading={
        selectedType ? detail.isPending && detail.fetchStatus === "fetching" : summary.isPending
      }
      unavailable={summaryError ?? (detail.isError && !detail.data ? detail.error.message : null)}
      onRetry={() => {
        void summary.refetch();

        if (selectedType) void detail.refetch();
      }}
      onOpenNote={services.onOpenNote}
      onOpenIssues={() =>
        services.onOpenIssues?.(
          selectedType
            ? { kind: context.kind === "interface" ? "interface" : "type", key: selectedType }
            : undefined,
        )
      }
      validation={validation.data ?? null}
      validationSummaries={summaries.summaries}
      graphDisplay={graphDisplay.display}
      onGraphDisplayChange={graphDisplay.onChange}
    />
  );
}
