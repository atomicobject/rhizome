import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useGraphQL } from "@rhizome/kit";
import {
  CEILING,
  citations,
  coverage,
  origin,
  relationships,
  sourceText,
} from "./lib/knowledge.ts";
import type { Knowledge, Assertion, Coverage } from "./lib/knowledge.ts";
import { readPassage } from "./lib/passage.ts";
import { readSavedAnalysis } from "./lib/saved-analysis.ts";

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
export function useCoverage(data: Knowledge | undefined) {
  return useQuery({
    queryKey: ["ao-kb", "origin-coverage", data],
    enabled: !!data,
    queryFn: async () => {
      const rows = await Promise.all(
        (data?.assertion ?? []).map(async (a) => {
          const c = origin(a, data?.source ?? []);
          const result = c
            ? await readPassage(c.source && sourceText(c.source), c.locator, c.raw)
            : undefined;
          return [a.path, coverage(c, result)] as const;
        }),
      );
      return new Map<string, Coverage>(rows);
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
