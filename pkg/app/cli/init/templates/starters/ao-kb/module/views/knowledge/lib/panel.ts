/** The Knowledge panel's logic (SPEC-0135 US7): Find by title, the Recent list, the
 * locator's scope, and the counted lists. Pure and kit-free so `node --test` can import it;
 * every computation over the KB comes from the site library. */
import { artifactScope, conceptScope, sourceScope } from "./site.ts";
import type { ArtifactRead, CitationRead, Root, Scope, Site } from "./site.ts";
import { cmp } from "./kinds.ts";

// ---- Find by title ----
export type TitleEntry = { path: string; label: string; names: { text: string; alias?: string }[] };
export type TitleMatch = { path: string; label: string; alias?: string };

/** Every record the orientation query read, with its label, title, and a concept's aliases. */
export function titleIndex(site: Site, label: (r: Root) => string): TitleEntry[] {
  return [...site.byPath.values()].map((r) => {
    const shown = label(r);
    const aliases = ((r as { aliases?: string[] | null }).aliases ?? []).map((a) => ({ text: a, alias: a }));
    return { path: r.path, label: shown, names: [{ text: shown }, { text: r.title }, ...aliases] };
  });
}

/** Records whose title or alias holds `text`, ignoring case: exact, then prefix, then
 * word prefix, then anywhere; at one rank a title before an alias, then by label ignoring case,
 * then path. A match says which alias it came
 * through when no title matched as well. */
export function findByTitle(index: TitleEntry[], text: string, limit = 8): TitleMatch[] {
  const q = text.trim().toLowerCase();
  if (!q) return [];
  const rank = (name: string) => {
    const n = name.toLowerCase();
    if (n === q) return 0;
    if (n.startsWith(q)) return 1;
    if (n.includes(` ${q}`) || n.includes(`-${q}`)) return 2;
    return n.includes(q) ? 3 : -1;
  };
  const hits: (TitleMatch & { rank: number })[] = [];
  for (const e of index) {
    let best: { rank: number; alias?: string } | undefined;
    for (const name of e.names) {
      const r = rank(name.text);
      if (r >= 0 && (!best || r < best.rank || (r === best.rank && best.alias && !name.alias))) best = { rank: r, alias: name.alias };
    }
    if (best) hits.push({ path: e.path, label: e.label, alias: best.alias, rank: best.rank });
  }
  return hits
    .sort((a, b) => a.rank - b.rank || +!!a.alias - +!!b.alias || cmp(a.label.toLowerCase(), b.label.toLowerCase()) || cmp(a.path, b.path))
    .slice(0, limit)
    .map(({ rank: _, ...m }) => (m.alias ? m : { path: m.path, label: m.label }));
}

// ---- Recent: the subjects this window visited, kept in sessionStorage ----
type Ref = { notePath: string; kind: string; fragment?: string | null; nodeId?: string | null; typeName?: string | null; structuralFingerprint?: string | null };
type Context =
  | { kind: "node"; type: string; ref: Ref }
  | { kind: "type"; type: string }
  | { kind: "interface"; interface: string }
  | { kind: string };
export type RecentEntry =
  | { key: string; kind: "node"; type: string; ref: Ref }
  | { key: string; kind: "type" | "interface"; name: string };

export const RECENT_MAX = 12;
export const recentKey = (vault: string, experience: string) => `rhizome.ao-kb.panel.recent:${vault}:${experience}`;

/** A visited subject as a Recent entry. Groups and the workspace are reached through the
 * panel's own links, and the panel's location names the subject, not the switcher's view. */
export function recentEntry(context: Context): RecentEntry | null {
  if (context.kind === "node" && "ref" in context) {
    const { notePath, kind, fragment, nodeId, typeName, structuralFingerprint } = context.ref;
    const ref: Ref = { notePath, kind };
    if (fragment) ref.fragment = fragment;
    if (nodeId) ref.nodeId = nodeId;
    if (typeName) ref.typeName = typeName;
    if (structuralFingerprint) ref.structuralFingerprint = structuralFingerprint;
    return { key: `node:${notePath}#${nodeId ?? fragment ?? ""}`, kind: "node", type: context.type, ref };
  }
  if (context.kind === "type" && "type" in context) return { key: `type:${context.type}`, kind: "type", name: context.type };
  if (context.kind === "interface" && "interface" in context) return { key: `interface:${context.interface}`, kind: "interface", name: context.interface };
  return null;
}

/** `entry` first, once, keeping at most RECENT_MAX. Unchanged when it is already first. */
export function addRecent(list: RecentEntry[], entry: RecentEntry | null): RecentEntry[] {
  if (!entry || list[0]?.key === entry.key) return list;
  return [entry, ...list.filter((e) => e.key !== entry.key)].slice(0, RECENT_MAX);
}

const isString = (v: unknown): v is string => typeof v === "string" && v.length > 0;
function isEntry(v: unknown): v is RecentEntry {
  if (!v || typeof v !== "object") return false;
  const e = v as Record<string, unknown>;
  if (!isString(e.key)) return false;
  if (e.kind === "type" || e.kind === "interface") return isString(e.name);
  const ref = e.ref as Record<string, unknown> | undefined;
  return e.kind === "node" && isString(e.type) && !!ref && isString(ref.notePath) && isString(ref.kind);
}

/** What sessionStorage held; anything malformed is dropped rather than trusted. */
export function parseRecent(raw: string | null): RecentEntry[] {
  try {
    const v: unknown = JSON.parse(raw ?? "[]");
    return Array.isArray(v) ? v.filter(isEntry).slice(0, RECENT_MAX) : [];
  } catch {
    return [];
  }
}

// ---- The locator (R4's highlight rule, R6's panel colours) ----
export type LocatorReads = { source?: CitationRead[]; artifact?: ArtifactRead };

/** The opened record's scope on the map: a concept with its Related Concepts (ringed), an
 * assertion's concepts, or, once their scope read answers, the concepts of a source's citing
 * assertions and of an artifact's assertions. Undefined while that read is pending; null
 * for a page that is not a record the KB knows, where the locator collapses. */
export function locatorScope(site: Site, context: Context, reads: LocatorReads = {}): Scope | null | undefined {
  if (context.kind !== "node" || !("ref" in context)) return null;
  const path = context.ref.notePath;
  if (site.members.has(path)) return conceptScope(site, path, []);
  if (site.conceptsOf.has(path)) return { topics: new Set(site.conceptsOf.get(path)), sources: new Set() };
  if (site.dates.has(path)) return reads.source && sourceScope(site, path, reads.source);
  if (site.artifacts.some((a) => a.path === path)) return reads.artifact && artifactScope(site, reads.artifact, []);
  return null;
}

/** The map's nodes framed tight, so the whole map fits the panel's width. */
export function locatorBox(nodes: { x: number; y: number; r: number }[], pad = 6) {
  if (!nodes.length) return null;
  const x0 = Math.min(...nodes.map((n) => n.x - n.r)) - pad, x1 = Math.max(...nodes.map((n) => n.x + n.r)) + pad;
  const y0 = Math.min(...nodes.map((n) => n.y - n.r)) - pad, y1 = Math.max(...nodes.map((n) => n.y + n.r)) + pad;
  return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 };
}

// ---- Counted lists ----
/** Raw assertion kinds with their counts, most first, then by name. Unrecorded kinds are not listed. */
export function kindCounts(site: Site) {
  const counts = new Map<string, number>();
  for (const a of site.assertions) if (a.kind) counts.set(a.kind, (counts.get(a.kind) ?? 0) + 1);
  return [...counts].map(([kind, count]) => ({ kind, count })).sort((a, b) => b.count - a.count || cmp(a.kind, b.kind));
}
