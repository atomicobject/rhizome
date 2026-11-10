/** The Knowledge site library (SPEC-0135 R5): the one place that computes membership,
 * counts, the topic layout, date placement, scope sets, citation states, and gaps for
 * every site page and the panel. Its whole-KB input is the orientation query; record
 * pages add one scope read. Pure and kit-free: no UI, no queries, so `node --test` can
 * import it. Every order is by code unit, never locale, so every frame agrees. */
import { makeResolver } from "./relations.ts";
import { citations, dateNumber } from "./knowledge.ts";
import type { Assertion as EvidenceAssertion, Source as EvidenceSource } from "./knowledge.ts";
import { KIND_GROUPS, KNOWN_CONFIDENCE, cmp, groupOf, isStored, kindConflict } from "./kinds.ts";
import type { GroupId } from "./kinds.ts";
import { neighbourhood } from "./neighbourhood.ts";

// ---- Inputs: the orientation query's fields (R5) and the scope reads' (R2, R5) ----
export type Link = { path: string };
export type Root = { ref?: unknown; path: string; aoId?: string | null; title: string };
export type OAssertion = Root & { headline?: string | null; kind?: string | null; legacyKind?: string | null; confidence?: string | null; provenanceSource?: string | null };
export type OSource = Root & { name?: string | null; headline?: string | null; recordedDate?: string | number | null; extractedAssertionIds?: string[] | null };
export type OConcept = Root & {
  name?: string | null; headline?: string | null; aliases?: string[] | null;
  linkedAssertions?: Link[] | null; linkingAssertions?: Link[] | null;
  relatedConceptsSection?: { relatedConcepts: Link[] } | null;
};
export type OArtifact = Root & { name?: string | null; headline?: string | null };
export type Orientation = { assertion: OAssertion[]; source: OSource[]; concept: OConcept[]; artifact: OArtifact[]; mapOfContent: Root[] };
/** Typed-root coverage, keyed by root alias, as the kit exposes it. */
export type RootCoverage = { status: string; returnedCount?: number };
export const ROOTS = ["assertion", "source", "concept", "artifact", "mapOfContent"] as const;
/** An assertion's Sources-section citations from a scope read; `citations()` decides. */
export type CitationRead = {
  path: string; provenanceSource?: string | null; provenanceLocator?: string | null;
  sourcesSection?: { citations: { recordedLink?: string | null; sources: Link[] }[] } | null;
};
export type ArtifactRead = { path: string; supportValues?: string[] | null; additionalAssertionReferences?: CitationRead[] | null; conceptReferences?: Link[] | null };

export type SourceDate = { date: string | null; basis: "recorded" | "file" | "none"; unreadable: boolean };
export type Site = ReturnType<typeof buildSite>;

const sorted = <T extends Root>(xs: T[]) => [...xs].sort((a, b) => cmp(a.path, b.path));
const uniq = (xs: Iterable<string>) => [...new Set(xs)].sort(cmp);
const basename = (path: string) => path.split("/").pop()!;
const isMocRoot = (r: Root) => basename(r.path) === "_moc.md";
const depth = (r: Root) => r.path.split("/").length;
export const folderOf = (path: string) => (path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "");

/** D1 membership: an authored link in either direction, one set deduplicated by canonical path.
 * With `assertionPaths`, members outside that set (not an indexed assertion) are dropped. */
export function membership(concepts: Pick<OConcept, "path" | "linkedAssertions" | "linkingAssertions">[], assertionPaths?: string[]) {
  const known = assertionPaths && new Set(assertionPaths);
  const members = new Map<string, string[]>(), conceptsOf = new Map<string, string[]>();
  for (const p of assertionPaths ?? []) conceptsOf.set(p, []);
  for (const c of [...concepts].sort((a, b) => cmp(a.path, b.path))) {
    const set = uniq([...(c.linkedAssertions ?? []), ...(c.linkingAssertions ?? [])].map((l) => l.path)).filter((p) => !known || known.has(p));
    members.set(c.path, set);
    for (const a of set) conceptsOf.set(a, [...(conceptsOf.get(a) ?? []), c.path]);
  }
  return { members, conceptsOf };
}

const isoDay = (v: unknown) => (typeof v === "string" && Number.isFinite(dateNumber(v.trim())) ? v.trim().slice(0, 10) : null);
/** A recorded ISO date first, else a leading YYYY-MM-DD in the file name; never a title date. */
export function sourceDate(s: OSource): SourceDate {
  const recorded = isoDay(s.recordedDate), unreadable = s.recordedDate != null && !recorded;
  if (recorded) return { date: recorded, basis: "recorded", unreadable };
  const file = isoDay(/^\d{4}-\d{2}-\d{2}/.exec(basename(s.path))?.[0]);
  return file ? { date: file, basis: "file", unreadable } : { date: null, basis: "none", unreadable };
}

/** Authored references through the shared resolver; values matching nothing or several keep their raw text. */
function resolveValues(values: string[], resolve: (v: string) => string | undefined) {
  const resolved = new Set<string>(), unresolved: string[] = [];
  for (const v of values) {
    const key = resolve(v);
    if (key) resolved.add(key);
    else unresolved.push(v);
  }
  return { resolved: uniq(resolved), unresolved };
}

export function buildSite(o: Orientation, coverage?: Partial<Record<string, RootCoverage>>) {
  const assertions = sorted(o.assertion), concepts = sorted(o.concept), sources = sorted(o.source), artifacts = sorted(o.artifact);
  const mocs = [...o.mapOfContent].sort((a, b) => +isMocRoot(b) - +isMocRoot(a) || (isMocRoot(a) ? depth(a) - depth(b) : cmp(a.title, b.title)) || cmp(a.path, b.path));
  const byPath = new Map<string, Root>([...mocs, ...artifacts, ...sources, ...concepts, ...assertions].map((r) => [r.path, r]));
  const resolver = (xs: Root[]) => makeResolver(xs.map((r) => ({ key: r.path, aoId: r.aoId, notePath: r.path })));
  const resolveAssertion = resolver(assertions), resolveSource = resolver(sources);
  const { members, conceptsOf } = membership(concepts, assertions.map((a) => a.path));
  // Related Concepts only: undirected, deduplicated, self-links and non-concepts dropped.
  const pairs = new Map<string, [string, string]>();
  for (const c of concepts)
    for (const r of c.relatedConceptsSection?.relatedConcepts ?? [])
      if (r.path !== c.path && members.has(r.path)) {
        const pair = [c.path, r.path].sort(cmp) as [string, string];
        pairs.set(pair.join("\0"), pair);
      }
  const edges = [...pairs.values()].sort((a, b) => cmp(a[0], b[0]) || cmp(a[1], b[1]));
  const related = new Map(concepts.map((c) => [c.path, uniq(edges.flatMap(([a, b]) => (a === c.path ? [b] : b === c.path ? [a] : [])))]));
  const dates = new Map(sources.map((s) => [s.path, sourceDate(s)]));
  const dated = sources.filter((s) => dates.get(s.path)!.date).map((s) => s.path)
    .sort((a, b) => cmp(dates.get(a)!.date!, dates.get(b)!.date!) || cmp(a, b));
  const extraction = new Map(sources.map((s) => [s.path, resolveValues(s.extractedAssertionIds ?? [], resolveAssertion)]));
  // A root whose coverage is missing is not assumed complete (R2c).
  const incomplete = ROOTS.flatMap((root) => {
    const c = coverage?.[root], status = c?.status ?? "not reported";
    return status === "complete" ? [] : [{ root, status, returned: c?.returnedCount ?? o[root].length }];
  });
  return { assertions, concepts, sources, artifacts, mocs, byPath, members, conceptsOf, edges, related, dates, dated, extraction, incomplete, resolveAssertion, resolveSource };
}

const kindOf = (site: Site, path: string) => (site.byPath.get(path) as OAssertion | undefined)?.kind;

/** US1 header: the `_moc.md` title without a leading "Map of Content" and its separator, else the vault name. */
export function kbTitle(mocs: Root[], vault: string) {
  const moc = mocs.filter(isMocRoot).sort((a, b) => depth(a) - depth(b) || cmp(a.path, b.path))[0];
  const title = moc?.title.replace(/^\s*Map of Content\s*(?:[-–—:|·]\s*)?/i, "").trim();
  return title ? { title, from: "moc" as const } : { title: vault, from: "vault" as const };
}

export const MIN_CONCEPTS = 3, MIN_DATED = 3;
/** "Not enough to draw yet" thresholds (R4). */
export const drawable = (site: Site) => ({
  map: site.concepts.length >= MIN_CONCEPTS,
  timeline: site.dated.length >= MIN_DATED,
  matrix: site.concepts.some((c) => site.members.get(c.path)!.length),
});

/** Member assertions grouped by kind group, each group ordered by kind then title. */
export function byKindGroup(site: Site, paths: string[]) {
  const title = (p: string) => site.byPath.get(p)?.title ?? p;
  return KIND_GROUPS.map((g) => ({
    id: g.id, label: g.label,
    items: paths.filter((p) => groupOf(kindOf(site, p)) === g.id).sort((a, b) => cmp(kindOf(site, a) ?? "", kindOf(site, b) ?? "") || cmp(title(a), title(b)) || cmp(a, b)),
  })).filter((g) => g.items.length);
}

export function folderCounts(paths: string[]) {
  const counts = new Map<string, number>();
  for (const p of paths) counts.set(folderOf(p), (counts.get(folderOf(p)) ?? 0) + 1);
  return [...counts].map(([folder, count]) => ({ folder, count })).sort((a, b) => b.count - a.count || cmp(a.folder, b.folder));
}

// ---- Gaps: structural observations with their rule. IDs are defined here once (R2a). ----
type GapDef = { id: string; list: "assertions" | "concepts" | "sources"; label: string; rule: string; command: string | null; sub?: boolean; test: (site: Site, r: Root) => boolean };
const date = (site: Site, r: Root) => site.dates.get(r.path)!;
// Commands follow the effort's hand-offs; source dates go to a follow-up request to AO, so they carry none.
export const GAPS: GapDef[] = [
  { id: "assertion-no-concept", list: "assertions", label: "Assertions in no concept", rule: "No concept links the assertion, and the assertion links no concept.", command: "/ao-kb:curate", test: (s, r) => !s.conceptsOf.get(r.path)?.length },
  { id: "concept-no-members", list: "concepts", label: "Concepts with no member assertions", rule: "No assertion links the concept, and the concept links no assertion.", command: "/ao-kb:curate", test: (s, r) => !s.members.get(r.path)?.length },
  { id: "source-no-recorded-date", list: "sources", label: "Sources with no recorded date", rule: "No sourceDate, meetingDate, or date holds an ISO date. A date in the file name does not count.", command: null, test: (s, r) => date(s, r).basis !== "recorded" },
  { id: "source-file-name-date", list: "sources", label: "Sources dated only by their file name", rule: "No recorded date, and a YYYY-MM-DD at the start of the file name places the source on the axis.", command: null, sub: true, test: (s, r) => date(s, r).basis === "file" },
  { id: "source-undated", list: "sources", label: "Sources with no date at all", rule: "No recorded date and no YYYY-MM-DD at the start of the file name, so the source is not on the time axis.", command: null, sub: true, test: (s, r) => date(s, r).basis === "none" },
  { id: "source-unreadable-date", list: "sources", label: "Sources with an unreadable recorded date", rule: "A recorded date key is present, but its value is not an ISO date.", command: null, sub: true, test: (s, r) => date(s, r).unreadable },
  { id: "source-no-extraction", list: "sources", label: "Sources with no recorded extraction", rule: "extractedAssertionIds is absent or empty.", command: "/ao-kb:process", test: (_, r) => !(r as OSource).extractedAssertionIds?.length },
  { id: "extraction-unresolved", list: "sources", label: "Extraction IDs that resolve to no assertion", rule: "An ID in extractedAssertionIds matches no assertion's AO ID or file name, or matches several. Counted by ID; the list shows the sources holding them.", command: "/ao-kb:process", test: (s, r) => s.extraction.get(r.path)!.unresolved.length > 0 },
  { id: "confidence-other", list: "assertions", label: "Assertions with another stored confidence", rule: "Stored confidence is something other than validated, hypothesis, or guess.", command: "/ao-kb:curate", test: (_, r) => !KNOWN_CONFIDENCE.includes((r as OAssertion).confidence ?? "") },
  { id: "kind-conflict", list: "assertions", label: "Assertions recording two different kinds", rule: "Both kind and assertionKind are recorded, with different values; kind is shown.", command: "/ao-kb:curate", test: (_, r) => kindConflict(r as OAssertion) },
];
const pool = (site: Site, list: GapDef["list"]): Root[] => ({ assertions: site.assertions, concepts: site.concepts, sources: site.sources })[list];

/** A gap's matching records and count; `values` names each other stored confidence. */
export function gap(site: Site, id: string) {
  const def = GAPS.find((g) => g.id === id);
  if (!def) return undefined;
  const { test, ...about } = def;
  const items = pool(site, def.list).filter((r) => test(site, r)).map((r) => r.path);
  const count = id === "extraction-unresolved" ? items.reduce((n, p) => n + site.extraction.get(p)!.unresolved.length, 0) : items.length;
  if (id !== "confidence-other") return { ...about, items, count };
  const values: Record<string, number> = {};
  for (const p of items) {
    const v = (site.byPath.get(p) as OAssertion).confidence ?? "Confidence not recorded";
    values[v] = (values[v] ?? 0) + 1;
  }
  return { ...about, items, count, values };
}

export type AssertionParams = { concept?: string; assertionKind?: string; confidence?: string; gap?: string };
/** The Assertions list's R2a params as one predicate, so a matrix cell, a breakdown count,
 * and a gap agree with the list they open. An unknown or non-assertion gap matches nothing. */
export function assertionFilter(site: Site, p: AssertionParams) {
  const confidence = p.confidence ? new Set(p.confidence.split(",").map((v) => v.trim()).filter(Boolean)) : null;
  const g = p.gap ? GAPS.find((x) => x.id === p.gap && x.list === "assertions") : undefined;
  return (a: OAssertion) =>
    (!p.concept || !!site.conceptsOf.get(a.path)?.includes(p.concept)) &&
    (!p.assertionKind || a.kind === p.assertionKind) &&
    (!confidence || confidence.has(a.confidence ?? "")) &&
    (!p.gap || (!!g && g.test(site, a)));
}

// ---- Topic map (R4) ----
export const MAP_WIDTH = 760, ARC_MIN = 12, LABEL_MAX = 30;
const CHAR_W = 7.1, LABEL_H = 17, ITERATIONS = 700, SEPARATION_PASSES = 60, CLEARANCE = 14;
export const mapHeight = (concepts: number) => Math.round(Math.max(430, Math.min(680, 250 + 9 * concepts)));
export const radius = (members: number, max: number) => (members ? 4 + 18 * Math.sqrt(members / max) : 5);
const BOUNDARY = /[\s:\u2192-]/;
/** A concept name at rest: cut at a word boundary with an ellipsis past 30 characters. */
export function short(title: string, max = LABEL_MAX) {
  if (title.length <= max) return title;
  let s = title.slice(0, max);
  if (!BOUNDARY.test(title[max])) s = s.replace(/[\s:\u2192-]+\S*$/, "");
  return `${s.replace(/[\s:\u2192-]+$/, "")}…`;
}

/** cos and sin from basic arithmetic: Math.cos and Math.sin may differ by an ulp between
 * engines, and the layout must give the same positions in every browser (R5). */
function cosSin(a: number): [number, number] {
  if (a > Math.PI) a -= 2 * Math.PI;
  let c = 1, s = 0, term = 1;
  for (let k = 1; k <= 30; k++) {
    term *= a / k;
    if (k % 4 === 1) s += term; else if (k % 4 === 2) c -= term; else if (k % 4 === 3) s -= term; else c += term;
  }
  return [c, s];
}

export type MapLabel = { text: string; x: number; y: number; w: number; h: number; anchor: "start" | "end" | "middle"; hidden: boolean };
export type MapNode = { path: string; title: string; x: number; y: number; r: number; members: number; hollow: boolean; arcs: boolean; shares: Record<GroupId, number>; label: MapLabel };
export type Layout = { width: number; height: number; nodes: MapNode[]; edges: [number, number][]; order: number[]; unlabelled: number };
const layouts = new WeakMap<Site, Layout>();

/** Deterministic force layout over the path-sorted concepts: fixed iterations, no randomness,
 * estimated label widths. Memoized per site, which is built once per index generation.
 * ponytail: global layout, so adding a concept moves every node; seed from the previous
 * positions if people lose their bearings between index generations. */
export function layout(site: Site): Layout {
  if (layouts.has(site)) return layouts.get(site)!;
  const N = site.concepts, n = N.length, W = MAP_WIDTH, H = mapHeight(n);
  const ix = new Map(N.map((c, i) => [c.path, i]));
  const E = site.edges.map(([a, b]) => [ix.get(a)!, ix.get(b)!] as [number, number]);
  const P = N.map((_, i) => { const [c, s] = cosSin((2 * Math.PI * i) / n); return { x: 500 + c * 240, y: 280 + s * 170, vx: 0, vy: 0 }; });
  for (let it = 0; it < ITERATIONS; it++) {
    const cool = 1 - it / ITERATIONS;
    for (let i = 0; i < n; i++)
      for (let j = i + 1; j < n; j++) {
        let dx = P[i].x - P[j].x, dy = P[i].y - P[j].y;
        const d2 = Math.max(dx * dx + dy * dy, 1), d = Math.sqrt(d2), f = 52000 / d2;
        dx /= d; dy /= d;
        P[i].vx += dx * f; P[i].vy += dy * f; P[j].vx -= dx * f; P[j].vy -= dy * f;
      }
    for (const [a, b] of E) {
      let dx = P[b].x - P[a].x, dy = P[b].y - P[a].y;
      const d = Math.sqrt(dx * dx + dy * dy) || 1, f = (d - 185) * 0.018;
      dx /= d; dy /= d;
      P[a].vx += dx * f; P[a].vy += dy * f; P[b].vx -= dx * f; P[b].vy -= dy * f;
    }
    for (const p of P) {
      p.vx += (500 - p.x) * 0.003; p.vy += (280 - p.y) * 0.006;
      p.x += Math.max(-24, Math.min(24, p.vx)) * cool; p.y += Math.max(-24, Math.min(24, p.vy)) * cool;
      p.vx *= 0.5; p.vy *= 0.5;
    }
  }
  const members = N.map((c) => site.members.get(c.path)!.length), max = Math.max(1, ...members);
  const rad = members.map((m) => radius(m, max));
  if (n) {
    const xs = P.map((p) => p.x), ys = P.map((p) => p.y), x0 = Math.min(...xs), x1 = Math.max(...xs), y0 = Math.min(...ys), y1 = Math.max(...ys);
    for (const p of P) { p.x = 135 + ((p.x - x0) / (x1 - x0 || 1)) * (W - 270); p.y = 30 + ((p.y - y0) / (y1 - y0 || 1)) * (H - 60); }
    // Separation passes push apart any two nodes closer than their radii plus the clearance, inside the bounds.
    for (let it = 0; it < SEPARATION_PASSES; it++) {
      for (let i = 0; i < n; i++)
        for (let j = i + 1; j < n; j++) {
          const dx = P[j].x - P[i].x, dy = P[j].y - P[i].y, d = Math.sqrt(dx * dx + dy * dy) || 1, need = rad[i] + rad[j] + CLEARANCE;
          if (d >= need) continue;
          const m = (need - d) / 2, ux = d > 1 ? dx / d : 1, uy = d > 1 ? dy / d : 0;
          P[i].x -= ux * m; P[i].y -= uy * m; P[j].x += ux * m; P[j].y += uy * m;
        }
      P.forEach((p, i) => { p.x = Math.max(rad[i] + 4, Math.min(W - rad[i] - 4, p.x)); p.y = Math.max(rad[i] + 4, Math.min(H - rad[i] - 4, p.y)); });
    }
  }
  const box = (i: number) => ({ x: P[i].x - rad[i] - 2, y: P[i].y - rad[i] - 2, w: 2 * rad[i] + 4, h: 2 * rad[i] + 4 });
  const boxes = N.map((_, i) => box(i));
  const hit = (a: { x: number; y: number; w: number; h: number }, b: typeof a) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
  // Arrow order and label priority: member count descending, then title.
  const order = N.map((_, i) => i).sort((a, b) => members[b] - members[a] || cmp(N[a].title, N[b].title) || cmp(N[a].path, N[b].path));
  const labels: MapLabel[] = [], placed: MapLabel[] = [];
  for (const i of order) {
    const { x, y } = P[i], r = rad[i], text = short(N[i].title), w = text.length * CHAR_W + 6, h = LABEL_H;
    const candidates: Omit<MapLabel, "text" | "w" | "h" | "hidden">[] = [
      { x: x + r + 5, y: y - h / 2, anchor: "start" }, { x: x - r - 5 - w, y: y - h / 2, anchor: "end" },
      { x: x - w / 2, y: y + r + 3, anchor: "middle" }, { x: x - w / 2, y: y - r - 3 - h, anchor: "middle" },
      { x: x + r + 3, y: y + r - 2, anchor: "start" }, { x: x - r - 3 - w, y: y + r - 2, anchor: "end" },
      { x: x + r + 3, y: y - r - h + 2, anchor: "start" }, { x: x - r - 3 - w, y: y - r - h + 2, anchor: "end" },
    ];
    const fits = (c: (typeof candidates)[number]) => {
      const b = { x: c.x, y: c.y, w, h };
      return b.x > 0 && b.x + w < W && b.y > 0 && b.y + h < H && !boxes.some((q, j) => j !== i && hit(b, q)) && !placed.some((q) => hit(b, q));
    };
    const found = candidates.find(fits);
    // Unlabelled at rest; the scoped neighbourhood uses the default position to the node's right.
    labels[i] = { ...(found ?? candidates[0]), text, w, h, hidden: !found };
    if (found) placed.push(labels[i]);
  }
  const nodes = N.map((c, i): MapNode => {
    const shares = { statements: 0, intent: 0, risks: 0, other: 0 };
    for (const a of site.members.get(c.path)!) shares[groupOf(kindOf(site, a))]++;
    return { path: c.path, title: c.title, x: P[i].x, y: P[i].y, r: rad[i], members: members[i], hollow: !members[i], arcs: !!members[i] && rad[i] >= ARC_MIN, shares, label: labels[i] };
  });
  const result = { width: W, height: H, nodes, edges: E, order, unlabelled: labels.filter((l) => l.hidden).length };
  layouts.set(site, result);
  return result;
}

// ---- Sources by date (R4) ----
const DAY = 864e5, stamp = (day: string) => Date.parse(`${day}T00:00:00Z`);
/** One time domain for the site. A long empty stretch becomes an axis break; nothing is trimmed. */
export function timeAxis(site: Site) {
  const ts = site.dated.map((p) => stamp(site.dates.get(p)!.date!));
  if (!ts.length) return null;
  const span = Math.max(ts[ts.length - 1] - ts[0], DAY), runs: [number, number][] = [];
  let start = ts[0];
  for (let i = 1; i < ts.length; i++)
    if (ts[i] - ts[i - 1] > 120 * DAY && ts[i] - ts[i - 1] > span * 0.35) { runs.push([start, ts[i - 1]]); start = ts[i]; }
  runs.push([start, ts[ts.length - 1]]);
  const BREAK = 0.07, total = runs.reduce((n, [a, b]) => n + Math.max(b - a, 7 * DAY), 0), avail = 1 - BREAK * (runs.length - 1);
  let acc = 0;
  const segments = runs.map(([a, b]) => { const w = (Math.max(b - a, 7 * DAY) / total) * avail, m = { a, b, u0: acc, u1: acc + w }; acc += w + BREAK; return m; });
  const u = (t: number) => {
    const m = segments.find((s) => t <= s.b) ?? segments[segments.length - 1];
    return m.u0 + (m.b === m.a ? 0.5 * (m.u1 - m.u0) : ((t - m.a) / (m.b - m.a)) * (m.u1 - m.u0));
  };
  return { segments, u, at: (day: string) => u(stamp(day)) };
}

/** Tick timestamps for one axis segment drawn `px` wide: weekly, monthly, quarterly, or yearly. */
export function ticks(m: { a: number; b: number }, px: number) {
  const span = (m.b - m.a) / DAY, out: number[] = [];
  if (span < 1) return [m.a];
  const step = span > 900 ? 12 : span > 400 ? 3 : span > 120 ? 1 : 0;
  if (!step) { for (let t = m.a; t <= m.b; t += 7 * DAY) out.push(t); }
  else {
    const d = new Date(m.a); d.setUTCDate(1);
    if (d.getTime() < m.a) d.setUTCMonth(d.getUTCMonth() + 1);
    for (; d.getTime() <= m.b; d.setUTCMonth(d.getUTCMonth() + step)) out.push(d.getTime());
  }
  const every = Math.ceil(out.length / Math.max(1, Math.floor(px / 64)));
  return out.filter((_, i) => i % every === 0);
}

/** A source mark: area by resolved extractions, colour by a strict majority kind group or none. */
export function sourceMark(site: Site, path: string) {
  const resolved = site.extraction.get(path)?.resolved ?? [], counts = new Map<GroupId, number>();
  for (const a of resolved) counts.set(groupOf(kindOf(site, a)), (counts.get(groupOf(kindOf(site, a))) ?? 0) + 1);
  const group = [...counts].find(([, c]) => c * 2 > resolved.length)?.[0] ?? null;
  return { r: resolved.length ? 3.2 + 1.45 * Math.sqrt(resolved.length) : 3.2, group, resolved: resolved.length, basis: site.dates.get(path)?.basis ?? "none" };
}

/** Row label: the folder path under its `sources/` or `artifacts/` root. Past 24 characters
 * the last folder stays whole and the folders before it shorten, so siblings never read the same. */
export function folderLabel(folder: string) {
  const parts = folder.split("/"), i = parts.findIndex((p) => p === "sources" || p === "artifacts");
  return parts.slice(i + 1).join(" / ") || "(top level)";
}
export function rowLabel(folder: string, max = 24) {
  const label = folderLabel(folder);
  if (label.length <= max) return label;
  const parts = label.split(" / "), last = parts.pop()!;
  if (!parts.length) return last;
  if (last.length > max - 6) return `… / ${last}`;
  return `${parts.join(" / ").slice(0, max - last.length - 4).trimEnd()}… / ${last}`;
}

/** Rows by folder (most dated sources first), arrow order by date, then folder, then path, and the caption's counts. */
export function timeline(site: Site) {
  const rows = folderCounts(site.dated).map(({ folder }) => ({ folder, label: rowLabel(folder), marks: site.dated.filter((p) => folderOf(p) === folder) }));
  const row = new Map(rows.map((r, i) => [r.folder, i]));
  const order = [...site.dated].sort((a, b) => cmp(site.dates.get(a)!.date!, site.dates.get(b)!.date!) || row.get(folderOf(a))! - row.get(folderOf(b))! || cmp(a, b));
  const all = [...site.dates.values()];
  const counts = { recorded: 0, file: 0, none: 0, unreadable: all.filter((d) => d.unreadable).length };
  for (const d of all) counts[d.basis]++;
  return { rows, order, counts };
}

/** Vertical offsets that keep marks on one row from covering each other. */
export function stackOffsets(marks: { x: number; r: number }[], steps = [0, -7, 7, -13, 13, -18, 18]) {
  const placed: { x: number; y: number; r: number }[] = [];
  return marks.map((m) => {
    const off = steps.find((o) => placed.every((q) => Math.sqrt((q.x - m.x) ** 2 + (q.y - o) ** 2) > q.r + m.r + 0.8)) ?? 0;
    placed.push({ x: m.x, y: off, r: m.r });
    return off;
  });
}

// ---- Matrix (R4, US4) ----
/** Rows are concepts with members; columns are raw kinds under their groups, then stored confidence. */
export function matrix(site: Site) {
  const members = (c: string) => site.members.get(c)!, title = (c: string) => site.byPath.get(c)!.title;
  const rows = site.concepts.map((c) => c.path).filter((c) => members(c).length).sort((a, b) => members(b).length - members(a).length || cmp(title(a), title(b)) || cmp(a, b));
  const kinds = new Map<string, number>();
  for (const a of site.assertions) if (a.kind) kinds.set(a.kind, (kinds.get(a.kind) ?? 0) + 1);
  const columns = KIND_GROUPS.flatMap((g) => [...kinds].filter(([k]) => groupOf(k) === g.id).sort((x, y) => y[1] - x[1] || cmp(x[0], y[0])).map(([kind]) => ({ kind, group: g.id })));
  const ofKind = (c: string, kind: string) => members(c).filter((a) => kindOf(site, a) === kind);
  const stored = (c: string) => members(c).filter((a) => isStored((site.byPath.get(a) as OAssertion).confidence));
  const cells = rows.map((concept) => ({ concept, cells: columns.map((col) => ofKind(concept, col.kind).length), stored: stored(concept).length }));
  // Column totals count every assertion once, though rows can share assertions.
  const totals = columns.map((col) => new Set(rows.flatMap((c) => ofKind(c, col.kind))).size);
  const groups = KIND_GROUPS.map((g) => ({ id: g.id, label: g.label, span: columns.filter((c) => c.group === g.id).length })).filter((g) => g.span);
  return {
    rows: cells, columns, groups, totals,
    storedTotal: new Set(rows.flatMap(stored)).size,
    max: Math.max(0, ...cells.flatMap((r) => [...r.cells, r.stored])),
    emptyConcepts: site.concepts.map((c) => c.path).filter((c) => !members(c).length),
  };
}

/** Cell tint in percent: linear from a 12% floor at one to `cap` at the table's largest cell; blank at zero. */
export function tintPercent(n: number, max: number, cap: number, floor = 12) {
  if (n <= 0) return 0;
  return max <= 1 ? cap : floor + ((cap - floor) * (n - 1)) / (max - 1);
}

// ---- Scope sets from scope reads (US2, R5) ----
export type Scope = { topics: Set<string>; sources: Set<string>; mark?: string };
function citedSources(site: Site, reads: CitationRead[]) {
  const out = new Map<string, Set<string>>();
  for (const r of reads) {
    const a = { ...r, provenanceSource: r.provenanceSource ?? (site.byPath.get(r.path) as OAssertion | undefined)?.provenanceSource };
    out.set(r.path, new Set(citations(a as unknown as EvidenceAssertion, site.sources as unknown as EvidenceSource[]).flatMap((c) => (c.source ? [c.source.path] : []))));
  }
  return out;
}
const conceptsOfAll = (site: Site, assertions: string[]) => assertions.flatMap((a) => site.conceptsOf.get(a) ?? []);

/** A concept: itself and its Related Concepts on the map; the sources its members cite on the strip. */
export function conceptScope(site: Site, concept: string, memberReads: CitationRead[]): Scope {
  const cited = [...citedSources(site, memberReads).values()].flatMap((s) => [...s]);
  return { topics: new Set(uniq([concept, ...(site.related.get(concept) ?? [])])), sources: new Set(uniq(cited)), mark: concept };
}

/** Candidates for "Assertions that cite it": the source's linking assertions, united with
 * every assertion whose provenanceSource resolves to it. The scope read confirms them. */
export function sourceCandidates(site: Site, source: string, linkingAssertions: Link[]) {
  const prov = site.assertions.filter((a) => a.provenanceSource && site.resolveSource(a.provenanceSource) === source).map((a) => a.path);
  return uniq([...linkingAssertions.map((l) => l.path), ...prov]);
}
/** A source: the confirmed citing assertions, their concepts on the map, and the source ringed. */
export function sourceScope(site: Site, source: string, candidateReads: CitationRead[]): Scope & { citing: string[] } {
  const citing = [...citedSources(site, candidateReads)].filter(([, s]) => s.has(source)).map(([a]) => a).sort(cmp);
  return { citing, topics: new Set(uniq(conceptsOfAll(site, citing))), sources: new Set([source]), mark: source };
}

/** Resolved support values the artifact read did not already carry citations for. */
export function artifactFollowUp(site: Site, read: ArtifactRead) {
  const have = new Set((read.additionalAssertionReferences ?? []).map((r) => r.path));
  return resolveValues(read.supportValues ?? [], site.resolveAssertion).resolved.filter((p) => !have.has(p));
}
/** An artifact: "Supported by (recorded)", unresolved support values, the other assertions it links,
 * the concepts it links and those assertions' concepts, and the sources those assertions cite. */
export function artifactScope(site: Site, read: ArtifactRead, followUpReads: CitationRead[]) {
  const { resolved: supported, unresolved } = resolveValues(read.supportValues ?? [], site.resolveAssertion);
  const links = uniq((read.additionalAssertionReferences ?? []).map((r) => r.path)).filter((p) => !supported.includes(p));
  const concepts = uniq((read.conceptReferences ?? []).map((c) => c.path)).filter((c) => site.members.has(c));
  const cited = citedSources(site, [...(read.additionalAssertionReferences ?? []), ...followUpReads]);
  const sources = uniq([...supported, ...links].flatMap((a) => [...(cited.get(a) ?? [])]));
  return { supported, unresolved, links, concepts, topics: new Set(uniq([...concepts, ...conceptsOfAll(site, [...supported, ...links])])), sources: new Set(sources) };
}

/** The scoped pair's two halves, each with its "not enough to draw yet" state and the record's own
 * counts. The map is not enough when the KB's map is not (`kb-map`), the scope holds no concept
 * (`no-concept`), or its crop would draw one node with nothing to place it against (`alone`). */
export function scopedPair(site: Site, scope: Scope) {
  const can = drawable(site), inScope = [...scope.topics];
  const hood = can.map ? neighbourhood(layout(site), scope.topics) : null;
  const missing = !can.map ? "kb-map" : !hood ? "no-concept" : hood.drawn.size < 2 ? "alone" : null;
  const dated = [...scope.sources].filter((s) => site.dates.get(s)?.date).sort((a, b) => cmp(site.dates.get(a)!.date!, site.dates.get(b)!.date!) || cmp(a, b));
  const undated = [...scope.sources].filter((s) => !site.dates.get(s)?.date).sort(cmp);
  return {
    map: { drawable: !missing, missing, inScope, concepts: site.concepts.length, hood },
    strip: { drawable: can.timeline, datedInKb: site.dated.length, dated, undated },
  };
}

// ---- Citation states (US3) ----
export type CitationState = "passage" | "locator" | "linked" | "not-linked" | "unresolved";
/** One state per citation. For "locator", the inline label is SPEC-0133's `coverage()` state.
 * `linked` is whether `linkedBlock` found an authored link to the assertion in the cited source. */
export function citationState(c: { source?: unknown; locator?: string }, passage: { status: string } | undefined, linked: boolean): CitationState {
  if (!c.source) return "unresolved";
  if (c.locator) return passage?.status === "resolved" ? "passage" : "locator";
  return linked ? "linked" : "not-linked";
}

/** The source offset of the first authored link in `links` (a body's links as the kit's
 * `markdownLinks` lists them) that the resolver maps to `assertion`, or undefined when the body
 * never links it. The first link wins, so the highlight never moves to a later or nearby block. */
export function linkedBlock(links: { target: string; offset: number }[], assertion: string, resolve: (ref: string) => string | undefined): number | undefined {
  return links.find((l) => resolve(l.target) === assertion)?.offset;
}
