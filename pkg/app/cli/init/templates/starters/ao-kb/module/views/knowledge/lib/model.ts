/** Product-model records for the Knowledge views (SPEC-0133 R7). Pure and kit-free so
 * `node --test` can import it. Relationships come from native edges; this module only
 * attributes them back to the authored values that declared them. */
import { dateNumber } from "./knowledge.ts";
import type { Assertion } from "./knowledge.ts";
import type { Ref } from "./refs.ts";

/** A native RECORD source value (`workspace.source.value`), in authored order. */
export type SourceValue = {
  kind: string;
  scalar?: string | null;
  entries?: { key: string; value: SourceValue }[];
  items?: SourceValue[];
};

export const EVIDENCED_BY = "evidenced-by";
/** AO records referenced code as text paths in these fields, not as model edges. */
export const CODE_FIELD = "implements";

/** One `@references` declaration from the type documentation, keyed by authored path. */
export type ReferenceSpec = {
  path: string;
  to: string[];
  values: string;
  edge?: { name: string; direction: string };
};
export type TypeSpec = {
  label: string;
  references: ReferenceSpec[];
  /** The authored identity field and title fallbacks the header already shows. */
  keyField?: string;
  titleFields: string[];
};

/** The label and reference declarations in `GET /api/v1/ontology/types/{name}`. */
export function typeSpec(response: unknown, name: string): TypeSpec {
  const type = (response as { type?: Record<string, unknown> } | null)?.type ?? {};
  const fields = Array.isArray(type.fields) ? (type.fields as Record<string, any>[]) : [];
  const source = (type.annotations as Record<string, any> | undefined)?.source;
  return {
    label: typeof type.label === "string" && type.label ? type.label : name,
    keyField: typeof source?.key?.field === "string" ? source.key.field : undefined,
    titleFields: Array.isArray(source?.title) ? source.title : [],
    references: fields.flatMap((f) => {
      const ref = f.annotations?.references;
      const path = f.annotations?.field?.path ?? f.sourcePath;
      return ref && typeof path === "string"
        ? [{ path, to: ref.to ?? [], values: ref.values ?? "ELEMENTS", edge: ref.edge }]
        : [];
    }),
  };
}
export const isEvidence = (spec: ReferenceSpec) =>
  spec.to.includes("Assertion") && spec.edge?.name === EVIDENCED_BY;

export const entries = (value?: SourceValue | null) => value?.entries ?? [];
export function scalars(value?: SourceValue | null): string[] {
  if (!value) return [];
  if (value.kind === "array") return (value.items ?? []).flatMap(scalars);
  return value.kind === "object" || value.scalar == null ? [] : [value.scalar];
}
/** Every scalar and mapping key inside a value: the atoms an authored reference can name. */
export function atoms(value?: SourceValue | null): string[] {
  if (!value) return [];
  if (value.kind === "array") return (value.items ?? []).flatMap(atoms);
  if (value.kind === "object")
    return (value.entries ?? []).flatMap((e) => [e.key, ...atoms(e.value)]);
  return value.scalar == null ? [] : [value.scalar];
}
/** The authored entry that names `key`, or undefined. */
export function declaringEntry(
  value: SourceValue | null | undefined,
  key: string | null | undefined,
) {
  return key ? entries(value).find((e) => atoms(e.value).includes(key)) : undefined;
}

export type ModelNode = { key: string; aoValue?: string | null; value?: SourceValue | null };
/** Which end of a stored edge authored it. A RECORD declaration stores its edge either
 * way round (an INBOUND field stores target to declarer), so the declarer is the end whose
 * authored value names the other end's key. Falls back to the stored source. */
export function declarer(from: ModelNode, to: ModelNode) {
  if (declaringEntry(from.value, to.aoValue)) return from.key;
  if (declaringEntry(to.value, from.aoValue)) return to.key;
  return from.key;
}

/** An exact repository-relative code path, or undefined when it could name a file
 * outside the configured roots. Never normalised or matched by basename. */
export function codePath(raw: string) {
  const path = raw.trim();
  return !path ||
    path.startsWith("/") ||
    /^[a-z][a-z0-9+.-]*:/i.test(path) ||
    path.split(/[\\/]/).some((part) => part === "..")
    ? undefined
    : path;
}

export type Sync = "later" | "not later" | "unknown";
/** Compare a claim's recorded updatedAt with a recorded sync date; incomparable is unknown. */
export function syncCompare(updatedAt?: string | null, synced?: string | null): Sync {
  const a = dateNumber(updatedAt),
    b = dateNumber(synced);
  return !Number.isFinite(a) || !Number.isFinite(b) ? "unknown" : a > b ? "later" : "not later";
}

/** A model record as the Knowledge views read it. */
export type ModelRecord = {
  key: string;
  ref: Ref;
  title: string;
  typeName: string;
  aoValue?: string | null;
  value?: SourceValue | null;
  /** Paths of the assertions its native evidenced-by edges reach. */
  evidence: string[];
  /** The evidence read hit its cap, so an unmatched declared ID was not checked. */
  evidenceTruncated?: boolean;
};
export type ModelFile = { notePath: string; synced?: string | null };

/** Declared evidence values in authored order, each with its resolved assertion or none. */
export function declaredEvidence(
  record: ModelRecord,
  spec: TypeSpec | undefined,
  assertions: Assertion[],
) {
  const reached = assertions.filter((a) => record.evidence.includes(a.path));
  return entries(record.value)
    .filter((e) => spec?.references.some((r) => r.path === e.key && isEvidence(r)))
    .flatMap((e) =>
      scalars(e.value).map((id) => ({
        field: e.key,
        id,
        assertion: reached.find((a) => a.aoId === id),
        unread: !reached.some((a) => a.aoId === id) && !!record.evidenceTruncated,
      })),
    );
}

export type ModelSync = {
  file: ModelFile;
  rows: { record: ModelRecord; changed: Assertion[]; unknown: Assertion[] }[];
};
export type ModelDangling = { record: ModelRecord; field: string; id: string };

/** R7 model observations: knowledge updated since each file's `synced`, grouped by file,
 * and declared evidence that resolves to no assertion in the selected product. */
export function modelObservations(
  files: ModelFile[],
  records: ModelRecord[],
  specs: Record<string, TypeSpec>,
  assertions: Assertion[],
) {
  const sync: ModelSync[] = [];
  const unknown: ModelSync[] = [];
  const dangling: ModelDangling[] = [];
  for (const file of files) {
    const rows = records
      .filter((r) => r.ref.notePath === file.notePath)
      .map((record) => {
        const cited = assertions.filter((a) => record.evidence.includes(a.path));
        return {
          record,
          changed: cited.filter((a) => syncCompare(a.updatedAt, file.synced) === "later"),
          unknown: cited.filter((a) => syncCompare(a.updatedAt, file.synced) === "unknown"),
        };
      });
    const later = rows.filter((r) => r.changed.length);
    if (later.length) sync.push({ file, rows: later });
    const open = rows.filter((r) => r.unknown.length);
    if (open.length) unknown.push({ file, rows: open });
  }
  for (const record of records)
    for (const d of declaredEvidence(record, specs[record.typeName], assertions))
      if (!d.assertion && !d.unread) dangling.push({ record, field: d.field, id: d.id });
  // Capped reads are disclosed, never reported as dangling.
  const incomplete = records.filter((r) => r.evidenceTruncated);
  return { sync, unknown, dangling, incomplete };
}

/** A record this one reaches through a native edge, in either stored direction. */
export type Neighbor = {
  relation: string;
  /** The edge is stored from this record to the neighbor. */
  out: boolean;
  key: string;
  ref: Ref;
  title: string;
  typeName: string;
  /** The neighbor's AO key: a model record's `aoValue` or an assertion's `aoId`. */
  aoValue?: string | null;
  value?: SourceValue | null;
};
export type Atom = {
  text: string;
  target?: Neighbor;
  /** Why an authored reference value links nowhere. */
  state?: "unresolved" | "ambiguous" | "wildcard" | "no edge" | "not read";
};
export type FieldRow = { key: string; value: SourceValue; spec?: ReferenceSpec; atoms?: Atom[] };

/** Authored fields in order, except identity, the title the header shows, and code paths.
 * A reference field's values link to the native edge target with that key; other values
 * stay as authored, with the reason they link nowhere. */
export function fieldRows(
  value: SourceValue | null | undefined,
  spec: TypeSpec | undefined,
  neighbors: Neighbor[],
): FieldRow[] {
  const all = entries(value);
  const title = spec?.titleFields.find((f) =>
    all.some((e) => e.key === f && scalars(e.value).length),
  );
  return all
    .filter((e) => e.key !== spec?.keyField && e.key !== title && e.key !== CODE_FIELD)
    .map((e) => {
      const ref = spec?.references.find((r) => r.path === e.key);
      const flat =
        e.value.kind !== "object" &&
        !(e.value.items ?? []).some((i) => i.kind === "object" || i.kind === "array");
      if (!ref || !flat || ref.values === "MAPPING_KEYS")
        return { key: e.key, value: e.value, spec: ref };
      return {
        key: e.key,
        value: e.value,
        spec: ref,
        atoms: scalars(e.value).map((text): Atom => {
          if (text === "*") return { text, state: "wildcard" };
          if (!ref.edge) return { text, state: "no edge" };
          const hits = neighbors.filter((n) => n.relation === ref.edge?.name && n.aoValue === text);
          return hits.length === 1
            ? { text, target: hits[0] }
            : { text, state: hits.length ? "ambiguous" : "unresolved" };
        }),
      };
    });
}
