import { lazy, Suspense } from "react";

import type { OntologyAtlasResponse } from "../api/types";

const OntologyGraph = lazy(() =>
  import("./OntologyGraph").then((m) => ({ default: m.OntologyGraph })),
);

type Props = {
  atlas: OntologyAtlasResponse | null;
  error: string | null;
  onRetry?: () => void;
};

export function OntologyAtlasHome({ atlas, error, onRetry }: Props) {
  if (error) {
    return (
      <section className="ontology-atlas">
        <h1>Ontology Atlas</h1>
        <div role="alert" className="ontology-atlas__error">
          Failed to load atlas: {error}
          {onRetry ? (
            <button type="button" onClick={onRetry}>
              Retry
            </button>
          ) : null}
        </div>
      </section>
    );
  }

  if (!atlas) {
    return (
      <section className="ontology-atlas">
        <h1>Ontology Atlas</h1>
        <p className="ontology-atlas__loading">Loading…</p>
      </section>
    );
  }

  if (!atlas.schemaPresent) {
    return (
      <section className="ontology-atlas">
        <h1>Ontology Atlas</h1>
        <p className="ontology-atlas__zero">
          This vault has no ontology schema yet. Add a <code>.rhizome/ontology/*.graphql</code> file
          to define typed notes, interfaces, and relations. Once present, this page will show every
          type Rhizome understands plus an ER diagram of the schema.
        </p>
      </section>
    );
  }

  const types = atlas.types ?? [];
  const interfaces = atlas.interfaces ?? [];
  const totalTyped = types.reduce((acc, entry) => acc + (entry.count ?? 0), 0);

  return (
    <section className="ontology-atlas">
      <header className="ontology-atlas__header">
        <h1>Ontology Atlas</h1>
        <dl className="ontology-atlas__stats">
          <div>
            <dt>Types</dt>
            <dd>{types.length}</dd>
          </div>
          <div>
            <dt>Interfaces</dt>
            <dd>{interfaces.length}</dd>
          </div>
          <div>
            <dt>Typed nodes</dt>
            <dd>{totalTyped}</dd>
          </div>
        </dl>
      </header>

      <div className="ontology-atlas__diagram-scroll">
        <Suspense fallback={<p>Rendering diagram…</p>}>
          <OntologyGraph atlas={atlas} />
        </Suspense>
      </div>
    </section>
  );
}
