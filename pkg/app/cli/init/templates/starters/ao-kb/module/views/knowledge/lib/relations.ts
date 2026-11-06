/** AO KB relation normalization, shared by every Knowledge presentation so the
 * record, Attention, and Trace agree (SPEC-0133 R6 adapter table, R7 traversal table).
 *
 * Typed queries expose declarations as authored: `## Connections` bullets on
 * `connectionsSection` (canonical and declared-inverse fields), map-shaped frontmatter
 * connections as raw `frontmatter<Rel>` strings, and `@reverse` navigation. This module
 * turns those declarations into canonical edges exactly as AO's reader does
 * (`REL_ALIASES` and `normalizeRel` in build-kb-graph.js at toolkit e9dce951): inverse
 * aliases swap direction, camelCase and snake_case spellings normalize to kebab-case,
 * self-edges are dropped, and a relation declared in the body and in frontmatter counts
 * once. One deliberate difference: AO reads `corroborated-by` as an inverted `grounds`
 * edge, while Rhizome keeps corroboration as its own claim-to-source relation, distinct
 * from origin and extraction.
 *
 * Pure and kit-free so `node --test` can import it.
 */

export type Origin = 'body' | 'frontmatter';

/** One authored declaration, as read from a typed field on the declaring record. */
export type Declaration = {
  /** Stable key of the declaring record (its notePath). */
  from: string;
  /** Field or frontmatter key it was declared under, e.g. `supportedBy`, `frontmatterSupports`, `supersedes`. */
  field: string;
  /** Stable key of the resolved target, or undefined when the authored value names nothing. */
  to: string | undefined;
  /** The authored value when it came from a raw string (an AO id or slug). */
  raw?: string;
  origin: Origin;
};

/** A canonical edge in AO's direction: `from` declares `rel` about `to`. */
export type Edge = {
  from: string;
  to: string;
  rel: string;
  /** Every declaration that produced this edge (body and frontmatter duplicates collapse here). */
  declaredBy: { record: string; as: string; origin: Origin }[];
};

/** A declaration whose authored target resolved to no record (a dangling declared reference). */
export type Dangling = { from: string; as: string; raw: string; origin: Origin };

const REL_ALIASES: Record<string, { rel: string; invert?: boolean }> = {
  'derived-from': { rel: 'traces-to' },
  'related-concepts': { rel: 'relates-to' },
  'supported-by': { rel: 'supports', invert: true },
  'refined-by': { rel: 'refines', invert: true },
  'contradicted-by': { rel: 'contradicts', invert: true },
  'grounded-by': { rel: 'grounds', invert: true },
  corroborates: { rel: 'grounds' },
  blocks: { rel: 'depends-on', invert: true },
  'resolved-by': { rel: 'resolves', invert: true },
  'addressed-by': { rel: 'resolves', invert: true },
  'superseded-by': { rel: 'supersedes', invert: true },
};

/** AO's relation normalization; `corroborated-by` stays a relation of its own. */
export function normalizeRel(name: string): { rel: string; invert: boolean } {
  const kebab = String(name).trim().replace(/^frontmatter(?=[A-Z])/, '')
    .replace(/([a-z0-9])([A-Z])/g, '$1-$2').replace(/_/g, '-').toLowerCase();
  if (kebab === 'corroborated-by') return { rel: kebab, invert: false };
  const alias = REL_ALIASES[kebab];
  return alias ? { rel: alias.rel, invert: !!alias.invert } : { rel: kebab, invert: false };
}

/** Canonical edges and dangling declarations from a set of declarations. */
export function canonicalEdges(declarations: Declaration[]): { edges: Edge[]; dangling: Dangling[] } {
  const edges = new Map<string, Edge>();
  const dangling: Dangling[] = [];
  for (const d of declarations) {
    const { rel, invert } = normalizeRel(d.field);
    const as = invert ? kebabOf(d.field) : rel;
    if (!d.to) {
      if (d.raw !== undefined) dangling.push({ from: d.from, as, raw: d.raw, origin: d.origin });
      continue;
    }
    const from = invert ? d.to : d.from;
    const to = invert ? d.from : d.to;
    if (from === to) continue;
    const key = `${from}\u0000${to}\u0000${rel}`;
    const edge = edges.get(key) ?? { from, to, rel, declaredBy: [] };
    if (!edge.declaredBy.some((by) => by.record === d.from && by.as === as && by.origin === d.origin)) {
      edge.declaredBy.push({ record: d.from, as, origin: d.origin });
    }
    edges.set(key, edge);
  }
  return { edges: [...edges.values()], dangling };
}

function kebabOf(field: string): string {
  return field.replace(/^frontmatter(?=[A-Z])/, '').replace(/([a-z0-9])([A-Z])/g, '$1-$2').replace(/_/g, '-').toLowerCase();
}

/** Resolve an authored AO reference (an id such as `a-...`, a slug, or a wikilink target)
 * the way AO's `resolveRef` does for ids and slugs: exact id first, then the last path
 * segment's slug, case-insensitively. Names are not matched. Returns undefined when nothing
 * or more than one record matches, so an ambiguity never picks a winner. */
export function makeResolver(records: { key: string; aoId?: string | null; notePath: string }[]) {
  const byId = new Map<string, string[]>();
  const bySlug = new Map<string, string[]>();
  const add = (map: Map<string, string[]>, k: string, key: string) => map.set(k, [...(map.get(k) ?? []), key]);
  for (const r of records) {
    if (r.aoId) add(byId, r.aoId.trim(), r.key);
    add(bySlug, r.notePath.split('/').pop()!.replace(/\.md$/i, '').toLowerCase(), r.key);
  }
  return (ref: string | undefined | null): string | undefined => {
    if (!ref) return undefined;
    const t = String(ref).trim().replace(/^\[\[|\]\]$/g, '').split('|')[0].split('#')[0].trim();
    const id = byId.get(t);
    if (id) return id.length === 1 ? id[0] : undefined;
    const slug = bySlug.get(t.split('/').pop()!.replace(/\.md$/i, '').toLowerCase());
    return slug && slug.length === 1 ? slug[0] : undefined;
  };
}

/** R7 traversal classification, separate from the edge's name and storage direction.
 * - `reference`: upstream follows the outgoing reference to its target; downstream follows
 *   incoming references to the citing record (claim cites or is corroborated by a source,
 *   an artifact is supported by a claim, an artifact references a record).
 * - `structure`: product-model structure (feature serves flow, ...). Not bound in this slice.
 * - `lateral`: shown beside the focus with its actual direction, never expanded
 *   (contradicts, supersedes, resolves, refines; concept mentions and related concepts).
 * - `extraction`: shown separately as extraction history, never as support.
 * - `unclassified`: listed in the relationship list only, with no upstream/downstream meaning. */
export type Traversal = 'reference' | 'structure' | 'lateral' | 'extraction' | 'unclassified';

const TRAVERSAL: Record<string, Traversal> = {
  cites: 'reference',
  'corroborated-by': 'reference',
  'supported-by-claim': 'reference',
  references: 'reference',
  contradicts: 'lateral',
  supersedes: 'lateral',
  resolves: 'lateral',
  refines: 'lateral',
  mentions: 'lateral',
  'related-concepts': 'lateral',
  extracted: 'extraction',
};

/** Relation names for edges that are not Connections declarations. */
export const CITES = 'cites';
export const MENTIONS = 'mentions';
export const RELATED_CONCEPTS = 'related-concepts';
/** An artifact's `supported_by` claim (artifact to assertion). */
export const ARTIFACT_SUPPORTED_BY = 'supported-by-claim';
export const REFERENCES = 'references';
export const EXTRACTED = 'extracted';

export function traversal(rel: string): Traversal {
  return TRAVERSAL[rel] ?? 'unclassified';
}
