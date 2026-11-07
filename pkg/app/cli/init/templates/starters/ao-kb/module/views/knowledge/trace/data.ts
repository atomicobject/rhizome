import {
  ARTIFACT_SUPPORTED_BY,
  CITES,
  MENTIONS,
  RELATED_CONCEPTS,
  REFERENCES,
  canonicalEdges,
  makeResolver,
  type Declaration,
} from "../lib/relations.ts";
import {
  edgeKey,
  type Expansion,
  type Record as KnowledgeRecord,
  type Stop,
  type TraceEdge,
} from "./model.ts";
import type { Reader } from "./traverse.ts";

const REF = "ref { notePath kind fragment nodeId typeName structuralFingerprint: structural }";
const SUMMARY = `__typename path title aoId ${REF} headline`;
const DIRECTORY_CAP = 200;
export const DIRECTORY_QUERY = `{ assertion(first: 201) { ${SUMMARY} } source(first: 201) { ${SUMMARY} name } concept(first: 201) { ${SUMMARY} name } artifact(first: 201) { ${SUMMARY} name } }`;
export type DirectoryData = {
  assertion: KnowledgeRecord[];
  source: KnowledgeRecord[];
  concept: KnowledgeRecord[];
  artifact: KnowledgeRecord[];
};
export function directory(data: DirectoryData) {
  return {
    records: Object.values(data).flatMap((rows) => rows.slice(0, DIRECTORY_CAP)),
    capped: Object.values(data).some((rows) => rows.length > DIRECTORY_CAP),
  };
}
const FM = [
  "frontmatterSupports",
  "frontmatterRefines",
  "frontmatterDependsOn",
  "frontmatterContradicts",
  "frontmatterSupersedes",
  "frontmatterResolves",
  "frontmatterRelatesTo",
  "frontmatterCorroboratedBy",
  "frontmatterSupportedBy",
  "frontmatterSupersededBy",
  "frontmatterResolvedBy",
] as const;
const BODY = new Set([
  "supports",
  "refines",
  "dependsOn",
  "contradicts",
  "supersedes",
  "resolves",
  "duplicate",
  "relatesTo",
  "tracesTo",
  "grounds",
  "exemplifies",
  "implements",
  "actorOf",
  "elementDependsOn",
  "supportedBy",
  "refinedBy",
  "contradictedBy",
  "supersededBy",
  "resolvedBy",
  "addressedBy",
  "blocks",
  "groundedBy",
  "corroboratedBy",
]);
type Neighborhood = {
  edges: { relation: string; target: { notePath: string } }[];
  nodes: { ref: { notePath: string } }[];
  truncated: boolean;
};
type Section = { ref: KnowledgeRecord["ref"]; neighborhood: Neighborhood };
type Citation = { recordedLink: string | null; sources: { path: string }[]; sourcesCount: number };
type Assertion = KnowledgeRecord & {
  __typename: "Assertion";
  provenanceSource: string | null;
  sourcesSection: { ref: KnowledgeRecord["ref"]; citations: Citation[] } | null;
  connectionsSection: Section | null;
  concepts: { path: string }[];
  conceptsCount: number;
} & { [K in (typeof FM)[number]]: string[] | null };
type Source = KnowledgeRecord & { __typename: "Source"; extractedAssertionIds: string[] | null };
type Concept = KnowledgeRecord & { __typename: "Concept"; relatedConceptsSection: Section | null };
type Artifact = KnowledgeRecord & {
  __typename: "Artifact";
  neighborhood: Neighborhood;
  additionalAssertionReferences: { path: string }[];
  conceptReferences: { path: string }[];
  sourceReferences: { path: string }[];
  artifactReferences: { path: string }[];
  additionalAssertionReferencesCount: number;
  conceptReferencesCount: number;
  sourceReferencesCount: number;
  artifactReferencesCount: number;
};
type Detail = Assertion | Source | Concept | Artifact;
export type Query = <T>(document: string) => Promise<T>;
export type Extraction = { id: string; record?: KnowledgeRecord; cites: boolean | null };
const neighborhood = (cap: number, direction = "OUTBOUND") =>
  `neighborhood(direction: ${direction}, first: ${cap}) { edges { relation target { notePath } } nodes { ref { notePath } } truncated }`;
const section = (cap: number) => `{ ${REF} ${neighborhood(cap)} }`;
const selection = (cap: number) => `__typename path title ${REF}
  ... on Assertion { aoId headline provenanceSource ${FM.join(" ")}
    sourcesSection { ${REF} citations { recordedLink sources(first: ${cap}) { path } sourcesCount } }
    connectionsSection ${section(cap)} concepts(first: ${cap}) { path } conceptsCount }
  ... on Source { aoId headline name extractedAssertionIds }
  ... on Concept { aoId headline name relatedConceptsSection { ${REF} ${neighborhood(cap)} relatedConcepts(first: ${cap}) { path } relatedConceptsCount } }
  ... on Artifact { aoId headline name ${neighborhood(cap)}
    additionalAssertionReferences(first: ${cap}) { path } additionalAssertionReferencesCount
    conceptReferences(first: ${cap}) { path } conceptReferencesCount
    sourceReferences(first: ${cap}) { path } sourceReferencesCount
    artifactReferences(first: ${cap}) { path } artifactReferencesCount }`;

/** The identity directory reads only the first 200 identities per type, never edges.
 * Traversal queries only frontier owners and their bounded incoming candidates.
 * Raw ids/slugs outside that directory remain explicitly unresolved/ambiguous. */
export function createReader(query: Query, records: KnowledgeRecord[], budget: number) {
  const byPath = new Map(records.map((r) => [r.path, r]));
  const resolverFor = (type: string) =>
    makeResolver(
      records
        .filter((r) => r.__typename === type)
        .map((r) => ({ key: r.path, aoId: r.aoId, notePath: r.path })),
    );
  const resolveAssertion = resolverFor("Assertion");
  const resolveSource = resolverFor("Source");
  const cache = new Map<string, Detail>();
  const edges = new Map<string, TraceEdge>();
  const diagnostics = new Set<string>();
  const limited = new Set<string>();
  const merge = (edge: TraceEdge) => {
    const key = edgeKey(edge);
    const old = edges.get(key);
    if (!old) {
      edges.set(key, edge);
      return;
    }
    for (const location of edge.locations)
      if (!old.locations.some((l) => l.path === location.path && l.section === location.section))
        old.locations.push(location);
    for (const declaration of edge.declaredBy)
      if (
        !old.declaredBy.some(
          (d) =>
            d.record === declaration.record &&
            d.as === declaration.as &&
            d.origin === declaration.origin,
        )
      )
        old.declaredBy.push(declaration);
  };
  const add = (
    from: string,
    to: string | undefined,
    rel: string,
    sectionName: string,
    ref?: KnowledgeRecord["ref"],
  ) => {
    if (!to || !byPath.has(to) || from === to) return;
    merge({
      from,
      to,
      rel,
      declaredBy: [{ record: from, as: rel, origin: "body" }],
      locations: [{ path: from, section: sectionName, ref: ref ?? byPath.get(from)?.ref }],
    });
  };
  const rawTarget = (from: string, raw: string, type = "Assertion") => {
    const target = (type === "Source" ? resolveSource : resolveAssertion)(raw);
    if (!target) diagnostics.add(`Unresolved or ambiguous reference “${raw}” in ${from}.`);
    return target;
  };
  const citationTarget = (citation: Citation, owner: string) => {
    const raw = citation.recordedLink?.match(/\[\[([^\]]+)\]\]/)?.[1];
    if (citation.sources.length) return citation.sources.map((source) => source.path);
    const resolved = raw ? rawTarget(owner, raw, "Source") : undefined;
    return resolved ? [resolved] : [];
  };
  const ingest = (record: Detail, cap: number) => {
    const path = record.path;
    if (record.__typename === "Assertion") {
      const citations = record.sourcesSection?.citations ?? [];
      const targets = citations.map((c) => citationTarget(c, path));
      const firstRecorded = citations[0]?.recordedLink?.match(/\[\[([^\]]+)\]\]/)?.[1];
      const origin = record.provenanceSource
        ? rawTarget(path, record.provenanceSource, "Source")
        : firstRecorded
          ? rawTarget(path, firstRecorded, "Source")
          : targets[0]?.[0];
      const originSlot = targets.findIndex((group) => group.includes(origin ?? ""));
      citations.forEach((citation, i) => {
        if (citation.sourcesCount > citation.sources.length) {
          limited.add(path);
          diagnostics.add(
            `${path} › Sources #${i + 1}: ${citation.sourcesCount - citation.sources.length} more source targets beyond the read cap.`,
          );
        }
      });
      targets.forEach((group, i) =>
        group.forEach((target) =>
          add(
            path,
            target,
            CITES,
            `Sources #${i + 1}${i === originSlot && target === origin ? " (origin)" : ""}`,
            record.sourcesSection?.ref,
          ),
        ),
      );
      if (record.provenanceSource && origin && originSlot < 0)
        add(path, origin, CITES, "provenance.source (origin)");
      record.concepts.forEach((target) => add(path, target.path, MENTIONS, "Concept link"));
      if (record.conceptsCount > record.concepts.length) {
        diagnostics.add(
          `${path}: ${record.conceptsCount - record.concepts.length} more concept targets beyond the read cap.`,
        );
      }
      if (record.conceptsCount > record.concepts.length || record.concepts.length === cap)
        limited.add(path);
      const declarations: Declaration[] = [];
      const connections = record.connectionsSection;
      if (connections) {
        if (connections.neighborhood.truncated) limited.add(path);
        for (const edge of connections.neighborhood.edges)
          if (BODY.has(edge.relation))
            declarations.push({
              from: path,
              field: edge.relation,
              to: edge.target.notePath,
              origin: "body",
            });
      }
      for (const field of FM)
        for (const raw of record[field] ?? [])
          declarations.push({
            from: path,
            field,
            to: rawTarget(
              path,
              raw,
              field === "frontmatterCorroboratedBy" ? "Source" : "Assertion",
            ),
            raw,
            origin: "frontmatter",
          });
      for (const edge of canonicalEdges(declarations).edges)
        merge({
          ...edge,
          locations: edge.declaredBy.map((by) => ({
            path: by.record,
            section: by.origin === "body" ? "Connections" : `frontmatter.connections.${by.as}`,
            ref: by.origin === "body" ? connections?.ref : byPath.get(by.record)?.ref,
          })),
        });
    } else if (record.__typename === "Concept") {
      const related = record.relatedConceptsSection;
      if (
        related?.neighborhood.truncated ||
        (related &&
          (related.relatedConceptsCount > related.relatedConcepts.length ||
            related.relatedConcepts.length === cap))
      )
        limited.add(path);
      for (const target of related?.relatedConcepts ?? [])
        add(path, target.path, RELATED_CONCEPTS, "Related Concepts", related?.ref);
    } else if (record.__typename === "Artifact") {
      if (record.neighborhood.truncated) limited.add(path);
      for (const edge of record.neighborhood.edges)
        if (edge.relation === "supportedBy")
          add(path, edge.target.notePath, ARTIFACT_SUPPORTED_BY, "frontmatter.supported_by");
      for (const field of [
        "additionalAssertionReferences",
        "conceptReferences",
        "sourceReferences",
        "artifactReferences",
      ] as const) {
        for (const target of record[field]) add(path, target.path, REFERENCES, "Body reference");
        const count = record[`${field}Count`];
        if (count > record[field].length)
          diagnostics.add(
            `${path}: ${count - record[field].length} more ${field} targets beyond the read cap.`,
          );
        if (count > record[field].length || record[field].length === cap) limited.add(path);
      }
    }
  };
  const describe = async (paths: string[], cap: number) => {
    const missing = [...new Set(paths)].filter((path) => byPath.has(path) && !cache.has(path));
    const selected = missing.slice(0, Math.max(0, budget - cache.size));
    if (selected.length) {
      const response = await query<{ [key: string]: Detail | null }>(
        `{ ${selected.map((path, i) => `n${i}: node(ref: ${JSON.stringify(path)}) { ${selection(cap)} }`).join("\n")} }`,
      );
      selected.forEach((path, i) => {
        const record = response[`n${i}`];
        if (record) {
          cache.set(path, record);
          ingest(record, cap);
        }
      });
    }
    return selected.length < missing.length;
  };
  const incoming = async (paths: string[], cap: number) => {
    const fields: string[] = [];
    paths.forEach((path, i) => {
      fields.push(`n${i}: node(ref: ${JSON.stringify(path)}) { ${neighborhood(cap, "INBOUND")} }`);
      const record = byPath.get(path);
      const refs = [path, path.split("/").pop()?.replace(/\.md$/, ""), record?.aoId].filter(
        Boolean,
      );
      const rawFields =
        record?.__typename === "Source"
          ? ["provenanceSource", "frontmatterCorroboratedBy"]
          : record?.__typename === "Assertion"
            ? FM
            : [];
      rawFields.forEach((field, j) =>
        fields.push(
          `r${i}_${j}: assertion(first: ${cap}, filters: [{field: "${field}", op: in, values: ${JSON.stringify(refs)}}]) { path }`,
        ),
      );
    });
    const response = await query<{
      [key: string]: { neighborhood: Neighborhood } | { path: string }[] | null;
    }>(`{ ${fields.join("\n")} }`);
    const candidates = new Set<string>();
    let capped = false;
    for (const value of Object.values(response)) {
      if (Array.isArray(value)) {
        value.forEach((r) => candidates.add(r.path));
        if (value.length === cap) capped = true;
      } else if (value) {
        value.neighborhood.nodes.forEach((r) => candidates.add(r.ref.notePath));
        capped ||= value.neighborhood.truncated;
      }
    }
    return { paths: [...candidates].filter((path) => byPath.has(path)), capped };
  };
  const read: Reader = async (paths, direction, hop, remaining) => {
    const cap = Math.max(2, Math.min(200, remaining + 1));
    const directionName =
      direction === "up" ? "upstream" : direction === "down" ? "downstream" : "related records";
    let budgetHit = await describe(paths, cap);
    let capped = paths.some((path) => limited.has(path));
    const needsIncoming =
      direction === "down" ||
      (direction === "related" &&
        paths.some((path) =>
          ["Assertion", "Concept"].includes(byPath.get(path)?.__typename ?? ""),
        ));
    if (needsIncoming && !budgetHit) {
      const candidates = await incoming(paths, cap);
      capped ||= candidates.capped;
      budgetHit = await describe(candidates.paths, cap);
    }
    const stop: Stop | undefined = budgetHit
      ? { direction: directionName, hop, reason: "budget" }
      : capped
        ? {
            direction: directionName,
            hop,
            reason: "field",
            detail: `A frontier neighborhood reached its ${cap}-link read cap.`,
          }
        : undefined;
    return {
      edges: [...edges.values()].filter((e) => paths.includes(e.from) || paths.includes(e.to)),
      records,
      stop,
      diagnostics: [...diagnostics],
    } satisfies Expansion;
  };
  const extraction = async (path: string): Promise<Extraction[]> => {
    const source = cache.get(path);
    if (source?.__typename !== "Source") return [];
    const ids = source.extractedAssertionIds ?? [];
    const rows = ids
      .slice(0, budget)
      .map((id) => ({ id, record: byPath.get(resolveAssertion(id) ?? "") }));
    await describe(
      rows.flatMap((row) => (row.record ? [row.record.path] : [])),
      budget,
    );
    return rows.map((row) => ({
      ...row,
      cites:
        row.record && cache.has(row.record.path)
          ? [...edges.values()].some(
              (e) => e.from === row.record?.path && e.to === path && e.rel === CITES,
            )
          : null,
    }));
  };
  return { read, extraction, cache };
}
