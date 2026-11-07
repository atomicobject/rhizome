import {
  canonicalEdges,
  makeResolver,
  CITES,
  MENTIONS,
  REFERENCES,
  ARTIFACT_SUPPORTED_BY,
  EXTRACTED,
  RELATED_CONCEPTS,
} from "./relations.ts";
import type { Declaration, Edge } from "./relations.ts";
import { noteBody, splitLines } from "./locators.ts";
import type { PassageResult, SourceText } from "./locators.ts";

export type Ref = {
  notePath: string;
  kind: string;
  fragment?: string | null;
  nodeId?: string | null;
  typeName?: string | null;
  structuralFingerprint?: string | null;
};
/** A relationship endpoint as GraphQL returns it (`NodeRef`, selected with `structural`). */
export type Endpoint = {
  notePath: string;
  kind?: string | null;
  fragment?: string | null;
  nodeId?: string | null;
  typeName?: string | null;
  structural?: string | null;
};
/** The identity key of a ref: its note path, plus the fragment for an embedded node.
 * It is also the string `node(ref:)` resolves, and it equals a record's GraphQL `path`. */
export const refKey = (ref: { notePath: string; fragment?: string | null }) =>
  ref.fragment ? `${ref.notePath}#${ref.fragment}` : ref.notePath;
/** Whether an endpoint is a structured RECORD node rather than a Markdown note or section. */
export const isRecord = (e: { fragment?: string | null }) =>
  /^record(-pos)?:/.test(e.fragment ?? "");
/** A positional RECORD (AO `key: null`): its locator is bound to the file revision read. */
export const isPositional = (e: { fragment?: string | null }) =>
  (e.fragment ?? "").startsWith("record-pos:");
/** How a positional record is labelled wherever it is listed (SPEC-0133 R3). */
export const POSITIONAL = "Positional record: its link names this revision of the file";
/** The key of the record an endpoint belongs to: a RECORD node is its own record, while a
 * Markdown section (such as a claim's Connections) belongs to its note. */
export const ownerKey = (e: Endpoint) => (isRecord(e) ? refKey(e) : e.notePath);
export type RecordLink = {
  ref: Ref;
  path: string;
  aoId?: string | null;
  title: string;
  headline?: string | null;
  name?: string | null;
};
export type CitationRow = { recordedLink?: string | null; sources: RecordLink[] };
export type Assertion = RecordLink & {
  content: string;
  kind?: string | null;
  confidence?: string | null;
  status?: string | null;
  attestation?: string | null;
  validFrom?: string | null;
  validUntil?: string | null;
  reviewedBy?: string | null;
  reviewedAt?: string | null;
  updatedAt?: string | null;
  priority?: string | null;
  horizon?: string | null;
  speaker?: string | null;
  speakerRole?: string | null;
  extractedBy?: string | null;
  provenanceSource?: string | null;
  provenanceLocator?: string | null;
  provenanceSpeaker?: string | null;
  provenanceSpeakerRole?: string | null;
  provenanceExtractedBy?: string | null;
  provenanceQuoteHash?: string | null;
  contradictsRows?: string[];
  supportsRows?: string[];
  sourcesSection?: { citations: CitationRow[] };
  connectionsSection?: {
    ref: Ref;
    content: string;
    workspace: { fields: { name: string; values: string[] }[] };
  };
  workspace: { capabilities: { readOnlyReason?: string } };
  concepts: RecordLink[];
} & Partial<Record<`frontmatter${string}`, string[]>>;
export type Source = RecordLink & {
  content: string;
  sourcePaths: string[];
  frontmatter: { path?: unknown };
  locatorScheme?: string | null;
  kind?: string | null;
  speakerRole?: string | null;
  ingestedAt?: string | null;
  extractedAssertionIds: string[];
  workspace: { capabilities: { readOnlyReason?: string } };
};
export type Concept = RecordLink & { relatedConceptsSection?: { relatedConcepts: RecordLink[] } };
export type Artifact = RecordLink & {
  graphSyncedAt?: string | null;
  status?: string | null;
  supportedBy: RecordLink[];
  additionalAssertionReferences: RecordLink[];
  conceptReferences: RecordLink[];
  sourceReferences: RecordLink[];
  artifactReferences: RecordLink[];
};
export type Conflict = RecordLink & {
  fields: { label: string; value: string }[];
  assertions: RecordLink[];
};
export type Knowledge = {
  assertion: Assertion[];
  source: Source[];
  concept: Concept[];
  artifact: Artifact[];
  conflictRegistry: { groups: { entries: Conflict[] }[] }[];
};
export const CEILING = 500;
export const name = (r: RecordLink) => r.headline || r.name || r.title;
export const claimText = (a: Assertion) =>
  noteBody(a.content)
    .split(/^##[ \t]+(?:Connections|Sources|Referenced By)[ \t]*$/im)[0]
    .trim();
export function sourceText(s: Source): SourceText {
  const raw = s.frontmatter?.path;
  const paths = Array.isArray(raw)
    ? raw.map((p) => (typeof p === "string" ? p : ""))
    : typeof raw === "string"
      ? [raw]
      : (s.sourcePaths ?? []);
  return { path: s.path, paths, body: noteBody(s.content), scheme: s.locatorScheme || undefined };
}
export function physicalSourceLine(s: Source | undefined, path: string, bodyLine: number) {
  return (
    bodyLine +
    (s && s.path === path
      ? splitLines(s.content).length - splitLines(noteBody(s.content)).length
      : 0)
  );
}
export type Citation = {
  raw: string;
  source?: Source;
  slot?: number;
  origin: boolean;
  chosenBy?: string | null;
  locator?: string;
};
export function citations(a: Assertion, sources: Source[]): Citation[] {
  const resolve = makeResolver(
    sources.map((s) => ({ key: s.path, aoId: s.aoId, notePath: s.path })),
  );
  const rows = (a.sourcesSection?.citations ?? []).map((c, i) => {
    const raw = c.recordedLink ?? "";
    const key =
      c.sources.length === 1 ? c.sources[0].path : undefined;
    return { raw, source: sources.find((s) => s.path === key), slot: i + 1, origin: false };
  });
  if (!a.provenanceSource)
    return rows.map((r, i) => ({
      ...r,
      origin: i === 0,
      ...(i === 0
        ? { chosenBy: "first Sources link", locator: a.provenanceLocator || undefined }
        : {}),
    }));
  const key = resolve(a.provenanceSource);
  const at = key ? rows.findIndex((r) => r.source?.path === key) : -1;
  const origin: Citation = {
    ...(at >= 0
      ? rows[at]
      : { raw: a.provenanceSource, source: sources.find((s) => s.path === key) }),
    origin: true,
    chosenBy: "recorded in provenance.source",
    locator: a.provenanceLocator || undefined,
  };
  return [origin, ...rows.filter((_, i) => i !== at)];
}
export function origin(a: Assertion, sources: Source[]) {
  return citations(a, sources).find((c) => c.origin);
}
export const COVERAGE = [
  "Recorded passage",
  "Unsupported locator",
  "Document only",
  "Unresolved source/locator",
  "No source",
] as const;
export type Coverage = (typeof COVERAGE)[number];
export function coverage(c: Citation | undefined, passage: PassageResult | undefined): Coverage {
  if (!c) return "No source";
  if (!passage) return "Unresolved source/locator";
  switch (passage.status) {
    case "resolved":
      return "Recorded passage";
    case "unsupported":
      return "Unsupported locator";
    case "document-only":
      return "Document only";
    default:
      return "Unresolved source/locator";
  }
}
export function relationships(data: Knowledge) {
  const all = [...data.assertion, ...data.source, ...data.concept, ...data.artifact];
  const resolve = makeResolver(all.map((r) => ({ key: r.path, aoId: r.aoId, notePath: r.path })));
  const declarations: Declaration[] = [];
  for (const a of data.assertion) {
    for (const f of a.connectionsSection?.workspace.fields ?? []) {
      for (const raw of f.values)
        declarations.push({ from: a.path, field: f.name, raw, to: resolve(raw), origin: "body" });
    }
    for (const [field, values] of Object.entries(a)) {
      if (field.startsWith("frontmatter") && Array.isArray(values))
        for (const raw of values)
          if (typeof raw === "string")
            declarations.push({
              from: a.path,
              field,
              raw,
              to: resolve(raw),
              origin: "frontmatter",
            });
    }
  }
  const normalized = canonicalEdges(declarations);
  const edges: Edge[] = [...normalized.edges];
  const add = (from: string, to: string, rel: string) => {
    if (!edges.some((e) => e.from === from && e.to === to && e.rel === rel))
      edges.push({ from, to, rel, declaredBy: [] });
  };
  for (const a of data.assertion) {
    for (const c of citations(a, data.source)) if (c.source) add(a.path, c.source.path, CITES);
    for (const c of a.concepts) add(a.path, c.path, MENTIONS);
  }
  for (const s of data.source)
    for (const id of s.extractedAssertionIds ?? []) {
      const target = resolve(id);
      if (target) add(s.path, target, EXTRACTED);
    }
  for (const c of data.concept)
    for (const r of c.relatedConceptsSection?.relatedConcepts ?? [])
      add(c.path, r.path, RELATED_CONCEPTS);
  for (const a of data.artifact) {
    for (const r of a.supportedBy) add(a.path, r.path, ARTIFACT_SUPPORTED_BY);
    for (const r of [
      ...a.additionalAssertionReferences,
      ...a.conceptReferences,
      ...a.sourceReferences,
      ...a.artifactReferences,
    ])
      add(a.path, r.path, REFERENCES);
  }
  return { ...normalized, edges, all, resolve };
}
export function dateNumber(value?: string | null) {
  if (!value || !/^\d{4}-\d{2}-\d{2}(?:T.*)?$/.test(value)) return NaN;
  const day = value.slice(0, 10),
    n = Date.parse(value);
  return Number.isFinite(n) && new Date(Date.parse(day)).toISOString().slice(0, 10) === day
    ? n
    : NaN;
}
export function badges(a: Assertion, edges: Edge[], today: string) {
  return [
    edges.some((e) => e.rel === "supersedes" && e.to === a.path) ? "Superseded" : "",
    dateNumber(a.validUntil) < dateNumber(today) ? "Expired" : "",
  ].filter(Boolean);
}
export type Observation = {
  record: RecordLink;
  reason: string;
  basis: string;
  related?: RecordLink[];
  risk?: boolean;
};
export function conflictPair(c: Conflict, resolve: (r: string) => string | undefined) {
  const raw =
    c.fields.find(
      (f) => f.label.replace(/\*/g, "").replace(/:$/, "").toLowerCase() === "assertions",
    )?.value ?? "";
  return [...raw.matchAll(/\[\[([^\]]+)\]\]/g)]
    .map((m) => resolve(m[1]))
    .filter((v): v is string => !!v);
}
export function conflictField(c: Conflict, field: string) {
  return c.fields.find(
    (f) => f.label.replace(/\*/g, "").replace(/:$/, "").toLowerCase() === field.toLowerCase(),
  )?.value;
}
export function observations(data: Knowledge, today: string): Record<string, Observation[]> {
  const graph = relationships(data),
    edges = graph.edges;
  const groups: Record<string, Observation[]> = {
    "Contradictions recorded": [],
    "Open questions": [],
    "Superseded, expired, or archived": [],
    "Missing Sources links": [],
    "Dangling declared references": [],
    "Knowledge updated since sync": [],
    "Sync date unknown": [],
  };
  const conflicts = data.conflictRegistry.flatMap((r) => r.groups.flatMap((g) => g.entries));
  const pairs = new Set<string>();
  for (const e of edges.filter((e) => e.rel === "contradicts")) {
    const key = [e.from, e.to].sort().join("\0");
    if (pairs.has(key)) continue;
    pairs.add(key);
    const a = graph.all.find((r) => r.path === e.from),
      b = graph.all.find((r) => r.path === e.to);
    if (!a || !b) continue;
    const registry = conflicts.find((c) => {
      const pair = conflictPair(c, graph.resolve);
      return pair.includes(e.from) && pair.includes(e.to);
    });
    const state = registry && conflictField(registry, "State");
    groups["Contradictions recorded"].push({
      record: a,
      related: [b],
      reason: `Contradiction recorded${registry ? ` · Registry State: ${state || "Not recorded"} · Impact: ${conflictField(registry, "Impact") || "Not recorded"}` : " · No registry entry recorded"}`,
      basis: `${name(a)} contradicts ${name(b)}. ${registry ? `${registry.title}: recorded State ${state ?? "Not recorded"}; Impact ${conflictField(registry, "Impact") ?? "Not recorded"}.` : "No registry entry recorded."}`,
      risk: state === "open",
    });
  }
  for (const a of data.assertion) {
    if (
      a.kind === "question" &&
      a.status !== "retracted" &&
      !edges.some((e) => e.to === a.path && ["resolves", "supersedes"].includes(e.rel))
    )
      groups["Open questions"].push({
        record: a,
        reason: "Open question",
        basis: `Recorded kind question; status ${a.status || "Not recorded"}; no incoming resolves or supersedes in the loaded scope. Valid from ${a.validFrom || "Not recorded"}.`,
      });
    const statuses = badges(a, edges, today);
    if (statuses.length || a.status === "archived")
      groups["Superseded, expired, or archived"].push({
        record: a,
        reason: [
          ...statuses,
          ...(a.status === "archived" ? ["Recorded archived status"] : []),
        ].join(" · "),
        basis: [
          a.validUntil ? `Valid until ${a.validUntil}, compared with today ${today}.` : "",
          ...edges
            .filter((e) => e.to === a.path && e.rel === "supersedes")
            .map((e) => `${e.from} declares supersedes ${a.path}.`),
          `Recorded AO status: ${a.status || "Not recorded"}.`,
        ]
          .filter(Boolean)
          .join(" "),
      });
    if (!a.sourcesSection?.citations.length)
      groups["Missing Sources links"].push({
        record: a,
        reason: "No Sources link",
        basis: `No citation occurrence in ${a.path} › Sources. ${a.provenanceSource ? `Recorded provenance source: ${a.provenanceSource}.` : "No provenance source recorded."}`,
      });
  }
  for (const d of graph.dangling) {
    const record = graph.all.find((r) => r.path === d.from);
    if (record)
      groups["Dangling declared references"].push({
        record,
        reason: "Unrecognized or unresolved declaration",
        basis: `${d.origin === "body" ? "Connections" : "Frontmatter connections"} › ${d.as}: ${d.raw} is unresolved or ambiguous in the loaded scope.`,
        risk: true,
      });
  }
  for (const a of data.assertion)
    for (const c of citations(a, data.source))
      if (!c.source && !/^(view|review):/.test(c.raw))
        groups["Dangling declared references"].push({
          record: a,
          reason: "Unresolved source reference",
          basis: `${c.origin ? "Origin" : `Sources link #${c.slot}`}: ${c.raw} is unresolved or ambiguous in the loaded scope.`,
          risk: true,
        });
  for (const a of data.artifact) {
    const claims = data.assertion.filter((c) =>
      edges.some(
        (e) =>
          e.from === a.path &&
          e.to === c.path &&
          [REFERENCES, ARTIFACT_SUPPORTED_BY].includes(e.rel),
      ),
    );
    const synced = dateNumber(a.graphSyncedAt),
      unknown = claims.filter((c) => !Number.isFinite(dateNumber(c.updatedAt)));
    const changed = claims.filter((c) => dateNumber(c.updatedAt) > synced);
    if (changed.length)
      groups["Knowledge updated since sync"].push({
        record: a,
        reason: "Knowledge updated since sync",
        related: changed,
        basis: `Recorded graphSyncedAt ${a.graphSyncedAt}. ${changed.map((c) => `${name(c)} updated ${c.updatedAt}`).join("; ")}.`,
      });
    if (!Number.isFinite(synced) || unknown.length)
      groups["Sync date unknown"].push({
        record: a,
        reason: "Unknown",
        basis: !Number.isFinite(synced)
          ? `graphSyncedAt ${a.graphSyncedAt || "Not recorded"}; sync comparison is unknown.`
          : `Incomparable claim updatedAt: ${unknown.map(name).join(", ")}; comparison is unknown.`,
      });
  }
  return groups;
}
export function filterAssertions(rows: Assertion[], find: string, concept: string) {
  const text = find.toLocaleLowerCase().trim();
  return rows.filter(
    (a) =>
      (!concept || a.concepts.some((c) => c.path === concept)) &&
      (!text || `${name(a)} ${a.aoId ?? ""} ${claimText(a)}`.toLocaleLowerCase().includes(text)),
  );
}
