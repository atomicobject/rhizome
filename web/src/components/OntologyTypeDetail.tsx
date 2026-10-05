import { publicTypeName } from "../lib/typeNames";
import type React from "react";
import { useMemo, useState } from "react";

import { isJsonObject, type JsonObject, type JsonValue } from "../api/parse";
import { publicOntologyListItemRef } from "../api/client";
import type {
  EnumDoc,
  EnumValueDoc,
  OntologyAtlasResponse,
  OntologyAtlasTypeEntry,
  OntologyNoteListItem,
  OntologyTypeResponse,
  TypeDoc,
  TypeFieldDoc,
} from "../api/types";
import { typeAccentColor } from "../lib/typeAccent";
import { buildNoteWebHref } from "../lib/content";
import { Markdown } from "./markdown/Markdown";
import { notifyLocationChange } from "./locationStore";
import { useNotePreviewTrigger, type OpenNote } from "./notePreview/NoteLinkPreview";
import { buildNotesPath } from "./notesRoute";
import { useActiveEditSession } from "./useOntologyEditSession";
import { useOntologyTypeQuery } from "./useNotesQueries";

type DetailTab = "overview" | "guide" | "schema";

const TAB_ORDER: DetailTab[] = ["overview", "guide", "schema"];

const TAB_LABELS: Record<DetailTab, string> = {
  overview: "Overview",
  guide: "Authoring guide",
  schema: "Schema",
};

const openNoteTarget: OpenNote = (target, mode) => {
  const href = buildNoteWebHref(target);

  if (mode === "beside") {
    window.open(href, "_blank", "noopener,noreferrer");

    return;
  }

  window.history.pushState({}, "", href);
  notifyLocationChange();
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
      <header className="ontology-type-detail__hero">
        <button type="button" className="ontology-type-detail__back" onClick={onBack}>
          ← Atlas
        </button>
        <div className="ontology-type-detail__hero-body">
          <div className="ontology-type-detail__hero-title">
            <span className="ontology-type-detail__accent-dot" aria-hidden="true" />
            <h1>{name}</h1>
            {data?.type?.role ? (
              <span className="ontology-type-detail__role-pill">{data.type.role}</span>
            ) : null}
          </div>
          {data?.type?.description ? (
            <p className="ontology-type-detail__tagline">{data.type.description}</p>
          ) : null}
        </div>
      </header>

      {error && !data ? (
        <div className="ontology-type-detail__error" role="alert">
          Failed to load type: {error}{" "}
          <button type="button" onClick={() => typeQuery.refetch()}>
            Retry
          </button>
        </div>
      ) : !data ? (
        <p className="ontology-type-detail__loading">Loading…</p>
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
          <TabBar active={tab} onSelect={setTab} />
          {tab === "overview" ? (
            <TypeDetailBody data={data} atlas={atlas} onSelectType={onSelectType} />
          ) : null}
          {tab === "guide" ? <AuthoringGuidePanel data={data} /> : null}
          {tab === "schema" ? <SchemaPanel data={data} /> : null}
        </>
      )}
    </section>
  );
}

function TabBar({ active, onSelect }: { active: DetailTab; onSelect: (tab: DetailTab) => void }) {
  return (
    <div className="ontology-type-detail__tabs" role="tablist" aria-label="Type view">
      {TAB_ORDER.map((id) => {
        const selected = id === active;

        return (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={selected}
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
      <section
        className="ontology-type-detail__section ontology-type-detail__guide"
        role="tabpanel"
      >
        <p className="ontology-type-detail__empty">
          No authoring guide available for this type. Ensure the ontology schema is loaded and the
          type is declared.
        </p>
      </section>
    );
  }

  return (
    <section className="ontology-type-detail__section ontology-type-detail__guide" role="tabpanel">
      <div className="ontology-type-detail__markdown">
        <Markdown>{guide}</Markdown>
      </div>
    </section>
  );
}

function SchemaPanel({ data }: { data: OntologyTypeResponse }) {
  const type = data.type;

  if (!type?.name) {
    return (
      <section
        className="ontology-type-detail__section ontology-type-detail__schema"
        role="tabpanel"
      >
        <p className="ontology-type-detail__empty">No schema data available for this type.</p>
      </section>
    );
  }

  const enums = type.enums ?? [];
  const fields = type.fields ?? [];

  return (
    <section className="ontology-type-detail__section ontology-type-detail__schema" role="tabpanel">
      <TypeSchemaHeader type={type} />
      <GuidanceBlock guidance={guidanceFromType(type)} />
      <TypeMetaGrid type={type} />
      {(() => {
        const typeDirectives = Object.entries(type.annotations ?? {}).filter(
          ([name]) => name !== "guidance",
        );

        return typeDirectives.length > 0 ? (
          <div className="schema-directives">
            <h3>Directives</h3>
            <div className="schema-directive-list">
              {typeDirectives.map(([name, args]) => (
                <DirectiveChip key={name} name={name} args={isJsonObject(args) ? args : {}} />
              ))}
            </div>
          </div>
        ) : null;
      })()}
      {fields.length > 0 ? (
        <div className="schema-fields-block">
          <h3>Fields</h3>
          <ul className="schema-field-list">
            {fields.map((field) => (
              <SchemaFieldCard key={field.name} field={field} />
            ))}
          </ul>
        </div>
      ) : null}
      {enums.length > 0 ? (
        <div className="schema-enums-block">
          <h3>Enums</h3>
          <div className="schema-enum-list">
            {enums.map((enumDoc) => (
              <SchemaEnumCard key={enumDoc.name} enum={enumDoc} />
            ))}
          </div>
        </div>
      ) : null}
    </section>
  );
}

type GuidanceBundle = {
  summary?: string;
  meaning?: string;
  authoring?: string;
  agentImplications?: string;
};

function guidanceFromType(type: TypeDoc): GuidanceBundle {
  return {
    summary: valueOrUndefined(type.summary),
    meaning: valueOrUndefined(type.meaning),
    authoring: valueOrUndefined(type.authoring),
    agentImplications: valueOrUndefined(type.agentImplications),
  };
}

function guidanceFromField(field: TypeFieldDoc): GuidanceBundle {
  return {
    summary: valueOrUndefined(field.summary),
    meaning: valueOrUndefined(field.meaning),
    authoring: valueOrUndefined(field.authoring),
    agentImplications: valueOrUndefined(field.agentImplications),
  };
}

function guidanceFromEnumLike(value: EnumDoc | EnumValueDoc): GuidanceBundle {
  return {
    summary: valueOrUndefined(value.summary),
    meaning: valueOrUndefined(value.meaning),
    authoring: valueOrUndefined(value.authoring),
    agentImplications: valueOrUndefined(value.agentImplications),
  };
}

function valueOrUndefined(s?: string): string | undefined {
  const trimmed = (s ?? "").trim();

  return trimmed === "" ? undefined : trimmed;
}

function hasGuidance(g: GuidanceBundle): boolean {
  return Boolean(g.summary || g.meaning || g.authoring || g.agentImplications);
}

function TypeSchemaHeader({ type }: { type: TypeDoc }) {
  const chips: Array<{ label: string; value: string; kind?: string }> = [];

  if (type.role) chips.push({ label: "role", value: type.role });

  if (type.semantics) chips.push({ label: "semantics", value: type.semantics });

  if (type.locator) chips.push({ label: "locator", value: type.locator });

  if (type.propertyCase) chips.push({ label: "propertyCase", value: type.propertyCase });

  if (type.keyField) chips.push({ label: "keyField", value: type.keyField });

  return (
    <header className="schema-type-header">
      <div className="schema-type-signature">
        <span className="schema-type-kw">{type.role === "interface" ? "interface" : "type"}</span>
        <code className="schema-type-name">{type.name}</code>
        {type.implements?.length ? (
          <>
            <span className="schema-type-kw">implements</span>
            <span className="schema-type-implements">{type.implements.join(" & ")}</span>
          </>
        ) : null}
      </div>
      {chips.length > 0 ? (
        <div className="schema-chip-row">
          {chips.map((chip) => (
            <span key={chip.label} className="schema-chip" title={`${chip.label}: ${chip.value}`}>
              <span className="schema-chip__label">{chip.label}</span>
              <code className="schema-chip__value">{chip.value}</code>
            </span>
          ))}
        </div>
      ) : null}
      {type.description ? <p className="schema-description">{type.description}</p> : null}
    </header>
  );
}

function TypeMetaGrid({ type }: { type: TypeDoc }) {
  const rows: Array<{ label: string; body: React.ReactNode }> = [];

  if (type.label)
    rows.push({
      label: "Label",
      body: <code>{type.label}</code>,
    });

  if (type.pluralLabel)
    rows.push({
      label: "Plural label",
      body: <code>{type.pluralLabel}</code>,
    });

  if (type.color)
    rows.push({
      label: "Color",
      body: (
        <span className="schema-color-swatch">
          <span className="schema-color-dot" style={{ background: type.color }} />
          <code>{type.color}</code>
        </span>
      ),
    });

  if (type.paths?.length)
    rows.push({
      label: "Paths",
      body: (
        <ul className="schema-code-list">
          {type.paths.map((p) => (
            <li key={p}>
              <code>{p}</code>
            </li>
          ))}
        </ul>
      ),
    });

  if (type.matches?.length)
    rows.push({
      label: "Matches",
      body: (
        <ul className="schema-code-list">
          {type.matches.map((m) => (
            <li key={m}>
              <code>{m}</code>
            </li>
          ))}
        </ul>
      ),
    });

  if (type.companionDocs?.length)
    rows.push({
      label: "Companion docs",
      body: (
        <ul className="schema-companion-list">
          {type.companionDocs.map((doc) => (
            <li key={doc.path}>
              <code>{doc.path}</code>
              {doc.purpose ? (
                <span className="schema-companion-purpose"> · {doc.purpose}</span>
              ) : null}
            </li>
          ))}
        </ul>
      ),
    });

  if (rows.length === 0) return null;

  return (
    <dl className="schema-meta-grid">
      {rows.map((row) => (
        <div key={row.label} className="schema-meta-row">
          <dt>{row.label}</dt>
          <dd>{row.body}</dd>
        </div>
      ))}
    </dl>
  );
}

function GuidanceBlock({ guidance }: { guidance: GuidanceBundle }) {
  if (!hasGuidance(guidance)) return null;

  return (
    <div className="schema-guidance">
      {guidance.summary ? <p className="schema-guidance__summary">{guidance.summary}</p> : null}
      {guidance.meaning ? <GuidanceSection label="Meaning" body={guidance.meaning} /> : null}
      {guidance.authoring ? <GuidanceSection label="Authoring" body={guidance.authoring} /> : null}
      {guidance.agentImplications ? (
        <GuidanceSection label="Agent implications" body={guidance.agentImplications} />
      ) : null}
    </div>
  );
}

function GuidanceSection({ label, body }: { label: string; body: string }) {
  return (
    <div className="schema-guidance__section">
      <span className="schema-guidance__label">{label}</span>
      <div className="schema-guidance__body">
        <Markdown>{body}</Markdown>
      </div>
    </div>
  );
}

type SchemaDirective = { name: string; args: JsonObject };

function DirectiveChip({ name, args }: { name: string; args: JsonObject }) {
  const entries = Object.entries(args ?? {}).filter(
    ([, v]) => v !== undefined && v !== null && v !== "",
  );

  return (
    <span className="schema-directive">
      <span className="schema-directive__name">@{name}</span>
      {entries.length > 0 ? (
        <span className="schema-directive__args">
          (
          {entries.map(([key, value], i) => (
            <span key={key}>
              {i > 0 ? ", " : ""}
              <span className="schema-directive__arg-key">{key}</span>
              {": "}
              <code className="schema-directive__arg-val">{formatDirectiveValue(value)}</code>
            </span>
          ))}
          )
        </span>
      ) : null}
    </span>
  );
}

function formatDirectiveValue(value: JsonValue): string {
  if (Array.isArray(value)) return `[${value.map(formatDirectiveValue).join(", ")}]`;

  return JSON.stringify(value);
}

function fieldSignatureType(field: TypeFieldDoc): string {
  const inner = field.typeName || "?";
  const base = field.list ? `[${inner}]` : inner;

  return field.required ? `${base}!` : base;
}

function SchemaFieldCard({ field }: { field: TypeFieldDoc }) {
  const directives = collectFieldDirectives(field);
  const guidance = guidanceFromField(field);
  const enumDoc = field.enum;

  return (
    <li className="schema-field">
      <header className="schema-field__head">
        <code className="schema-field__sig">
          <span className="schema-field__name">{field.name}</span>
          <span className="schema-field__colon">: </span>
          <span className="schema-field__type">{fieldSignatureType(field)}</span>
        </code>
        {field.kind ? (
          <span className={`schema-field__kind schema-field__kind--${field.kind}`}>
            {field.kind}
          </span>
        ) : null}
      </header>
      {directives.length > 0 ? (
        <div className="schema-field__directives">
          {directives.map((d) => (
            <DirectiveChip key={d.name} name={d.name} args={d.args} />
          ))}
        </div>
      ) : null}
      {field.description && !guidance.summary ? (
        <p className="schema-field__desc">{field.description}</p>
      ) : null}
      <GuidanceBlock guidance={guidance} />
      {enumDoc?.name && (enumDoc.values?.length ?? 0) > 0 ? (
        <div className="schema-field__enum-inline">
          <span className="schema-field__enum-label">Values</span>
          <div className="schema-field__enum-chips">
            {(enumDoc.values ?? []).map((v) => (
              <code key={v.name} className="schema-enum-value-chip">
                {v.name}
              </code>
            ))}
          </div>
        </div>
      ) : null}
    </li>
  );
}

function collectFieldDirectives(field: TypeFieldDoc): SchemaDirective[] {
  const out: SchemaDirective[] = [];
  const seen = new Set<string>();
  const fromAnnotations = field.annotations ?? {};

  for (const [name, args] of Object.entries(fromAnnotations)) {
    if (name === "guidance") continue;
    out.push({ name, args: isJsonObject(args) ? args : {} });
    seen.add(name);
  }

  // Synthesize directive summaries from typed columns when annotations are absent.
  if (field.kind === "link" && !seen.has("link")) {
    const args: JsonObject = {};

    if (field.direction) args.direction = field.direction;

    if (field.inverse) args.inverse = field.inverse;
    out.push({ name: "link", args });
  }

  if (field.kind === "reverse" && !seen.has("reverse")) {
    out.push({ name: "reverse", args: field.reverseField ? { field: field.reverseField } : {} });
  }

  if (field.kind === "neighbor" && !seen.has("neighbors")) {
    const args: JsonObject = {};

    if (field.direction) args.direction = field.direction;

    if (field.scope) args.scope = field.scope;

    if (field.includeBodyLinks) args.includeBodyLinks = true;

    if (field.includeBacklinks) args.includeBacklinks = true;
    out.push({ name: "neighbors", args });
  }

  if (field.kind === "section" && !seen.has("section")) {
    const args: JsonObject = {};

    if (field.sectionHeading) args.heading = field.sectionHeading;

    if (field.sectionLevel) args.level = field.sectionLevel;

    if (field.sectionRequired) args.required = true;

    if (field.sectionDisplay) args.display = field.sectionDisplay;
    out.push({ name: "section", args });
  }

  if (
    (field.kind === "scalar" || field.kind === "field") &&
    !seen.has("field") &&
    (field.source || field.sourceKind)
  ) {
    const args: JsonObject = {};

    if (field.source) args.source = field.source;

    if (field.sourceKind) args.sourceKind = field.sourceKind;

    if (field.sourceAliases?.length) args.aliases = field.sourceAliases;
    out.push({ name: "field", args });
  }

  if (field.semantics && !seen.has("semantics")) {
    out.push({ name: "semantics", args: { kind: field.semantics } });
  }

  if (isJsonObject(field.policy)) {
    out.push({ name: "policy", args: field.policy });
  }

  return out;
}

function SchemaEnumCard({ enum: enumDoc }: { enum: EnumDoc }) {
  const guidance = guidanceFromEnumLike(enumDoc);
  const values = enumDoc.values ?? [];

  return (
    <div className="schema-enum">
      <header className="schema-enum__head">
        <span className="schema-type-kw">enum</span>
        <code className="schema-enum__name">{enumDoc.name}</code>
      </header>
      <GuidanceBlock guidance={guidance} />
      {values.length > 0 ? (
        <ul className="schema-enum-values">
          {values.map((v) => {
            const vGuidance = guidanceFromEnumLike(v);

            return (
              <li key={v.name} className="schema-enum-value">
                <code className="schema-enum-value__name">{v.name}</code>
                {vGuidance.summary ? (
                  <span className="schema-enum-value__summary">{vGuidance.summary}</span>
                ) : null}
                {vGuidance.meaning || vGuidance.authoring || vGuidance.agentImplications ? (
                  <div className="schema-enum-value__guidance">
                    <GuidanceBlock
                      guidance={{
                        meaning: vGuidance.meaning,
                        authoring: vGuidance.authoring,
                        agentImplications: vGuidance.agentImplications,
                      }}
                    />
                  </div>
                ) : null}
              </li>
            );
          })}
        </ul>
      ) : null}
    </div>
  );
}

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

function TypeDetailBody({
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
  const examples = (data.notes ?? []).slice(0, 6);

  return (
    <>
      <dl className="ontology-type-detail__stats">
        <div>
          <dt>Notes</dt>
          <dd>{data.count}</dd>
        </div>
        {data.issueCount ? (
          <div className="ontology-type-detail__stat--warn">
            <dt>Issues</dt>
            <dd>{data.issueCount}</dd>
          </div>
        ) : null}
        {type.role ? (
          <div>
            <dt>Role</dt>
            <dd>{type.role}</dd>
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
                    {iface}
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
                        <span className="ontology-type-detail__impl-name">{implType.name}</span>
                        <span className="ontology-type-detail__impl-count">{entry.count ?? 0}</span>
                      </div>
                      {implType.description ? (
                        <p className="ontology-type-detail__impl-desc">{implType.description}</p>
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
                <th>Field</th>
                <th>Target</th>
                <th>Direction</th>
                <th>Description</th>
              </tr>
            </thead>
            <tbody>
              {relationFields.map((f) => (
                <tr key={f.name}>
                  <td>
                    <code>{f.name}</code>
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
                  <td>{f.direction || "—"}</td>
                  <td>{f.description || ""}</td>
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
                <th>From type</th>
                <th>Field</th>
                <th>Direction</th>
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
                  <td>{ref.direction || "—"}</td>
                  <td>{ref.description || ""}</td>
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

function TargetChip({
  name,
  list,
  onSelect,
  role,
}: {
  name: string;
  list: boolean;
  onSelect: (name: string) => void;
  role?: string;
}) {
  const accent = typeAccentColor(name);

  return (
    <button
      type="button"
      className="ontology-type-detail__target-chip"
      // SAFETY: React's CSSProperties omits `--*` custom properties; this object sets only one.
      style={{ "--target-accent": accent } as React.CSSProperties}
      onClick={() => onSelect(name)}
      title={role ? `${role}: ${name}` : name}
    >
      <span className="ontology-type-detail__target-dot" aria-hidden="true" />
      {name}
      {list ? "[]" : ""}
    </button>
  );
}

function FieldTable({ fields }: { fields: TypeFieldDoc[] }) {
  return (
    <table className="ontology-type-detail__table">
      <thead>
        <tr>
          <th>Name</th>
          <th>Type</th>
          <th>Required</th>
          <th>Source</th>
          <th>Description</th>
        </tr>
      </thead>
      <tbody>
        {fields.map((f) => (
          <tr key={f.name}>
            <td>
              <code>{f.name}</code>
            </td>
            <td>
              {f.typeName || f.kind || "string"}
              {f.list ? "[]" : ""}
              {f.enumValues?.length ? <small> ({f.enumValues.join(" | ")})</small> : null}
            </td>
            <td>{f.required ? "yes" : "no"}</td>
            <td>{f.source || ""}</td>
            <td>{f.description || ""}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function ExampleCard({
  note,
  onSelectType,
}: {
  note: OntologyNoteListItem;
  onSelectType: (name: string) => void;
}) {
  const target = publicOntologyListItemRef(note);
  const href = buildNoteWebHref(target);

  const previewTrigger = useNotePreviewTrigger<HTMLAnchorElement>({
    target,
    open: openNoteTarget,
  });

  const resolved = publicTypeName(note.resolvedType);
  const resolvedAccent = resolved ? typeAccentColor(resolved) : undefined;
  const tags = (note.tags ?? []).slice(0, 3);

  return (
    <li className="ontology-type-detail__example-card">
      <a
        {...previewTrigger.triggerProps}
        className="ontology-type-detail__example-title"
        href={href}
        onClick={previewTrigger.close}
      >
        {note.title || note.path}
      </a>
      {previewTrigger.preview}
      <div className="ontology-type-detail__example-meta">
        <code>{note.path}</code>
      </div>
      <div className="ontology-type-detail__example-chips">
        {resolved ? (
          <button
            type="button"
            className="ontology-type-detail__target-chip"
            // SAFETY: React's CSSProperties omits `--*` custom properties; this object sets only one.
            style={
              {
                "--target-accent": resolvedAccent,
              } as React.CSSProperties
            }
            onClick={() => onSelectType(resolved)}
          >
            <span className="ontology-type-detail__target-dot" aria-hidden="true" />
            {resolved}
          </button>
        ) : null}
        {tags.map((tag) => (
          <span key={tag} className="ontology-type-detail__tag-chip">
            #{tag}
          </span>
        ))}
        {note.relationCount ? (
          <span className="ontology-type-detail__example-stat">{note.relationCount} rel</span>
        ) : null}
        {note.hasIssues ? (
          <span className="ontology-type-detail__example-stat ontology-type-detail__example-stat--warn">
            ⚠ issues
          </span>
        ) : null}
      </div>
    </li>
  );
}
