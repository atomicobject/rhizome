import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { queryKeys } from "../api/queryKeys";
import { publicWorkspaceRef } from "../api/client";
import type { JsonObject } from "../api/parse";
import type { ViewCatalogEntry, ViewExecuteRequest } from "../api/types";
import { ConfiguredView } from "../components/ConfiguredView";
import { activeViewVariant } from "../components/ConfiguredViewModel";
import { viewErrorFromUnknown } from "./viewErrors";
import { useValidationQuery, useViewExecutionQuery } from "../components/useNotesQueries";
import {
  noteIssueCounts,
  useValidationScopeSummaries,
} from "../components/useValidationScopeSummaries";
import { useViewState } from "../components/useViewState";
import type { ViewRuntimeProps } from "./ViewHost";
import { remapCommittedEditOps } from "../components/editing/editSessionState";
import {
  DURABLE_QUERY_FIELDS,
  durableRequest,
  nativePreferenceScope,
} from "../viewPreferences/native";
import { sanitizeNativePreferences } from "../viewPreferences/sanitize";
import type { PreferenceStore } from "../viewPreferences/store";
import { preferenceScopeKey } from "../viewPreferences/types";

/** Query overrides naming fields or presets the view may no longer offer. */
const FIELD_KEYS = ["filters", "filterPreset", "sort", "group", "columnField", "laneField"].map(
  (field) => `native.${field}`,
);

/**
 * Remembered overrides checked against the fields the view offers (SPEC-0114).
 * A stale field would hide rows or fail the query without saying why, so when
 * restored overrides name fields, a settled execution of the view's defaults
 * proves which fields exist first. Each instance checks once per definition:
 * an old baseline must not judge choices made afterward, and index or session
 * changes must not repeat the probe. A failed probe runs the saved request as
 * is, since a personal Board axis can be valid when the authored one is not.
 */
function useCheckedOverrides(
  definition: ViewCatalogEntry,
  instance: string,
  variant: string,
  savedState: ViewExecuteRequest,
  preferences: {
    values: JsonObject;
    loading: boolean;
    error: Error | null;
    store: PreferenceStore | null;
  },
  enabled: boolean,
  services: ViewRuntimeProps["services"],
) {
  const { values, store } = preferences;
  const [checkedInstance, setCheckedInstance] = useState<string | null>(null);
  const named = FIELD_KEYS.some((key) => Object.hasOwn(values, key));
  const pending = named && checkedInstance !== instance;

  const probe = useViewExecutionQuery(
    definition.id,
    { variant },
    services.session,
    enabled && !preferences.loading && pending,
    services.readLifecycle,
  );

  // Held or refetching results may predate a schema change; only a settled answer is proof.
  const baseline =
    pending && probe.isSuccess && !probe.isPlaceholderData && !probe.isFetching
      ? probe.data
      : undefined;

  const failed = pending && probe.isError && !probe.isFetching;

  const checked = useMemo(
    () => (baseline ? sanitizeNativePreferences(values, definition, baseline) : null),
    [baseline, definition, values],
  );

  // Restored overrides without fields need no proof, but later choices must not get one either.
  const restoredClean = !preferences.loading && !preferences.error && !named;

  useEffect(() => {
    if (checkedInstance === instance || !(checked || failed || restoredClean)) return;
    setCheckedInstance(instance);

    if (!store || !checked?.changed) return;
    const { set = {}, unset = [] } = checked.patch;

    // A value the reader changed since the probe is theirs, not the stale one.
    const unchanged = (current: JsonObject, key: string) =>
      JSON.stringify(current[key]) === JSON.stringify(values[key]);

    // The store applies this optimistically, so the saved state is clean once checked.
    void store
      .patch((current) => ({
        set: Object.fromEntries(Object.entries(set).filter(([key]) => unchanged(current, key))),
        unset: unset.filter((key) => unchanged(current, key)),
      }))
      .catch(() => {});
  }, [checked, checkedInstance, failed, instance, restoredClean, store, values]);

  const state = useMemo(() => {
    if (!checked) return savedState;

    const transient = Object.fromEntries(
      Object.entries(savedState).filter(
        ([field]) => !DURABLE_QUERY_FIELDS.some((durable) => durable === field),
      ),
    );

    return { ...transient, ...durableRequest(checked.values) };
  }, [checked, savedState]);

  return { state, ready: !preferences.loading && (!pending || Boolean(checked) || failed) };
}

export function NativeCollectionView({
  definition,
  choice,
  active,
  services,
  embedded,
  context,
  preferenceSlot,
}: ViewRuntimeProps & { definition: ViewCatalogEntry }) {
  const queryClient = useQueryClient();

  const [savedState, setState, preferences] = useViewState(
    definition,
    services.vaultKey ?? null,
    context,
    preferenceSlot,
  );

  const variant = choice.variant ?? choice.renderer;

  const instance = JSON.stringify([
    services.vaultKey ?? null,
    preferenceScopeKey(nativePreferenceScope(definition, context, preferenceSlot)),
    definition.definition,
  ]);

  const checked = useCheckedOverrides(
    definition,
    instance,
    variant,
    savedState,
    preferences,
    active,
    services,
  );

  const state = useMemo(() => {
    const next = { ...checked.state, variant };
    const offset = next.page?.offset ?? 0;

    // The table grows one window from offset 0 and has no Previous control, so
    // a restored later page widens to a window that starts at the first row.
    return offset > 0 && activeViewVariant(definition, next.variant, undefined) === "table"
      ? { ...next, page: { offset: 0, first: offset + (next.page?.first ?? 25) } }
      : next;
  }, [checked.state, variant, definition]);

  const query = useViewExecutionQuery(
    definition.id,
    state,
    services.session,
    // Remembered filters and sort decide the query, so it waits for them (SPEC-0114).
    active && checked.ready,
    services.readLifecycle,
  );

  const validation = useValidationQuery(active);

  const scopes = useMemo(
    () =>
      (query.displayData?.rows ?? []).map((row) => ({
        kind: "note" as const,
        key: row.ref.notePath || row.path || "",
      })),
    [query.displayData?.rows],
  );

  const summaries = useValidationScopeSummaries(
    active ? (validation.data?.snapshot?.generation ?? null) : null,
    scopes,
  );

  const issues = useMemo(() => noteIssueCounts(summaries.summaries), [summaries.summaries]);

  return (
    <ConfiguredView
      view={definition}
      execution={query.displayData ?? null}
      loading={!checked.ready || query.isLoading || query.isFetching || Boolean(services.busy)}
      stagedEditsPending={Boolean(services.unacknowledged) || query.savedEditsPending}
      error={query.isError ? viewErrorFromUnknown(query.error) : null}
      state={state}
      onStateChange={setState}
      onRefresh={() => void query.refetch()}
      onOpenRow={(row, mode) =>
        services.onOpenNode
          ? services.onOpenNode(row.ref, { beside: mode === "beside" })
          : services.onOpenNote(publicWorkspaceRef(row.ref), mode)
      }
      editSession={query.displaySession}
      vaultKey={services.vaultKey}
      onStageOps={(ops) =>
        services.onStageOps(remapCommittedEditOps(ops, query.displaySession?.refLineage ?? []))
      }
      issueCounts={issues}
      onOpenIssues={services.onOpenIssues}
      embedded={embedded}
      active={active}
      context={context}
      preferenceSlot={preferenceSlot}
      onViewSaved={async (response) => {
        await queryClient.invalidateQueries({ queryKey: queryKeys.views.all() });

        // A view saved from the generated layouts replaces them under its own id.
        if (response.id !== definition.id) services.onOpenView?.(response.id, context);
      }}
    />
  );
}
