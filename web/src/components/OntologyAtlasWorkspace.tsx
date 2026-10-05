import { useQuery } from "@tanstack/react-query";
import { useCallback } from "react";

import { getOntologyAtlas } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import { OntologyAtlasHome } from "./OntologyAtlasHome";
import { OntologyTypeDetail } from "./OntologyTypeDetail";
import { notifyLocationChange, useLocationSnapshot } from "./locationStore";
import {
  buildOntologyPath,
  type OntologyRouteSelection,
  parseOntologyRoute,
} from "./ontologyRoute";

export function OntologyAtlasWorkspace() {
  const { pathname } = useLocationSnapshot();
  const selection = parseOntologyRoute(pathname);

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
  );
}
