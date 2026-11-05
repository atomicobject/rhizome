import type { NoteWorkspaceLink } from "../../api/types";
import { displayTitle } from "../../lib/labels";
import { publicTypeName } from "../../lib/typeNames";
import type { TypeLabelFn } from "../typeLabels";
import type { RailRow, RailBucket } from "./NoteInfoRows";

const NOTE_CONTENT_KINDS = new Set(["note", "section", "embedded"]);

export function isNoteContent(item: NoteWorkspaceLink): boolean {
  return NOTE_CONTENT_KINDS.has(item.kind);
}

// Anchored items address a node inside a note; the shell opens `path#anchor`.
export function relationTarget(item: NoteWorkspaceLink): string {
  return item.anchor && !item.path.includes("#") ? `${item.path}#${item.anchor}` : item.path;
}

// Link kinds carry an index qualifier ("note_link:wikilink:basic"); readers
// only need the kind.
export function humanizeRelationName(name: string): string {
  const spaced = name
    .split(":", 1)[0]
    .trim()
    .replace(/[_-]+/g, " ")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .toLowerCase();

  return spaced ? spaced[0].toUpperCase() + spaced.slice(1) : name;
}

// Servers before `direction` existed only marked backlinks.
function itemDirection(item: NoteWorkspaceLink): RailRow["direction"] {
  if (item.direction) return item.direction;

  return item.provenance === "backlink" ? "incoming" : "outgoing";
}

export const RELATION_DIRECTIONS: RailRow["direction"][] = ["incoming", "outgoing", "both"];

export const RELATION_DIRECTION_LABEL = {
  incoming: "Incoming",
  outgoing: "Outgoing",
  both: "Both ways",
};

export const DIRECTION_GLYPH = { incoming: "←", outgoing: "→", both: "↔" };

export function railRow(item: NoteWorkspaceLink, typeLabel: TypeLabelFn): RailRow {
  const target = relationTarget(item);
  const type = publicTypeName(item.resolvedType || item.structuralNode?.typeName);

  return {
    key: target,
    target,
    label: displayTitle(item.title) || target,
    title: item.targetTitle || item.title || target,
    typeName: type ? typeLabel(type) : "",
    direction: itemDirection(item),
    current: item.current,
    openable: isNoteContent(item),
  };
}

// Typed field relations keep their target type on each record.
export function relationBuckets(
  items: NoteWorkspaceLink[],
  typeLabel: TypeLabelFn,
  noteReader: boolean,
): RailBucket[] {
  const buckets = new Map<string, RailBucket>();

  for (const item of items) {
    const row = railRow(item, typeLabel);
    const relation = item.relationName || item.provenance || "relation";
    const name = humanizeRelationName(relation);
    // Key by the schema field, not its label: `partOf` and `part_of` stay apart.
    const key = `${row.direction}:${relation}`;
    const bucket = buckets.get(key) ?? { key, label: name, direction: row.direction, rows: [] };

    if (!bucket.rows.some((existing) => existing.target === row.target)) bucket.rows.push(row);
    buckets.set(key, bucket);
  }

  if (noteReader) return Array.from(buckets.values());

  return Array.from(buckets.values()).map((bucket) => {
    const types = new Set(bucket.rows.map((row) => row.typeName));

    if (types.size !== 1) return bucket;

    return {
      ...bucket,
      hint: bucket.rows[0].typeName,
      rows: bucket.rows.map((row) => ({ ...row, typeName: "" })),
    };
  });
}

// The folder that holds a note, shown beside an untyped link ("pkg/app/").
function folderOf(target: string): string {
  const path = target.split("#", 1)[0];
  const slash = path.lastIndexOf("/");

  return slash > 0 ? path.slice(0, slash + 1) : "";
}

const UNTYPED_BUCKET_LABEL = "Other notes";

// Body links and backlinks, one row per note with its direction, bucketed by
// type; untyped notes share one bucket and show their folder instead.
export function linkedBuckets(
  items: NoteWorkspaceLink[],
  exclude: ReadonlySet<string>,
  typeLabel: TypeLabelFn,
  noteReader: boolean,
): RailBucket[] {
  const rows = new Map<string, RailRow>();

  for (const item of items) {
    const row = railRow(item, typeLabel);

    if (exclude.has(row.target)) continue;
    const existing = rows.get(row.target);

    if (!existing) {
      rows.set(row.target, row);
    } else if (existing.direction !== row.direction) {
      existing.direction = "both";
    }
  }

  const buckets = new Map<string, RailBucket & { typed: boolean }>();

  for (const row of rows.values()) {
    const typed = Boolean(row.typeName);
    const label = row.typeName || UNTYPED_BUCKET_LABEL;
    const key = `${typed ? "type" : "untyped"}:${label}`;
    const bucket = buckets.get(key) ?? { key, label, typed, rows: [] };
    bucket.rows.push({
      ...row,
      typeName: noteReader ? row.typeName : "",
      folder: typed ? undefined : folderOf(row.target),
    });
    buckets.set(key, bucket);
  }

  return Array.from(buckets.values()).sort(
    (a, b) =>
      Number(b.typed) - Number(a.typed) ||
      b.rows.length - a.rows.length ||
      a.label.localeCompare(b.label),
  );
}
