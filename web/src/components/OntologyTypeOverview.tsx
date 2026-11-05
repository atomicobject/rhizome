import "./ontology-refinements.css";

import type React from "react";
import { useMemo } from "react";

import type {
  OntologyAtlasResponse,
  OntologyAtlasTypeEntry,
  OntologyTypeResponse,
  TypeDoc,
  TypeFieldDoc,
} from "../api/types";
import { humanizeName } from "../lib/labels";
import { typeAccentColor } from "../lib/typeAccent";

import { buildNotesPath } from "./notesRoute";

import { TargetChip, directionLabel, Prose, FieldTable, ExampleCard } from "./OntologyTypeFields";

function isRelationField(field: TypeFieldDoc): boolean {
  return (
    field.kind === "link" ||
    field.kind === "reverse" ||
    field.kind === "neighbor" ||
    field.kind === "section"
  );
}

type InboundRef = {
  fromType: string;
  fromRole?: string;
  fieldName: string;
  direction?: string;
  list?: boolean;
  description?: string;
};

function collectInboundRefs(atlas: OntologyAtlasResponse | null, targetName: string): InboundRef[] {
  if (!atlas) return [];
  const refs: InboundRef[] = [];

  const sources: Array<{ role: string; docs: TypeDoc[] }> = [
    {
      role: "note",
      docs: (atlas.types ?? []).flatMap((entry) => (entry.type ? [entry.type] : [])),
    },
    { role: "interface", docs: atlas.interfaces ?? [] },
    { role: "section", docs: atlas.sections ?? [] },
  ];

  for (const { role, docs } of sources) {
    for (const doc of docs) {
      if (!doc?.name || doc.name === targetName) continue;

      for (const field of doc.fields ?? []) {
        if (!isRelationField(field)) continue;

        if (field.typeName !== targetName) continue;
        refs.push({
          fromType: doc.name,
          fromRole: doc.role || role,
          fieldName: field.name ?? "",
          direction: field.direction,
          list: field.list,
          description: field.description,
        });
      }
    }
  }

  return refs.sort((a, b) => {
    if (a.fromType === b.fromType) return a.fieldName.localeCompare(b.fieldName);

    return a.fromType.localeCompare(b.fromType);
  });
}

function collectImplementors(
  atlas: OntologyAtlasResponse | null,
  interfaceName: string,
): OntologyAtlasTypeEntry[] {
  if (!atlas) return [];
  const entries = atlas.types ?? [];

  return entries.filter((entry) => (entry.type?.implements ?? []).includes(interfaceName));
}

export function TypeDetailBody({
  data,
  atlas,
  onSelectType,
}: {
  data: OntologyTypeResponse;
  atlas: OntologyAtlasResponse | null;
  onSelectType: (name: string) => void;
}) {
  const type = data.type;
  const typeName = type?.name ?? "";

  const isInterface = useMemo(() => {
    if (!typeName) return false;

    if (type?.role === "interface") return true;

    return (atlas?.interfaces ?? []).some((iface) => iface.name === typeName);
  }, [atlas, type?.role, typeName]);

  const implementors = useMemo(
    () => (isInterface ? collectImplementors(atlas, typeName) : []),
    [atlas, isInterface, typeName],
  );

  const inboundRefs = useMemo(
    () => (typeName ? collectInboundRefs(atlas, typeName) : []),
    [atlas, typeName],
  );

  if (!type?.name) {
    return <p>Type not found.</p>;
  }

  const fields = type.fields ?? [];
  const scalarFields = fields.filter((f) => !isRelationField(f));
  const relationFields = fields.filter(isRelationField);
  // A Direction column shows only when some row has one.
  const relationDirections = relationFields.some((f) => f.direction);
  const inboundDirections = inboundRefs.some((ref) => ref.direction);
  const examples = (data.notes ?? []).slice(0, 6);

  return (
    <>
      {type.description ? (
        <p className="ontology-type-detail__tagline">
          <Prose text={type.description} />
        </p>
      ) : null}
      <dl className="ontology-type-detail__stats">
        {data.issueCount ? (
          <div className="ontology-type-detail__stat--warn">
            <dt>Issues</dt>
            <dd>{data.issueCount}</dd>
          </div>
        ) : null}
        {type.locator ? (
          <div>
            <dt>Locator</dt>
            <dd>
              <code>{type.locator}</code>
            </dd>
          </div>
        ) : null}
        {type.propertyCase ? (
          <div>
            <dt>Property case</dt>
            <dd>{type.propertyCase}</dd>
          </div>
        ) : null}
        {type.implements?.length ? (
          <div className="ontology-type-detail__stat--wide">
            <dt>Implements</dt>
            <dd>
              <div className="ontology-type-detail__chip-row">
                {type.implements.map((iface) => (
                  <button
                    key={iface}
                    type="button"
                    className="ontology-type-detail__chip ontology-type-detail__chip--interactive"
                    onClick={() => onSelectType(iface)}
                  >
                    {humanizeName(iface)}
                  </button>
                ))}
              </div>
            </dd>
          </div>
        ) : null}
      </dl>

      {isInterface ? (
        <section className="ontology-type-detail__section ontology-type-detail__implementors">
          <h2>Implementors</h2>
          {implementors.length === 0 ? (
            <p className="ontology-type-detail__empty">
              No types declare <code>implements {typeName}</code> yet.
            </p>
          ) : (
            <ul className="ontology-type-detail__impl-grid">
              {implementors.map((entry) => {
                const implType = entry.type;

                if (!implType?.name) return null;
                const implName = implType.name;
                const implAccent = typeAccentColor(implName);

                return (
                  <li key={implName}>
                    <button
                      type="button"
                      className="ontology-type-detail__impl-card"
                      // SAFETY: React's CSSProperties omits `--*` custom properties; this object sets only one.
                      style={{ "--impl-accent": implAccent } as React.CSSProperties}
                      onClick={() => onSelectType(implName)}
                    >
                      <div className="ontology-type-detail__impl-head">
                        <span className="ontology-type-detail__impl-name">
                          <span className="ontology-type-detail__impl-dot" aria-hidden="true" />
                          <span className="ontology-type-detail__impl-title">
                            {humanizeName(implType.name)}
                          </span>
                          <code className="ontology-type-detail__ident-secondary">
                            {implType.name}
                          </code>
                        </span>
                        <span className="ontology-type-detail__impl-count">{entry.count ?? 0}</span>
                      </div>
                      {implType.description ? (
                        <p className="ontology-type-detail__impl-desc">
                          <Prose text={implType.description} />
                        </p>
                      ) : null}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </section>
      ) : null}

      {type.matches?.length || type.paths?.length ? (
        <section className="ontology-type-detail__section ontology-type-detail__matchers">
          <h2>How Rhizome recognizes it</h2>
          <div className="ontology-type-detail__matchers-grid">
            {type.paths?.length ? (
              <div>
                <h3>Paths</h3>
                <ul>
                  {type.paths.map((p) => (
                    <li key={p}>
                      <code>{p}</code>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
            {type.matches?.length ? (
              <div>
                <h3>Matches</h3>
                <ul>
                  {type.matches.map((m) => (
                    <li key={m}>
                      <code>{m}</code>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
          </div>
        </section>
      ) : null}

      {scalarFields.length ? (
        <section className="ontology-type-detail__section ontology-type-detail__fields">
          <h2>Fields</h2>
          <FieldTable fields={scalarFields} />
        </section>
      ) : null}

      {relationFields.length ? (
        <section className="ontology-type-detail__section ontology-type-detail__relations">
          <h2>Typed relations</h2>
          <table className="ontology-type-detail__table">
            <thead>
              <tr>
                <th className="ontology-type-detail__col-name">Field</th>
                <th className="ontology-type-detail__col-type">Target</th>
                {relationDirections ? <th>Direction</th> : null}
                <th>Description</th>
              </tr>
            </thead>
            <tbody>
              {relationFields.map((f) => (
                <tr key={f.name}>
                  <td>
                    <span>{humanizeName(f.name)}</span>{" "}
                    {humanizeName(f.name).toLowerCase() !== (f.name ?? "").toLowerCase() ? (
                      <code className="ontology-type-detail__ident-secondary">{f.name}</code>
                    ) : null}
                  </td>
                  <td>
                    {f.typeName ? (
                      <TargetChip
                        name={f.typeName}
                        list={f.list ?? false}
                        onSelect={onSelectType}
                      />
                    ) : (
                      "—"
                    )}
                  </td>
                  {relationDirections ? <td>{directionLabel(f.direction)}</td> : null}
                  <td>
                    <Prose text={f.description} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ) : null}

      {inboundRefs.length ? (
        <section className="ontology-type-detail__section ontology-type-detail__inbound">
          <h2>Referenced by</h2>
          <table className="ontology-type-detail__table">
            <thead>
              <tr>
                <th className="ontology-type-detail__col-type">From type</th>
                <th className="ontology-type-detail__col-name">Field</th>
                {inboundDirections ? <th>Direction</th> : null}
                <th>Description</th>
              </tr>
            </thead>
            <tbody>
              {inboundRefs.map((ref) => (
                <tr key={`${ref.fromType}.${ref.fieldName}`}>
                  <td>
                    <TargetChip
                      name={ref.fromType}
                      list={false}
                      onSelect={onSelectType}
                      role={ref.fromRole}
                    />
                  </td>
                  <td>
                    <code>
                      {ref.fieldName}
                      {ref.list ? "[]" : ""}
                    </code>
                  </td>
                  {inboundDirections ? <td>{directionLabel(ref.direction)}</td> : null}
                  <td>
                    <Prose text={ref.description} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ) : null}

      {type.companionDocs?.length ? (
        <section className="ontology-type-detail__section ontology-type-detail__docs">
          <h2>Companion docs</h2>
          <ul className="ontology-type-detail__doc-list">
            {type.companionDocs.map((doc) => (
              <li key={doc.path}>
                <a
                  className="ontology-type-detail__doc-link"
                  href={`/notes?note=${encodeURIComponent(doc.path)}`}
                >
                  <code>{doc.path}</code>
                  {doc.purpose ? (
                    <span className="ontology-type-detail__doc-purpose">{doc.purpose}</span>
                  ) : null}
                </a>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      <section className="ontology-type-detail__section ontology-type-detail__examples">
        <h2>
          Examples
          {data.count > examples.length ? (
            <a
              className="ontology-type-detail__see-all"
              href={buildNotesPath({ kind: "type", typeName })}
            >
              See all {data.count} →
            </a>
          ) : null}
        </h2>
        {examples.length === 0 ? (
          <p className="ontology-type-detail__empty">No notes of this type are indexed yet.</p>
        ) : (
          <ul className="ontology-type-detail__example-grid">
            {examples.map((note) => (
              <ExampleCard key={note.path} note={note} onSelectType={onSelectType} />
            ))}
          </ul>
        )}
      </section>
    </>
  );
}
