---
type: TechnicalSpec
summary: "Defines format-aware ontology projection for note roots, including typed and fallback HTML roots, canonical root-metadata semantics, and root-owned visible and supplemental search evidence."
id: SPEC-0086
spec-status: active
last-updated: 2026-09-11
aliases:
  - SPEC-0086
  - Format-aware root ontology projection
---

# Format-aware root ontology projection

## Summary

Rhizome will project every configured note format through one ontology-owned root-node contract. A format provider owns authored syntax and reports format-neutral root metadata, source ranges, links, diagnostics, and search regions. Ontology owns the meaning of those facts: type resolution, canonical fields, relations, typed or fallback identity, validation, read hydration, and root-owned search evidence.

Markdown continues to supply YAML frontmatter and its existing structural projections. HTML supplies plain JSON root metadata through its canonical metadata element. The two syntaxes share canonical metadata-key meanings after parsing. In HTML v1, ontology projection stops at the file-backed note root: headings, elements, fragments, and other DOM structures do not become sections, embedded nodes, or ontology containment.

A valid HTML root can therefore behave like an equivalent Markdown root across typed query, graph, validation, search, CLI, agent/MCP, and web read surfaces. HTML without usable type metadata remains a first-class internal fallback root. Duplicate or syntactically malformed canonical HTML metadata produces a provider-owned metadata diagnostic and an untyped fallback root without discarding independently parseable visible text, supplemental content, or links. Metadata-dependent mutations validate the source and fail closed until the metadata is repaired. Syntactically valid metadata whose `type` is unknown or violates the ontology follows the same assessment policy as equivalent Markdown metadata; the HTML provider does not silently reinterpret that ontology error as absent metadata.

## Goals

- separate format-owned source syntax from ontology-owned metadata semantics
- project configured HTML notes as typed roots when canonical root metadata is valid
- preserve searchable and navigable fallback roots when HTML is untyped or its metadata is unusable
- give equivalent Markdown and HTML metadata the same field, relation, validation, and query meanings
- keep HTML v1 root-only without implicitly treating DOM structure as ontology structure
- make visible and supplemental HTML search regions available as root-owned lexical and semantic evidence
- preserve source locators and ranges needed by format-specific, source-preserving maintenance operations

## Non-Goals

- defining the HTML metadata element, HTML text extraction, or HTML link parser; `SPEC-0085` owns those source-syntax contracts
- creating ontology sections or embedded nodes from HTML headings, elements, IDs, named anchors, microdata, RDFa, JSON-LD, or DOM selectors
- interpreting or executing inline JavaScript or JSON to discover ontology fields, types, relations, or runtime DOM content
- making supplemental script/data regions code-index owners, code symbols, imports, calls, or coderef targets
- defining general-purpose HTML editing or DOM restructuring
- changing Markdown structural-section and embedded-node behavior
- defining iframe rendering, active-content trust, or pane navigation

## User Stories

### US1 - Format-neutral root typing and fallback identity

- id:: ^SPEC-0086-US1
- summary:: Project Markdown and HTML through one root-node contract so valid root metadata creates the same typed meaning while absent or unusable metadata preserves an honest fallback root.
- status:: ready

#### Acceptance Criteria

- A configured HTML note with one valid canonical metadata object and a valid ontology `type` projects to the same public type, canonical root-field meanings, and `NodeRef` semantics as an equivalent Markdown note. ^SPEC-0086-US1-AC1
- An HTML note without a declared type projects to a stable internal fallback note root that remains readable, searchable, linkable, and navigable without appearing as a public ontology type. ^SPEC-0086-US1-AC2
- Duplicate or syntactically malformed canonical HTML metadata emits a provider diagnostic, projects the note as an untyped fallback, causes metadata-dependent mutations to fail when attempted, and does not suppress independently parseable visible-text, supplemental-region, or ordinary-link indexing; valid-but-unknown or type-invalid metadata follows normal cross-format ontology assessment semantics. ^SPEC-0086-US1-AC3
- A declared type that is unknown, selector-incompatible, or otherwise invalid follows the shared ontology classification and diagnostic contract rather than format-specific guessing. ^SPEC-0086-US1-AC4
- HTML headings, elements, IDs, and named anchors create no ontology section, embedded-node, or containment identities in v1. ^SPEC-0086-US1-AC5

### US2 - Cross-format ontology read and graph parity

- id:: ^SPEC-0086-US2
- summary:: Read and traverse typed HTML roots through the same ontology surfaces as equivalent Markdown roots while retaining format and source provenance.
- status:: ready

#### Acceptance Criteria

- Typed query, node read, field hydration, validation, and graph projection return equivalent canonical values and relations for semantically equivalent Markdown YAML and HTML JSON root metadata. ^SPEC-0086-US2-AC1
- CLI, agent/MCP, GraphQL, search, and web read payloads preserve canonical node identity and disclose the authored format and source locator rather than presenting derived text as raw HTML source. ^SPEC-0086-US2-AC2
- Link-valued root metadata resolves through the shared ontology link-field and candidate-resolution infrastructure; the HTML provider supplies authored values and source ranges but does not own relation semantics. ^SPEC-0086-US2-AC3
- Ordinary HTML links and fragments remain source/link-graph and navigation facts unless a schema-declared root field gives them ontology relation semantics. ^SPEC-0086-US2-AC4
- Fallback HTML roots participate in ambient note graph and backlink reads without masquerading as a public typed node. ^SPEC-0086-US2-AC5

### US3 - Root-owned visible and supplemental evidence

- id:: ^SPEC-0086-US3
- summary:: Make statically embedded HTML content discoverable from its canonical note root without turning script/data bodies into ontology structure or code identities.
- status:: ready

#### Acceptance Criteria

- Root chunk planning consumes all eligible provider search regions, including visible text and supplemental inline script/data content, for both typed and fallback HTML roots. ^SPEC-0086-US3-AC1
- Supplemental regions retain their kind, media type or language hint, source range, and note-root ownership through lexical and semantic evidence construction. ^SPEC-0086-US3-AC2
- Supplemental regions do not create ontology fields, relations, sections, embedded nodes, code owners, symbols, imports, calls, or coderef evidence. ^SPEC-0086-US3-AC3
- The canonical HTML metadata block is excluded from supplemental content because its values already enter ontology and search through canonical metadata projection. ^SPEC-0086-US3-AC4
- Search results and evidence navigation resolve to the owning HTML note and appropriate source provenance while distinguishing visible from supplemental derived content. ^SPEC-0086-US3-AC5

### US4 - Convergent diagnostics and lifecycle

- id:: ^SPEC-0086-US4
- summary:: Keep ontology state truthful when HTML metadata, format ownership, schemas, or source content change.
- status:: ready

#### Acceptance Criteria

- Full rebuild and incremental create, modify, rename, delete, ownership-change, and schema-change paths converge on the same typed-or-fallback root, fields, relations, diagnostics, and root chunks. ^SPEC-0086-US4-AC1
- Fixing malformed or duplicate metadata replaces the fallback projection with the valid typed or untyped projection without leaving stale diagnostics, fields, relations, or chunks. ^SPEC-0086-US4-AC2
- Introducing or removing configured note ownership atomically installs or removes ontology/fallback ownership and never leaves simultaneous HTML note and code identities for the same path. ^SPEC-0086-US4-AC3
- Fatal source-read failures retain only the note identity and diagnostic required by the shared ingestion contract and remove stale ontology fields, relations, and search evidence. ^SPEC-0086-US4-AC4

## Requirements

### Projection boundary

- Format providers MUST parse authored syntax and emit format-neutral root metadata values, source ranges, source diagnostics, links/fragments, and search regions.
- Ontology projection MUST own type resolution, canonical field interpretation, typed relations, fallback identity, source locators, validation issues, and projected root-node reads.
- Format providers MUST NOT interpret schema fields, manufacture ontology node IDs, or write ontology catalog rows.
- Ontology projection MUST NOT reparse YAML or HTML/JSON syntax when a current provider projection is available.
- All projected values MUST preserve authored-format and source-range provenance sufficient for diagnostics, navigation, and source-preserving maintenance.

### Canonical root metadata

- Markdown YAML frontmatter and HTML plain JSON root metadata MUST normalize into one canonical root-metadata value model before ontology interpretation.
- Canonical keys such as `type`, `title`, aliases, tags, and schema-declared fields MUST have the same meanings across formats.
- JSON scalars, arrays, and objects MUST be accepted only where the canonical field and ontology schema permit the corresponding value shape. Format syntax MUST NOT silently broaden field cardinality or coercion.
- HTML root type declaration MUST use the canonical `type` key. JSON-LD, microdata, RDFa, ordinary `<meta>` elements, element attributes, and runtime DOM state MUST NOT participate in v1 ontology projection.
- Root title selection MAY use the format provider's fallback title evidence, but an explicit valid canonical metadata `title` MUST retain the shared highest-precedence root-field meaning.
- Link-valued root metadata MUST use plain canonical string or string-array values and the shared link-field resolution contract; it MUST NOT require Markdown wikilink decoration.

### Typed and fallback roots

- A valid root declaration MUST pass through the shared schema selector, identifier, ambiguity, and declared-type diagnostic rules.
- A successfully resolved public type MUST produce the same canonical `NodeRef`, field, relation, validation, and query model regardless of authored format.
- A note that does not resolve to a public type MUST still receive the stable internal fallback root defined by `SPEC-0076`.
- Missing metadata and provider-invalid metadata MUST be distinguishable: absence yields an ordinary untyped fallback, while duplicate or syntactically malformed metadata yields that fallback plus a provider diagnostic.
- Syntactically valid metadata with an unknown, mismatched, or otherwise invalid `type` MUST follow the same ontology assessment and diagnostic behavior as equivalent Markdown root metadata; a format provider MUST NOT downgrade it to ordinary absence.
- Fallback roots MUST be excluded from public ontology type enumeration by default while remaining available to note read, search, navigation, graph, and diagnostics.
- Duplicate or malformed canonical HTML metadata MUST emit a metadata diagnostic, leave the root untyped, and cause metadata mutation to fail its source validation until the ambiguity or parse failure is corrected.
- A metadata diagnostic MUST NOT require per-capability projection status or prevent extraction or indexing of independently parseable visible text, supplemental regions, ordinary links, or fragments.

### HTML root-only model

- HTML v1 MUST project exactly one file-backed note root and zero DOM-derived structural or embedded ontology nodes.
- HTML headings MAY contribute breadcrumbs or chunk context through the provider/search contract, but MUST NOT become ontology sections.
- HTML `id` attributes and legacy named anchors MAY act as link targets and navigation locators, but MUST NOT become ontology node identifiers.
- Inline script and data bodies MUST NOT be executed or interpreted to produce ontology structure or metadata.
- A future structural HTML projection MUST require an explicit successor or revision to this root-only contract; provider extensibility alone MUST NOT activate it.

### Read, query, graph, and validation parity

- Root-node reads MUST expose canonical fields and relations plus authored format, source locator, and relevant diagnostics.
- Typed query behavior MUST depend on canonical ontology semantics, not on whether the source used YAML or JSON syntax.
- Graph projection MUST keep typed ontology relations distinct from ordinary source-link/backlink evidence and avoid duplicate equivalent edges.
- CLI, MCP/agent, GraphQL, search, validation, and web consumers MUST use the same canonical root identity and projected field truth.
- Raw-source reads MUST continue to return authored HTML, while derived read/search payloads MUST label their representation so callers can distinguish raw source from extracted text or canonical fields.
- Validation MUST report source ranges in the authored syntax and MUST route format-local parse errors separately from ontology semantic errors.

### Root search evidence

- Typed and fallback HTML root chunk planning MUST consume eligible visible and supplemental search regions from the canonical source projection.
- Root chunks MUST preserve region kind and source provenance while remaining owned by the canonical root node.
- Supplemental inline script/data content MUST be treated as note content, not code indexing, and MUST NOT create a second search owner for the HTML path.
- The canonical metadata JSON body MUST NOT be duplicated as a supplemental script/data region.
- Semantic and lexical indexing MUST weight supplemental content below visible prose before each retrieval lane truncates candidates; both modes MUST retain bounded coverage, evidence-kind disclosure, and deterministic truncation diagnostics.
- Provider/search-region version changes that alter root evidence MUST participate in freshness invalidation under `SPEC-0076` and the format-registry contract.

### Validation strategy

- Cross-format fixtures MUST pair semantically equivalent Markdown and HTML notes and assert matching public types, canonical fields, relations, typed-query results, and validation outcomes.
- HTML fixtures MUST cover typed roots, untyped fallback roots, absent metadata, duplicate metadata, malformed JSON, unknown/mismatched types, scalar/list/object values, link fields, ordinary links, fragments, visible regions, and supplemental regions.
- Negative tests MUST prove that headings, IDs, named anchors, microdata, RDFa, JSON-LD, and inline scripts do not create ontology sections, embedded nodes, fields, or code identities.
- Lifecycle tests MUST prove full/incremental convergence across source edits, schema changes, ownership changes, rename/delete, error recovery, and format/parser-version invalidation.
- Surface tests MUST prove consistent root identity and format disclosure across typed query, graph, validation, CLI, agent/MCP, GraphQL, search, and web reads.

## Related Specs

- [[multi-format-html-notes|SPEC-0083]] defines the product behavior and cross-surface expectations for multi-format notes.
- [[note-format-provider-registry|SPEC-0084]] defines the note-format registry, ownership/classification, capabilities, paths, and freshness contract consumed here.
- [[html-note-format-provider|SPEC-0085]] defines the HTML provider's canonical JSON root metadata, extraction, links/fragments, and visible/supplemental regions.
- [[format-aware-note-maintenance-mutations|SPEC-0087]] defines source-preserving metadata and link maintenance over the ranges projected here.
- [[trusted-html-note-viewer|SPEC-0088]] defines active HTML rendering and pane navigation without changing ontology projection.
- [[note-node-indexing-architecture|SPEC-0076]] defines canonical authored-source ownership, typed and fallback root identity, and source-owned search evidence.
- [[structural-node-model-and-ontology-read-path|SPEC-0013]] remains authoritative for Markdown structural and embedded-node behavior; HTML v1 deliberately does not project those shapes.
- [[ontology-indexed-read-model-contract|SPEC-0040]] defines persisted ontology read-model ownership.
- [[primary-semantic-chunks-and-noderef-search|SPEC-0032]] defines source-owned primary chunks and `NodeRef` propagation.

## Documentation Plan

- Update ontology authoring guidance to describe format-neutral root metadata, Markdown YAML syntax, HTML JSON syntax, canonical key meanings, and HTML's root-only v1 boundary.
- Update query, graph, validation, CLI, MCP/agent, and web API documentation to describe format disclosure and raw-versus-derived representations.
- Add paired Markdown/HTML examples for typed roots, link fields, fallback roots, and metadata diagnostics that fail affected mutations locally.
- Document that inline script/data regions are searchable note content but never ontology or code structure.

## Open Questions

None.
