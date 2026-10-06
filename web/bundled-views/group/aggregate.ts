// The Overview's aggregate reads (SPEC-0117): `GET /api/v1/ontology/shape`,
// one part per request, and the type summaries that name and classify every
// type. Everything that arrives as JSON is decoded here; the derivations never
// inspect raw JSON.
//
// - aggregatePath(part): the request for one part.
// - parseAggregate(json): the scope facts and whichever parts the response holds.
// - parseTypeSummaries(json): labels, display group, description, role, and
//   record count of every type.
import {
  isBoolean,
  isJsonObject,
  isNumber,
  isString,
  type JsonObject,
  type JsonValue,
} from "./api.ts";

/** The shape endpoint's name for untyped notes, in link pairs and target sets. */
export const UNTYPED = "__untyped__";

/**
 * The display group the API synthesizes from ungrouped roots, which the
 * display-groups contract names and sorts last.
 */
export const UNGROUPED = "Other";

export type AggregatePart = "members" | "links" | "folders";

export type ValueCount = { name: string; count: number };

export type StageCounts = { field: string; values: readonly ValueCount[] };

export type GapCount = { field: string; empty: number };

/** Records of a type grouped by the set of other types (and untyped notes) each links to. */
export type TargetSet = { types: readonly string[]; records: number };

/** Figures shared by a type and an interface. `lastChanged` is in epoch milliseconds. */
export type MemberStats = {
  name: string;
  count: number;
  issueCount: number;
  lastChanged: number | null;
  lifecycle: StageCounts | null;
  gaps: readonly GapCount[];
};

export type TypeFigures = MemberStats & { targetSets: readonly TargetSet[] };

export type InterfaceFigures = MemberStats & { implementors: readonly string[] };

export type MemberFigures = {
  types: ReadonlyMap<string, TypeFigures>;
  interfaces: ReadonlyMap<string, InterfaceFigures>;
  untyped: { count: number; links: number };
};

export type FieldCount = { type: string; field: string; count: number };

/** Links between two types, or among one type's records when `a === b`; `a <= b`. */
export type LinkPair = {
  a: string;
  b: string;
  links: number;
  relationLinks: number;
  plainLinks: number;
  fields: readonly FieldCount[];
};

export type FolderRow = {
  folder: string;
  total: number;
  untyped: number;
  typed: ReadonlyMap<string, number>;
  untypedLinksTo: ReadonlyMap<string, number>;
};

export type ScopeFacts = {
  rebuilding: boolean;
  totalNotes: number;
  typedNotes: number;
  untypedNotes: number;
  ambiguousNotes: number;
};

export type Aggregate = {
  facts: ScopeFacts;
  members: MemberFigures | null;
  links: readonly LinkPair[] | null;
  folders: readonly FolderRow[] | null;
};

export type TypeSummary = {
  name: string;
  label: string;
  pluralLabel: string;
  group: string | null;
  description: string;
  embedded: boolean;
  count: number;
};

export const aggregatePath = (part: AggregatePart) => `/api/v1/ontology/shape?parts=${part}`;

const number = (value: JsonValue | undefined) =>
  isNumber(value) && Number.isFinite(value) ? value : 0;

const text = (value: JsonValue | undefined) => (isString(value) ? value : "");

const objects = (value: JsonValue | undefined) =>
  Array.isArray(value) ? value.filter(isJsonObject) : [];

const strings = (value: JsonValue | undefined) =>
  Array.isArray(value) ? value.filter(isString) : [];

const counts = (value: JsonValue | undefined) =>
  new Map(
    isJsonObject(value)
      ? Object.entries(value).flatMap(([key, count]) =>
          isNumber(count) ? [[key, count] as const] : [],
        )
      : [],
  );

/** Unix seconds from the API, as epoch milliseconds; 0 or absent reads as never. */
const time = (value: JsonValue | undefined) => (number(value) > 0 ? number(value) * 1000 : null);

function parseLifecycle(value: JsonValue | undefined): StageCounts | null {
  if (!isJsonObject(value) || !text(value.field)) return null;

  return {
    field: text(value.field),
    values: objects(value.values).map((entry) => ({
      name: text(entry.name),
      count: number(entry.count),
    })),
  };
}

function parseStats(entry: JsonObject): MemberStats {
  return {
    name: text(entry.name),
    count: number(entry.count),
    issueCount: number(entry.issueCount),
    lastChanged: time(entry.lastChanged),
    lifecycle: parseLifecycle(entry.lifecycle),
    gaps: objects(entry.gaps).map((gap) => ({ field: text(gap.field), empty: number(gap.empty) })),
  };
}

function parseMembers(value: JsonValue | undefined): MemberFigures | null {
  if (!isJsonObject(value)) return null;
  const untyped = isJsonObject(value.untyped) ? value.untyped : {};

  return {
    types: new Map(
      objects(value.types).map((entry) => [
        text(entry.name),
        {
          ...parseStats(entry),
          targetSets: objects(entry.targetSets).map((set) => ({
            types: strings(set.types),
            records: number(set.records),
          })),
        },
      ]),
    ),
    interfaces: new Map(
      objects(value.interfaces).map((entry) => [
        text(entry.name),
        { ...parseStats(entry), implementors: strings(entry.implementors) },
      ]),
    ),
    untyped: { count: number(untyped.count), links: number(untyped.links) },
  };
}

function parseLinks(value: JsonValue | undefined): LinkPair[] | null {
  if (!isJsonObject(value)) return null;

  return objects(value.pairs).map((pair) => ({
    a: text(pair.a),
    b: text(pair.b),
    links: number(pair.links),
    relationLinks: number(pair.relationLinks),
    plainLinks: number(pair.plainLinks),
    fields: objects(pair.fields).map((field) => ({
      type: text(field.type),
      field: text(field.field),
      count: number(field.count),
    })),
  }));
}

function parseFolders(value: JsonValue | undefined): FolderRow[] | null {
  if (!isJsonObject(value)) return null;

  return objects(value.rows).map((row) => ({
    folder: text(row.folder),
    total: number(row.total),
    untyped: number(row.untyped),
    typed: counts(row.typed),
    untypedLinksTo: counts(row.untypedLinksTo),
  }));
}

/** Decode a shape response. A part the response leaves out reads as null. */
export function parseAggregate(json: JsonValue): Aggregate {
  const value: JsonObject = isJsonObject(json) ? json : {};

  return {
    facts: {
      rebuilding: isBoolean(value.rebuilding) && value.rebuilding,
      totalNotes: number(value.totalNotes),
      typedNotes: number(value.typedNotes),
      untypedNotes: number(value.untypedNotes),
      ambiguousNotes: number(value.ambiguousNotes),
    },
    members: parseMembers(value.members),
    links: parseLinks(value.links),
    folders: parseFolders(value.folders),
  };
}

/** Every type in `GET /api/v1/ontology/types`, by name. */
export function parseTypeSummaries(json: JsonValue): Map<string, TypeSummary> {
  return new Map(
    objects(json).flatMap((entry) => {
      const name = text(entry.name);

      if (!name) return [];
      const label = text(entry.label) || name;

      return [
        [
          name,
          {
            name,
            label,
            pluralLabel: text(entry.pluralLabel) || label,
            group: text(entry.displayGroup) || null,
            description: text(entry.description),
            embedded: entry.role === "embedded",
            count: number(entry.count),
          },
        ],
      ];
    }),
  );
}
