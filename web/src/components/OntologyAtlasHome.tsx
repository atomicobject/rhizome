import { lazy, Suspense } from "react";

import type { OntologyAtlasResponse } from "../api/types";

const OntologyGraph = lazy(() =>
  import("./OntologyGraph").then((m) => ({ default: m.OntologyGraph })),
);

function AtlasHeader({ about }: { about?: string }) {
  return (
    <header className="view-header">
      <div className="view-header__title-row">
        <h2>Ontology Atlas</h2>
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
  // Cards tell records from items embedded in other notes, so the header does too.
  const isEmbedded = (entry: (typeof types)[number]) => entry.type?.locator === "EMBEDDED";
  const sum = (list: typeof types) => list.reduce((acc, entry) => acc + (entry.count ?? 0), 0);
  const records = sum(types.filter((entry) => !isEmbedded(entry)));
  const embedded = sum(types.filter(isEmbedded));

  return (
    <section className="ontology-atlas">
      <AtlasHeader
        about={`${plural(types.length, "type", "types")} and ${plural(interfaces.length, "interface", "interfaces")} · ${plural(records, "record", "records")}${embedded ? ` · ${plural(embedded, "embedded item", "embedded items")}` : ""}`}
      />

      <div className="ontology-atlas__diagram-scroll">
        <Suspense fallback={<p className="delayed-status">Rendering diagram…</p>}>
          <OntologyGraph atlas={atlas} />
        </Suspense>
      </div>
    </section>
  );
}
