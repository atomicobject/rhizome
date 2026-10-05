// Requests and response parsing for the group views' public APIs. Everything
// that arrives as JSON is decoded here into the named types below; the rest of
// the module never inspects raw JSON.
//
// - recordsQuery(docs, guidePath, options): one GraphQL document reading every
//   record of the given concrete types, newest first and aliased by type name,
//   plus the guide.
// - parseRecords(data, docs): the decoded records per type, and the guide.
// - parseTargetViews(catalog, kind, name): authored views one subject offers.
// - parseTableChoice(catalog, kind, name): a collection's standard Table choice.
// - parseTypeLabels(summaries): labels and display groups of every ontology type.
import type { TypeDoc, TypeFieldDoc } from "@rhizome/kit";

export type JsonValue = string | number | boolean | null | JsonValue[] | JsonObject;

export type JsonObject = { [key: string]: JsonValue };

/** Records read per concrete type; one more is requested to detect truncation. */
export const RECORD_CAP = 500;

/** Notes read per record for a group's boundary, linked in either direction. */
export const NEIGHBOR_CAP = 200;

/**
 * Notes read per record for a collection's links in. Lower than a group's cap:
 * a hub type's records can each have hundreds of backlinks.
 */
export const INBOUND_CAP = 50;

/**
 * Targets read per record of a list reverse field, for its most common
 * targets; `<field>Count` counts them all. With `INBOUND_CAP`, a collection
 * reads at most `RECORD_CAP * (INBOUND_CAP + REVERSE_SAMPLE * reverse fields)`
 * linked notes per type, besides authored forward links.
 */
export const REVERSE_SAMPLE = 20;

/** The alias the guide note is read under. */
const GUIDE_ALIAS = "groupGuide";

/** A record's canonical identity, passed unchanged to `openNode`. */
export type RecordRef = {
  notePath: string;
  kind: string;
  fragment?: string;
  nodeId?: string;
  typeName?: string;
  structuralFingerprint?: string;
};

/**
 * A record at the other end of a link or neighbor edge. `key` matches a group
 * record's key: the note path, or `path#node` for an embedded node.
 */
export type LinkedNote = { key: string; path: string; title: string; type: string | null };

export type FieldValue = string | number | boolean | readonly string[] | null;

/** One record as read, before the model assigns it to a member. */
export type ParsedRecord = {
  key: string;
  ref: RecordRef;
  path: string;
  title: string;
  type: string;
  updatedAt: number | null;
  issueCount: number;
  /** Selected scalar and enum fields, plus the body of a Section summary. */
  values: ReadonlyMap<string, FieldValue>;
  /** Forward link fields, each with its targets. */
  links: ReadonlyMap<string, readonly LinkedNote[]>;
  /**
   * The profile's reverse fields, each with records linking in: at most
   * `REVERSE_SAMPLE` of a list field's. Empty unless read.
   */
  reverse: ReadonlyMap<string, readonly LinkedNote[]>;
  /**
   * How many records link in through each reverse field: the generated count,
   * else the targets read, which is one past the sample when there are more.
   */
  reverseCounts: ReadonlyMap<string, number>;
  /** Notes linking to or from the record, by typed or body links; empty for embedded nodes. */
  neighbors: readonly LinkedNote[];
  /** The record has more neighbors than were read, so `neighbors` is partial. */
  neighborsTruncated: boolean;
};

export type RecordPage = { records: readonly ParsedRecord[]; truncated: boolean };

export type GuideNote = { path: string; title: string; summary: string | null };

export type ParsedRecords = { pages: ReadonlyMap<string, RecordPage>; guide: GuideNote | null };

export type GroupView = { id: string; name: string; description: string | null };

export type TypeLabels = { label: string; pluralLabel: string; group: string | null };

export function isString(value: JsonValue | undefined): value is string;
export function isString(value: unknown): value is string;
export function isString(value: unknown): value is string {
  return typeof value === "string";
}

const isNumber = (value: JsonValue | undefined): value is number => typeof value === "number";

export function isBoolean(value: JsonValue | undefined): value is boolean;
export function isBoolean(value: unknown): value is boolean;
export function isBoolean(value: unknown): value is boolean {
  return typeof value === "boolean";
}

export function isJsonObject(value: JsonValue | undefined): value is JsonObject;
export function isJsonObject(value: unknown): value is JsonObject;
export function isJsonObject(value: unknown): value is JsonObject {
  return (
    value !== undefined && value !== null && !Array.isArray(value) && typeof value === "object"
  );
}

const text = (value: JsonValue | undefined) => (isString(value) ? value : null);

const objects = (value: JsonValue | undefined) =>
  Array.isArray(value) ? value.filter(isJsonObject) : isJsonObject(value) ? [value] : [];

const lowerFirst = (name: string) => name.charAt(0).toLowerCase() + name.slice(1);

const isDateType = (field: TypeFieldDoc) =>
  field.typeName === "Date" || field.typeName === "DateTime";

/**
 * Scalar fields a group view reads: the summary, every enum (tones can mark any
 * enum field), KEY fields, and dates. Other scalars stay unread.
 */
export function selectedScalars(doc: TypeDoc): TypeFieldDoc[] {
  return doc.fields.filter(
    (field) =>
      field.kind === "enum" ||
      (field.kind === "scalar" &&
        (field.name === doc.summaryField ||
          field.display?.importance === "KEY" ||
          isDateType(field))),
  );
}

// A Section summary is authored prose, projected into the same display value
// as a scalar summary after requesting its body explicitly.
const summarySection = (doc: TypeDoc) =>
  doc.fields.find(
    (field) => field.name === doc.summaryField && field.kind === "section" && !field.list,
  );

const linkFields = (doc: TypeDoc) => doc.fields.filter((field) => field.kind === "link");

const reverseFields = (doc: TypeDoc) => doc.profile?.reverseFields ?? [];

const hasNeighbors = (doc: TypeDoc) => doc.role === undefined || doc.role === "NOTE";

/**
 * Records most recently changed first, so a type past `RECORD_CAP` keeps the
 * records recent changes, in-motion work, and stale signals read.
 */
const NEWEST_FIRST = 'sort: [{ field: "updatedAt", direction: desc }]';

const NODE_TARGET = "{ ... on Node { ref { kind fragment nodeId } path title resolvedType } }";

/** A list reverse field, which takes `first`. */
const isReverseList = (doc: TypeDoc, name: string) =>
  doc.fields.some((field) => field.name === name && field.list === true);

/** The type leaves `<field>Count` to the generated count rather than authoring it. */
const hasGeneratedCount = (doc: TypeDoc, name: string) =>
  isReverseList(doc, name) && !doc.fields.some((field) => field.name === `${name}Count`);

/** Response names a record selects besides the type's own fields. */
const RECORD_NAMES = ["ref", "path", "title", "updatedAt", "issueCount", "neighborhood"];

/**
 * The aliases each list reverse field's generated `<field>Count` is read
 * under: `<field>__generatedCount`, numbered on until it matches no field of
 * the type, no other name the record selects, and no other alias. An authored
 * field never reads as the count, and the query never names one key twice.
 */
function countAliases(doc: TypeDoc): ReadonlyMap<string, string> {
  const taken = new Set([...RECORD_NAMES, ...doc.fields.map((field) => field.name)]);
  const aliases = new Map<string, string>();

  for (const name of reverseFields(doc)) {
    if (!hasGeneratedCount(doc, name)) continue;
    let alias = `${name}__generatedCount`;

    for (let suffix = 2; taken.has(alias); suffix += 1) alias = `${name}__generatedCount${suffix}`;
    taken.add(alias);
    aliases.set(name, alias);
  }

  return aliases;
}

/**
 * A reverse field's sample of targets and, when generated, its count. Without
 * one, a list field reads one target past the sample, which says the sample
 * is partial.
 */
function reverseSelection(doc: TypeDoc, name: string, aliases: ReadonlyMap<string, string>) {
  const alias = aliases.get(name);

  if (alias) return [`${alias}: ${name}Count`, `${name}(first: ${REVERSE_SAMPLE}) ${NODE_TARGET}`];

  return isReverseList(doc, name)
    ? [`${name}(first: ${REVERSE_SAMPLE + 1}) ${NODE_TARGET}`]
    : [`${name} ${NODE_TARGET}`];
}

export type RecordsOptions = {
  /** Read the profile's reverse fields. */
  reverse?: boolean;
  /** Which links `neighbors` follows: both ways (the default) or only links in. */
  neighbors?: "BOTH" | "INBOUND";
};

// `neighborhood` rather than `connected`, which reads only body links and
// backlinks and so misses typed links in either direction.
const neighborSelection = (direction: "BOTH" | "INBOUND") =>
  `neighborhood(direction: ${direction}, first: ${direction === "INBOUND" ? INBOUND_CAP : NEIGHBOR_CAP}) { truncated nodes { ... on Node { path title resolvedType } } }`;

function typeSelection(doc: TypeDoc, options: RecordsOptions) {
  const aliases = countAliases(doc);

  const fields = [
    "ref { notePath kind fragment nodeId typeName structuralFingerprint: structural }",
    "path title updatedAt issueCount",
    ...(hasNeighbors(doc) ? [neighborSelection(options.neighbors ?? "BOTH")] : []),
    ...selectedScalars(doc).map((field) => field.name),
    ...(summarySection(doc) ? [`${doc.summaryField} { content }`] : []),
    ...linkFields(doc).map((field) => `${field.name} ${NODE_TARGET}`),
    ...(options.reverse
      ? reverseFields(doc).flatMap((name) => reverseSelection(doc, name, aliases))
      : []),
  ];

  return `  ${doc.name}: ${lowerFirst(doc.name)}(first: ${RECORD_CAP + 1}, ${NEWEST_FIRST}) {\n    ${fields.join("\n    ")}\n  }`;
}

/**
 * One query for every record of `docs`' types, each aliased by its type name.
 * The guide note is read with the `$guide` variable when `guidePath` is set.
 * Link targets use a `Node` fragment because a link may be typed by an
 * interface, such as a spec interface, that declares no title of its own.
 */
export function recordsQuery(
  docs: readonly TypeDoc[],
  guidePath: string | null,
  options: RecordsOptions = {},
) {
  const selections = docs.map((doc) => typeSelection(doc, options));

  if (guidePath === null) return `query GroupRecords {\n${selections.join("\n")}\n}`;

  const guide = `  ${GUIDE_ALIAS}: note(path: $guide) { path title frontmatter }`;

  return `query GroupRecords($guide: String) {\n${[...selections, guide].join("\n")}\n}`;
}

/** A record's key: its note path, or `path#node` for an embedded node. */
function recordKey(path: string, ref: JsonValue | undefined) {
  if (!isJsonObject(ref) || ref.kind === "NOTE") return path;
  const anchor = text(ref.nodeId) ?? text(ref.fragment);

  return anchor ? `${path}#${anchor}` : path;
}

function parseLinked(value: JsonObject): LinkedNote | null {
  const path = text(value.path);

  if (path === null) return null;

  return {
    key: recordKey(path, value.ref),
    path,
    title: text(value.title) ?? path,
    type: text(value.resolvedType),
  };
}

const linkedNotes = (value: JsonValue | undefined) =>
  objects(value).flatMap((entry) => parseLinked(entry) ?? []);

function parseValue(value: JsonValue | undefined): FieldValue {
  if (isString(value) || isNumber(value) || isBoolean(value)) return value;

  return Array.isArray(value) ? value.filter(isString) : null;
}

const OPTIONAL_REF_FIELDS = ["fragment", "nodeId", "typeName", "structuralFingerprint"] as const;

/**
 * The record's ref with absent fields left out: the workspace accepts only
 * plain JSON refs, and `postMessage` would carry an `undefined` field along.
 */
function parseRef(value: JsonValue | undefined, path: string): RecordRef {
  const ref: JsonObject = isJsonObject(value) ? value : {};

  const parsed: RecordRef = {
    notePath: text(ref.notePath) ?? path,
    kind: text(ref.kind) ?? "NOTE",
  };

  for (const name of OPTIONAL_REF_FIELDS) {
    const field = text(ref[name]);

    if (field !== null) parsed[name] = field;
  }

  return parsed;
}

function parseTime(value: JsonValue | undefined) {
  const time = isString(value) ? Date.parse(value) : Number.NaN;

  return Number.isNaN(time) ? null : time;
}

function parseRecord(
  value: JsonObject,
  doc: TypeDoc,
  aliases: ReadonlyMap<string, string>,
): ParsedRecord | null {
  const path = text(value.path) ?? text(isJsonObject(value.ref) ? value.ref.notePath : null);

  if (path === null) return null;

  const section = summarySection(doc);
  const sectionValue = section ? value[section.name] : null;

  const summary: [string, FieldValue][] = section
    ? [[section.name, text(isJsonObject(sectionValue) ? sectionValue.content : null)]]
    : [];

  return {
    key: recordKey(path, value.ref),
    ref: parseRef(value.ref, path),
    path,
    title: text(value.title) ?? path,
    type: doc.name,
    updatedAt: parseTime(value.updatedAt),
    issueCount: isNumber(value.issueCount) ? value.issueCount : 0,
    values: new Map([
      ...selectedScalars(doc).map((field): [string, FieldValue] => [
        field.name,
        parseValue(value[field.name]),
      ]),
      ...summary,
    ]),
    links: new Map(linkFields(doc).map((field) => [field.name, linkedNotes(value[field.name])])),
    reverse: new Map(
      reverseFields(doc).map((name) => [name, linkedNotes(value[name]).slice(0, REVERSE_SAMPLE)]),
    ),
    reverseCounts: new Map(
      reverseFields(doc).map((name) => {
        const alias = aliases.get(name);
        const count = alias === undefined ? undefined : value[alias];

        return [name, isNumber(count) ? count : linkedNotes(value[name]).length];
      }),
    ),
    neighbors: linkedNotes(isJsonObject(value.neighborhood) ? value.neighborhood.nodes : null),
    neighborsTruncated: isJsonObject(value.neighborhood) && value.neighborhood.truncated === true,
  };
}

function parseGuide(value: JsonValue | undefined): GuideNote | null {
  if (!isJsonObject(value)) return null;
  const path = text(value.path);

  if (path === null) return null;
  const frontmatter: JsonObject = isJsonObject(value.frontmatter) ? value.frontmatter : {};

  return { path, title: text(value.title) ?? path, summary: text(frontmatter.summary) };
}

/** Decode a `recordsQuery` response. A type absent from `data` reads as empty. */
export function parseRecords(data: JsonObject, docs: readonly TypeDoc[]): ParsedRecords {
  const pages = new Map<string, RecordPage>();

  for (const doc of docs) {
    const rows = objects(data[doc.name]);
    const aliases = countAliases(doc);

    const records = rows
      .slice(0, RECORD_CAP)
      .flatMap((row) => parseRecord(row, doc, aliases) ?? []);

    pages.set(doc.name, { records, truncated: rows.length > RECORD_CAP });
  }

  return { pages, guide: parseGuide(data[GUIDE_ALIAS]) };
}

/** The bundled views, which the page's switcher already offers beside these. */
const BUNDLED_VIEW_IDS = new Set([
  "group.briefing",
  "group.trace",
  "group.sections",
  "type.briefing",
  "interface.briefing",
]);

export type SubjectKind = "group" | "type" | "interface";

const findTarget = (catalog: JsonValue, kind: SubjectKind, name: string) =>
  isJsonObject(catalog)
    ? objects(catalog.targets).find((entry) => entry.kind === kind && entry.name === name)
    : undefined;

/**
 * The views a subject's switcher offers in `GET /api/v1/views`, in its order:
 * those mounted on it and generic views alike, without Overview, the bundled
 * views, and a collection's generated layouts.
 */
export function parseTargetViews(catalog: JsonValue, kind: SubjectKind, name: string): GroupView[] {
  if (!isJsonObject(catalog)) return [];

  const target = findTarget(catalog, kind, name);

  const descriptions = new Map(
    objects(catalog.views).map((view) => [text(view.id), text(view.description)]),
  );

  const seen = new Set<string>();

  return objects(target?.choices).flatMap((choice) => {
    const id = text(choice.viewId);

    if (id === null || BUNDLED_VIEW_IDS.has(id) || id.startsWith("generated.") || seen.has(id))
      return [];
    seen.add(id);

    return [{ id, name: text(choice.name) ?? id, description: descriptions.get(id) ?? null }];
  });
}

/**
 * The id of a collection's standard Table choice, generated or replacing,
 * which `openView` opens as the collection's presentation. A choice id rather
 * than the view's id, which would open the view's default layout.
 */
export function parseTableChoice(catalog: JsonValue, kind: SubjectKind, name: string) {
  const table = objects(findTarget(catalog, kind, name)?.choices).find(
    (choice) => choice.renderer === "table" && choice.custom !== true,
  );

  return text(table?.id);
}

/** Labels and display groups from `GET /api/v1/ontology/types`, by type name. */
export function parseTypeLabels(summaries: JsonValue): Map<string, TypeLabels> {
  const labels = new Map<string, TypeLabels>();

  for (const entry of objects(summaries)) {
    const name = text(entry.name);

    if (name === null) continue;
    const label = text(entry.label) ?? name;

    labels.set(name, {
      label,
      pluralLabel: text(entry.pluralLabel) ?? label,
      group: text(entry.displayGroup) || null,
    });
  }

  return labels;
}
