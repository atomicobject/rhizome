import "./ontology-refinements.css";

import type React from "react";

import { isJsonObject, type JsonObject, type JsonValue } from "../api/parse";

import type {
  EnumDoc,
  EnumValueDoc,
  OntologyTypeResponse,
  TypeDoc,
  TypeFieldDoc,
} from "../api/types";
import { humanizeName } from "../lib/labels";

import { Markdown } from "./markdown/Markdown";

export function SchemaPanel({ data }: { data: OntologyTypeResponse }) {
  const type = data.type;

  if (!type?.name) {
    return (
      <section className="ontology-type-detail__section ontology-type-detail__schema">
        <p className="ontology-type-detail__empty">No schema data available for this type.</p>
      </section>
    );
  }

  const enums = type.enums ?? [];
  const fields = type.fields ?? [];

  return (
    <section className="ontology-type-detail__section ontology-type-detail__schema">
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
            {humanizeName(field.kind)}
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
