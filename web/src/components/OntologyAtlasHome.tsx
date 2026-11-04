import { lazy, Suspense } from "react";

import type { OntologyAtlasResponse } from "../api/types";

const OntologyGraph = lazy(() =>
  import("./OntologyGraph").then((m) => ({ default: m.OntologyGraph })),
);

function AtlasHeader({ count, about }: { count?: number; about?: string }) {
  return (
    <header className="view-header">
      <div className="view-header__title-row">
        <h2>Ontology Atlas</h2>
        {count !== undefined ? <span className="view-header__count">{count}</span> : null}
        {about ? <span className="view-header__about">{about}</span> : null}
      </div>
    </header>
  );
}

const plural = (count: number, one: string, many: string) =>
  `${count.toLocaleString("en-US")} ${count === 1 ? one : many}`;

type Props = {
  atlas: OntologyAtlasResponse | null;
  error: string | null;
  onRetry?: () => void;
};

export function OntologyAtlasHome({ atlas, error, onRetry }: Props) {
  if (error) {
    return (
      <section className="ontology-atlas">
        <AtlasHeader />
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
        <AtlasHeader />
        <p className="ontology-atlas__loading delayed-status">Loading…</p>
      </section>
    );
  }

  if (!atlas.schemaPresent) {
    return (
      <section className="ontology-atlas">
        <AtlasHeader />
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
      <AtlasHeader
        count={types.length + interfaces.length}
        about={`${plural(types.length, "type", "types")} and ${plural(interfaces.length, "interface", "interfaces")} · ${plural(totalTyped, "typed note", "typed notes")}`}
      />

      <div className="ontology-atlas__diagram-scroll">
        <Suspense fallback={<p className="delayed-status">Rendering diagram…</p>}>
          <OntologyGraph atlas={atlas} />
        </Suspense>
      </div>
    </section>
  );
}
