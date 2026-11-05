import "./ontology-refinements.css";

import type React from "react";
import { type KeyboardEvent, useId, useState } from "react";

import type { OntologyAtlasResponse, OntologyTypeResponse } from "../api/types";
import { humanizeName } from "../lib/labels";
import { typeAccentColor } from "../lib/typeAccent";

import { tabKeyTarget } from "./focusManagement";
import { Markdown } from "./markdown/Markdown";

import { useActiveEditSession } from "./useOntologyEditSession";
import { useOntologyTypeQuery } from "./useNotesQueries";

import { SchemaPanel } from "./OntologyTypeSchema";
import { TypeDetailBody } from "./OntologyTypeOverview";

type DetailTab = "overview" | "guide" | "schema";

const TAB_ORDER: DetailTab[] = ["overview", "guide", "schema"];

const TAB_LABELS: Record<DetailTab, string> = {
  overview: "Overview",
  guide: "Authoring guide",
  schema: "Schema",
};

type Props = {
  name: string;
  atlas: OntologyAtlasResponse | null;
  onBack: () => void;
  onSelectType: (name: string) => void;
};

export function OntologyTypeDetail({ name, atlas, onBack, onSelectType }: Props) {
  const [tabState, setTabState] = useState<{
    name: string;
    tab: DetailTab;
  }>({ name, tab: "overview" });

  const tab = tabState.name === name ? tabState.tab : "overview";
  const setTab = (next: DetailTab) => setTabState({ name, tab: next });
  const tabsID = useId();

  const typeQuery = useOntologyTypeQuery(name, useActiveEditSession());

  const data = typeQuery.data ?? null;

  const error = typeQuery.error
    ? typeQuery.error instanceof Error
      ? typeQuery.error.message
      : String(typeQuery.error)
    : null;

  const accent = typeAccentColor(name);

  return (
    <section
      className="ontology-type-detail"
      // SAFETY: React's CSSProperties omits `--*` custom properties; this object sets only one.
      style={{ "--type-accent": accent } as React.CSSProperties}
    >
      <header className="view-header ontology-type-detail__header">
        <div className="view-header__title-row">
          <button type="button" className="ontology-type-detail__back" onClick={onBack}>
            Atlas
          </button>
          <span className="ontology-type-detail__crumb" aria-hidden="true">
            /
          </span>
          <h2 title={name}>{humanizeName(name)}</h2>
          {data ? (
            <span className="view-header__count">{data.count} implementing records</span>
          ) : null}
          {data?.type?.role ? (
            <span className="view-header__about">{humanizeName(data.type.role.toLowerCase())}</span>
          ) : null}
        </div>
        {data ? (
          <div className="view-header__tabs">
            <TabBar idPrefix={tabsID} active={tab} onSelect={setTab} />
          </div>
        ) : null}
      </header>
      {error && !data ? (
        <div className="ontology-type-detail__error" role="alert">
          Failed to load type: {error}{" "}
          <button type="button" onClick={() => typeQuery.refetch()}>
            Retry
          </button>
        </div>
      ) : !data ? (
        <p className="ontology-type-detail__loading delayed-status">Loading…</p>
      ) : (
        <>
          {error ? (
            <div className="ontology-type-detail__error" role="status">
              Refresh failed; showing cached data.{" "}
              <button type="button" onClick={() => typeQuery.refetch()}>
                Retry
              </button>
            </div>
          ) : null}
          <div
            id={`${tabsID}-panel`}
            role="tabpanel"
            aria-labelledby={`${tabsID}-tab-${tab}`}
            className="ontology-type-detail__tabpanel"
          >
            {tab === "overview" ? (
              <TypeDetailBody data={data} atlas={atlas} onSelectType={onSelectType} />
            ) : null}
            {tab === "guide" ? <AuthoringGuidePanel data={data} /> : null}
            {tab === "schema" ? <SchemaPanel data={data} /> : null}
          </div>
        </>
      )}
    </section>
  );
}

/** The tab pattern, as in the note context rail: one Tab stop, arrows move and select. */
function TabBar({
  idPrefix,
  active,
  onSelect,
}: {
  idPrefix: string;
  active: DetailTab;
  onSelect: (tab: DetailTab) => void;
}) {
  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    const next = tabKeyTarget(event.key, TAB_ORDER.indexOf(active), TAB_ORDER.length);

    if (next === null) return;
    event.preventDefault();
    onSelect(TAB_ORDER[next]);
    document.getElementById(`${idPrefix}-tab-${TAB_ORDER[next]}`)?.focus();
  };

  return (
    <div
      className="ontology-type-detail__tabs"
      role="tablist"
      aria-label="Type view"
      aria-orientation="horizontal"
    >
      {TAB_ORDER.map((id) => {
        const selected = id === active;

        return (
          <button
            key={id}
            id={`${idPrefix}-tab-${id}`}
            type="button"
            role="tab"
            aria-selected={selected}
            aria-controls={`${idPrefix}-panel`}
            tabIndex={selected ? 0 : -1}
            onKeyDown={onKeyDown}
            className={`ontology-type-detail__tab${
              selected ? " ontology-type-detail__tab--active" : ""
            }`}
            onClick={() => onSelect(id)}
          >
            {TAB_LABELS[id]}
          </button>
        );
      })}
    </div>
  );
}

function AuthoringGuidePanel({ data }: { data: OntologyTypeResponse }) {
  const guide = data.authoringGuide?.trim() ?? "";

  if (!guide) {
    return (
      <section className="ontology-type-detail__section ontology-type-detail__guide">
        <p className="ontology-type-detail__empty">
          No authoring guide available for this type. Ensure the ontology schema is loaded and the
          type is declared.
        </p>
      </section>
    );
  }

  return (
    <section className="ontology-type-detail__section ontology-type-detail__guide">
      <div className="ontology-type-detail__markdown">
        <Markdown>{guide}</Markdown>
      </div>
    </section>
  );
}
