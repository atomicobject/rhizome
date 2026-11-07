import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { graphql, useGraphQL } from "@rhizome/kit";
import {
  CEILING,
  citations,
  coverage,
  origin,
  ownerKey,
  relationships,
  sourceText,
} from "./lib/knowledge.ts";
import type { Knowledge, Assertion, Coverage, Endpoint, Ref } from "./lib/knowledge.ts";
import { readPassage } from "./lib/passage.ts";
import { readSavedAnalysis } from "./lib/saved-analysis.ts";
import { EVIDENCED_BY, typeSpec } from "./lib/model.ts";
import type { FieldLink, ModelFile, ModelRecord, SourceValue, TypeSpec } from "./lib/model.ts";

// Composite section-scoped citations and normalized AO relations need GraphQL;
// a native collection row does not carry the origin-citation coverage contract.
// The resolver reads at most 500 records per root. Hitting a ceiling is explicit.
export const QUERY = `{
  assertion(first: 500) {
    ref { notePath kind fragment nodeId typeName structuralFingerprint: structural }
    path aoId title headline content kind confidence status attestation validFrom validUntil
    reviewedBy reviewedAt updatedAt priority horizon speaker speakerRole extractedBy
    provenanceSource provenanceLocator provenanceSpeaker provenanceSpeakerRole provenanceExtractedBy provenanceQuoteHash
    contradictsRows supportsRows
    frontmatterSupports frontmatterRefines frontmatterDependsOn frontmatterContradicts frontmatterSupersedes
    frontmatterResolves frontmatterRelatesTo frontmatterCorroboratedBy frontmatterSupportedBy frontmatterSupersededBy frontmatterResolvedBy
    workspace { capabilities { readOnlyReason } }
    sourcesSection { citations { recordedLink sources { ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path aoId title headline } } }
    connectionsSection { ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } content workspace { fields { name values } } }
    concepts(first: 500) { ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path aoId title headline name }
  }
  source(first: 500) {
    ref { notePath kind fragment nodeId typeName structuralFingerprint: structural }
    path aoId title name headline content frontmatter sourcePaths locatorScheme kind speakerRole ingestedAt extractedAssertionIds
    workspace { capabilities { readOnlyReason } }
  }
  concept(first: 500) {
    ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path aoId title headline name
    relatedConceptsSection { relatedConcepts(first: 500) { ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path aoId title headline name } }
  }
  artifact(first: 500) {
    ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path aoId title headline name status graphSyncedAt
    supportedBy { ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path aoId title headline }
    additionalAssertionReferences(first: 500) { ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path aoId title headline }
    conceptReferences(first: 500) { ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path aoId title headline name }
    sourceReferences(first: 500) { ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path aoId title headline name }
    artifactReferences(first: 500) { ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path aoId title headline name }
  }
  conflictRegistry(first: 500) {
    groups { entries {
      ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path title
      assertions(first: 500) { ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path aoId title headline }
      fields { label value }
    } }
  }
}`;

export function useKnowledge() {
  const query = useGraphQL<Knowledge>(QUERY);
  const graph = useMemo(() => (query.data ? relationships(query.data) : undefined), [query.data]);
  const capped =
    query.data &&
    [
      query.data.assertion,
      query.data.source,
      query.data.concept,
      query.data.artifact,
      query.data.conflictRegistry,
    ].some((rows) => rows.length >= CEILING);
  return { query, data: query.data, graph, capped };
}
export function useSavedAnalysis() {
  return useQuery({ queryKey: ["ao-kb", "saved-analysis"], queryFn: () => readSavedAnalysis() });
}
/** Origin-citation coverage for `rows` (every loaded claim by default). */
export function useCoverage(data: Knowledge | undefined, rows = data?.assertion) {
  return useQuery({
    queryKey: ["ao-kb", "origin-coverage", data, rows?.map((a) => a.path)],
    enabled: !!data,
    queryFn: async () => {
      const results = await Promise.all(
        (rows ?? []).map(async (a) => {
          const c = origin(a, data?.source ?? []);
          const result = c
            ? await readPassage(c.source && sourceText(c.source), c.locator, c.raw)
            : undefined;
          return [a.path, coverage(c, result)] as const;
        }),
      );
      return new Map<string, Coverage>(results);
    },
  });
}
export function useCitationPassages(a: Assertion, data: Knowledge) {
  const rows = citations(a, data.source);
  return useQuery({
    queryKey: ["ao-kb", "citations", a.path, rows],
    queryFn: async () =>
      Promise.all(
        rows.map(async (c) => ({
          citation: c,
          passage: await readPassage(c.source && sourceText(c.source), c.locator, c.raw),
        })),
      ),
  });
}
/** Whether product-model bindings are published: the generated SDL declares
 * `AOKBModelRecord`. Its concrete types are the model record types. */
export function useModelBindings() {
  const query = useGraphQL<{ __type: { possibleTypes: { name: string }[] } | null }>(
    `{ __type(name: "AOKBModelRecord") { possibleTypes { name } } }`,
  );
  return {
    ...query,
    data: query.data && {
      ready: !!query.data.__type,
      types: query.data.__type?.possibleTypes.map((t) => t.name) ?? [],
    },
  };
}

export const REF =
  "ref { notePath kind fragment nodeId typeName structuralFingerprint: structural }";
export const ENDPOINT = "notePath kind fragment nodeId typeName structural";
/** At most this many model records are read for one Attention pass. */
export const MODEL_CEILING = 500;
type RawModel = {
  __typename: string;
  title: string;
  ref: Ref;
  aoValue?: string | null;
  workspace?: {
    source?: { value?: SourceValue; address?: string } | null;
    fields?: { name: string; links: FieldLink[] }[];
  } | null;
  evidence?: { truncated: boolean; edges: { target: Endpoint }[] };
};
/** Model records by key (`notePath#fragment`), each with its native evidence edges. */
export async function readModelRecords(keys: string[]): Promise<ModelRecord[]> {
  if (!keys.length) return [];
  const data = await graphql<Record<string, RawModel | null>>(`{ ${keys
    .map(
      (key, i) => `n${i}: node(ref: ${JSON.stringify(key)}) { __typename title ${REF}
          ... on AOKBModelRecord { aoValue } workspace { source fields { name links { value resolved title ${REF} } } }
          evidence: neighborhood(direction: OUTBOUND, relation: "${EVIDENCED_BY}", first: 200) { truncated edges { target { ${ENDPOINT} } } } }`,
    )
    .join("\n")} }`);
  return keys.flatMap((key, i) => {
    const r = data[`n${i}`];
    return r && r.aoValue !== undefined
      ? [
          {
            key,
            ref: r.ref,
            title: r.title,
            typeName: r.__typename,
            aoValue: r.aoValue,
            value: r.workspace?.source?.value,
            evidence: r.evidence?.edges.map((e) => ownerKey(e.target)) ?? [],
            evidenceTruncated: !!r.evidence?.truncated,
            links: Object.fromEntries((r.workspace?.fields ?? []).map((f) => [f.name, f.links])),
          },
        ]
      : [];
  });
}
/** Every model source file with its recorded `synced`, and the records each holds. */
export async function readModelFiles() {
  const data = await graphql<{
    files: (ModelFile & { ref: Ref; workspace: { structure: { ref: Ref }[] } | null })[];
  }>(`{ files: aOKBModelFile(first: 201) { ${REF} synced workspace { structure { ${REF} } } } }`);
  return data.files.map((f) => ({
    notePath: f.ref.notePath,
    synced: f.synced,
    records: (f.workspace?.structure ?? [])
      .filter((s) => s.ref.typeName && s.ref.typeName !== "AOKBModelFile" && s.ref.fragment)
      .map((s) => `${s.ref.notePath}#${s.ref.fragment}`),
  }));
}
/** Labels and `@references` declarations for each type, from the public type docs. */
export async function readTypeSpecs(names: string[]) {
  const specs: Record<string, TypeSpec> = {};
  await Promise.all(
    [...new Set(names)].map(async (name) => {
      const response = await fetch(`/api/v1/ontology/types/${encodeURIComponent(name)}?notes=none`);
      if (!response.ok) throw new Error(`Type documentation for ${name}: HTTP ${response.status}`);
      specs[name] = typeSpec(await response.json(), name);
    }),
  );
  return specs;
}
/** Model records whose declared evidence names this claim (incoming evidenced-by). */
export function useModelCiters(path: string, ready: boolean) {
  return useQuery({
    queryKey: ["ao-kb", "model-citers", path],
    enabled: ready,
    queryFn: async () => {
      const data = await graphql<{
        node: { neighborhood: { truncated: boolean; edges: { target: Endpoint }[] } } | null;
      }>(
        `query Citers($ref: String!) { node(ref: $ref) { neighborhood(direction: INBOUND, relation: "${EVIDENCED_BY}", first: 200) { truncated edges { target { ${ENDPOINT} } } } } }`,
        { ref: path },
      );
      const ends = (data.node?.neighborhood.edges ?? []).map((e) => ownerKey(e.target));
      const [records, files] = await Promise.all([
        readModelRecords([...new Set(ends)]),
        readModelFiles(),
      ]);
      return {
        records,
        files,
        truncated: !!data.node?.neighborhood.truncated,
        specs: await readTypeSpecs(records.map((r) => r.typeName)),
      };
    },
  });
}
/** Every model record in the selected product, bounded by MODEL_CEILING. */
export function useModelIndex(ready: boolean) {
  return useQuery({
    queryKey: ["ao-kb", "model-index"],
    enabled: ready,
    queryFn: async () => {
      const files = await readModelFiles();
      const keys = files.flatMap((f) => f.records);
      const records = await readModelRecords(keys.slice(0, MODEL_CEILING));
      return {
        files,
        records,
        capped: keys.length > MODEL_CEILING || files.length > 200,
        specs: await readTypeSpecs(records.map((r) => r.typeName)),
      };
    },
  });
}
