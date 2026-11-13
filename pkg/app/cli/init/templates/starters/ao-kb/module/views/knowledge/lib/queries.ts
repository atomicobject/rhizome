/** The Knowledge site's GraphQL text (SPEC-0135 R5), in one place so the views and the Go
 * integration tests run the same strings. Kit-free, like `model.ts`, so
 * `node --experimental-strip-types` can import it. Whole-KB roots ask for the public maximum
 * and report typed-root coverage; declared relation fields pass no `first`, which returns
 * every row. Nothing here reads a body. */

export const ROOT_MAX = 5000;

const REF = "ref { notePath kind fragment nodeId typeName structuralFingerprint: structural }";
const CITATIONS = "sourcesSection { citations { recordedLink sources { path } } }";

/** The body-free, whole-KB query every site page and the panel run: R5's fields except the
 * Related Concepts pairs. It reads only the index, so the server's read cache serves one
 * execution per generation to every frame (SPEC-0136 R4). */
export const ORIENTATION_QUERY = `{
  assertion(first: ${ROOT_MAX}) { ${REF} path aoId title headline kind legacyKind confidence provenanceSource }
  source(first: ${ROOT_MAX}) { ${REF} path aoId title name headline recordedDate extractedAssertionIds }
  concept(first: ${ROOT_MAX}) {
    ${REF} path aoId title name headline aliases
    linkedAssertions { path } linkingAssertions { path }
  }
  artifact(first: ${ROOT_MAX}) { ${REF} path aoId title name headline }
  mapOfContent(first: ${ROOT_MAX}) { ${REF} path aoId title }
}`;

/** R5's Related Concepts pairs, read beside the orientation query: the section is projected
 * from each concept's note file, which the read cache never serves (SPEC-0136 R4). */
export const RELATED_CONCEPTS_QUERY = `{ concept(first: ${ROOT_MAX}) { path relatedConceptsSection { relatedConcepts { path } } } }`;

/** The selection each batched assertion read returns: its Sources-section citations. Record
 * pages pass it to the kit's `readNodes(refs, CITATION_SELECTION)` (SPEC-0136 R7), which chunks
 * refs at the `nodes(refs:)` maximum, merges by canonical identity, and reports coverage:
 * a concept's member assertions, a source's provenance-only candidates, and an artifact's
 * resolved support values not already read. */
export const CITATION_SELECTION = `... on Assertion { path ${CITATIONS} }`;

/** Concept scope: its member assertions in both link directions, with their citations. It runs
 * beside the orientation query, so the concept page need not wait for that query to name its
 * members before reading them; members it does not return follow with `readNodes`. */
export const conceptScopeQuery = (conceptPath: string) =>
  `{ node(ref: ${JSON.stringify(conceptPath)}) { ... on Concept { path linkedAssertions { ${CITATION_SELECTION} } linkingAssertions { ${CITATION_SELECTION} } } } }`;

/** Source scope: every assertion that links the source, with its citations (declared, so no
 * `first`). The provenance-only candidates the orientation query resolved to the source and
 * that are not among `linkingAssertions` are read with `readNodes`; `citations()` decides. */
export const sourceScopeQuery = (sourcePath: string) =>
  `{ node(ref: ${JSON.stringify(sourcePath)}) { ... on Source { path linkingAssertions { path ${CITATIONS} } } } }`;

/** Artifact scope: its recorded support values and references. The citations of support values
 * that resolve to assertions not already read follow with `readNodes`. */
export const artifactScopeQuery = (artifactPath: string) =>
  `{ node(ref: ${JSON.stringify(artifactPath)}) { ... on Artifact { path supportValues additionalAssertionReferences { path ${CITATIONS} } conceptReferences { path } } } }`;

/** Review's submission and curation lists: their own body-free query. The conflict registry
 * and the other curation documents form one inventory. */
export const REVIEW_DOCUMENTS_QUERY = `{
  submission(first: ${ROOT_MAX}) { ${REF} path title status recordType }
  curationDocument(first: ${ROOT_MAX}) { ${REF} path title recordedUpdatedAt }
  conflictRegistry(first: ${ROOT_MAX}) { ${REF} path title recordedUpdatedAt }
}`;
