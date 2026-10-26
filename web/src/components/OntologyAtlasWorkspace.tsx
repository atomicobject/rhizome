import { useQuery } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";

import { getOntologyAtlas } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import { OntologyAtlasHome } from "./OntologyAtlasHome";
import { OntologyTypeDetail } from "./OntologyTypeDetail";
import { notifyLocationChange, useLocationSnapshot } from "./locationStore";
import { buildRailGroups } from "./noteRailGroups";
import {
  buildOntologyPath,
  type OntologyRouteSelection,
  parseOntologyRoute,
} from "./ontologyRoute";
import { RailIcon, TypeTree, useRail } from "./RailTree";
import { summaryCountsKnown, useOntologySummaryQuery } from "./useNotesQueries";
import { WorkbenchSidebar } from "./WorkbenchSidebar";
import { useWorkbenchSidebar } from "./sidebarState";

const EXPANDED_KEY = "rhizome:ontology:sidebar:expanded:v1";

export function OntologyAtlasWorkspace({
  sidebarTitle,
  sidebarHint,
}: {
  sidebarTitle?: string | null;
  sidebarHint?: string | null;
}) {
  const { pathname } = useLocationSnapshot();
  const selection = parseOntologyRoute(pathname);
  const selected = selection.kind === "type" ? selection.name : null;
  const sidebarShown = !useWorkbenchSidebar().collapsed;
  const summaryQuery = useOntologySummaryQuery();
  const summary = summaryQuery.data ?? null;
  const groups = useMemo(() => buildRailGroups(summary), [summary]);
  const { railProps, ...expansion } = useRail(EXPANDED_KEY, groups, selected);

  const atlasQuery = useQuery({
    queryKey: queryKeys.ontology.atlas(),
    queryFn: ({ signal }) => getOntologyAtlas({ signal }),
  });

  const atlas = atlasQuery.data ?? null;

  const atlasError =
    !atlas && atlasQuery.error
      ? atlasQuery.error instanceof Error
        ? atlasQuery.error.message
        : String(atlasQuery.error)
      : null;

  const navigate = useCallback((next: OntologyRouteSelection) => {
    const url = buildOntologyPath(next);

    if (url !== window.location.pathname) {
      window.history.pushState({}, "", url);
    }

    notifyLocationChange();
  }, []);

  const onSelectType = useCallback((name: string) => navigate({ kind: "type", name }), [navigate]);
  const onBack = useCallback(() => navigate({ kind: "atlas" }), [navigate]);

  return (
    <div className="workspace workbench">
      <WorkbenchSidebar title={sidebarTitle} titleHint={sidebarHint}>
        <aside className="notes-left-rail" aria-label="Ontology navigation" {...railProps}>
          <div className="notes-left-rail__collections">
            <div className="ontology-type-list">
              <div className="ontology-type-row-wrap">
                <button
                  type="button"
                  data-rail-row
                  className={`ontology-type-row ontology-type-row--pseudo${selected === null ? " is-selected" : ""}`}
                  aria-current={selected === null ? "page" : undefined}
                  onClick={onBack}
                >
                  <RailIcon name="atlas" />
                  <span className="ontology-type-row__label">Atlas</span>
                </button>
              </div>
              {!summary && summaryQuery.isError ? (
                <p className="notes-left-rail__state" role="status">
                  Types unavailable.{" "}
                  <button type="button" onClick={() => void summaryQuery.refetch()}>
                    Retry
                  </button>
                </p>
              ) : null}
              <TypeTree
                groups={groups}
                expansion={expansion}
                active={sidebarShown}
                selected={selected}
                showCounts={summaryCountsKnown(summary)}
                onSelect={onSelectType}
                selectEmbedded
              />
            </div>
          </div>
        </aside>
      </WorkbenchSidebar>
      <div className="workbench__panel">
        <div className="ontology-atlas-workspace">
          {selection.kind === "atlas" ? (
            <OntologyAtlasHome
              atlas={atlas}
              error={atlasError}
              onRetry={() => void atlasQuery.refetch()}
            />
          ) : (
            <OntologyTypeDetail
              name={selection.name}
              atlas={atlas}
              onBack={onBack}
              onSelectType={onSelectType}
            />
          )}
        </div>
      </div>
    </div>
  );
}
