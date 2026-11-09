/** The Knowledge site's GraphQL text (SPEC-0135 R5), in one place so the views and the Go
 * integration tests run the same strings. Kit-free, like `model.ts`, so
 * `node --experimental-strip-types` can import it. Whole-KB roots ask for the public maximum
 * and report typed-root coverage; declared relation fields pass no `first`, which returns
 * every row. Nothing here reads a body. */

export const ROOT_MAX = 5000;
/** The `nodes(refs:)` maximum (SPEC-0136 R8). */
export const BATCH_MAX = 200;

const REF = "ref { notePath kind fragment nodeId typeName structuralFingerprint: structural }";
const CITATIONS = "sourcesSection { citations { recordedLink sources { path } } }";

/** The one body-free, whole-KB query every site page and the panel run. Exactly R5's fields. */
export const ORIENTATION_QUERY = `{
  assertion(first: ${ROOT_MAX}) { ${REF} path aoId title headline kind legacyKind confidence provenanceSource }
  source(first: ${ROOT_MAX}) { ${REF} path aoId title name headline recordedDate extractedAssertionIds }
  concept(first: ${ROOT_MAX}) {
    ${REF} path aoId title name headline aliases
    linkedAssertions { path } linkingAssertions { path }
    relatedConceptsSection { relatedConcepts { path } }
  }
  artifact(first: ${ROOT_MAX}) { ${REF} path aoId title name headline }
  mapOfContent(first: ${ROOT_MAX}) { ${REF} path aoId title }
}`;

/** The selection each batched assertion read returns: its Sources-section citations. */
export const CITATION_SELECTION = `... on Assertion { path ${CITATIONS} }`;

/** One `nodes(refs:)` query per batch of at most BATCH_MAX refs, with `first` set to the
 * batch size so no ref falls past the default page.
 * ponytail: stands in for the kit's `readNodes` (SPEC-0136 R7) until it lands; switch the
 * views to `readNodes(refs, selection)` then, which also merges coverage. */
export function batchedNodeQueries(refs: readonly string[], selection: string): string[] {
  const unique = [...new Set(refs)];
  const queries: string[] = [];
  for (let at = 0; at < unique.length; at += BATCH_MAX) {
    const batch = unique.slice(at, at + BATCH_MAX);
    queries.push(
      `{ nodes(refs: ${JSON.stringify(batch)}, first: ${batch.length}) { items { requestedRef node { ${selection} } error { code message } } pageInfo { truncated } } }`,
    );
  }
  return queries;
}

/** Concept scope: the Sources-section citations of its member assertions, batched. */
export const conceptScopeQueries = (memberPaths: readonly string[]) =>
  batchedNodeQueries(memberPaths, CITATION_SELECTION);

/** Source scope: every assertion that links the source, with its citations (declared, so no
 * `first`), then the provenance-only candidates the orientation query resolved to it. The
 * caller passes those not already among `linkingAssertions`; `citations()` decides. */
export const sourceScopeQuery = (sourcePath: string) =>
  `{ node(ref: ${JSON.stringify(sourcePath)}) { ... on Source { path linkingAssertions { path ${CITATIONS} } } } }`;
export const sourceProvenanceQueries = (assertionPaths: readonly string[]) =>
  batchedNodeQueries(assertionPaths, CITATION_SELECTION);

/** Artifact scope: its recorded support values and references, then, batched, the citations of
 * support values that resolve to assertions not already read. */
export const artifactScopeQuery = (artifactPath: string) =>
  `{ node(ref: ${JSON.stringify(artifactPath)}) { ... on Artifact { path supportValues additionalAssertionReferences { path ${CITATIONS} } conceptReferences { path } } } }`;
export const artifactSupportQueries = (assertionPaths: readonly string[]) =>
  batchedNodeQueries(assertionPaths, CITATION_SELECTION);

/** Review's submission and curation lists: their own body-free query. The conflict registry
 * and the other curation documents form one inventory. */
export const REVIEW_DOCUMENTS_QUERY = `{
  submission(first: ${ROOT_MAX}) { ${REF} path title status recordType }
  curationDocument(first: ${ROOT_MAX}) { ${REF} path title recordedUpdatedAt }
  conflictRegistry(first: ${ROOT_MAX}) { ${REF} path title recordedUpdatedAt }
}`;
