---
type: TechnicalSpec
summary: "Defines Rhizome's format-neutral note/node indexing architecture: authored note sources and provider projections in notemeta, typed and fallback node identity in ontology, source-owned search evidence in semantic indexes, and anchors narrowed to code-anchor declarations and code-doc bridging."
id: SPEC-0076
spec-status: active
last-updated: 2026-08-03
aliases:
  - Note/node indexing architecture
  - note-node-indexing-architecture
  - SPEC-0076
---
# Note/node indexing architecture

## Summary

Rhizome note processing is moving from Markdown-specific indexing toward format-neutral, ontology-centered operation. The target architecture separates four responsibilities that have historically overlapped: authored note source bytes, format-provider projections, ontology/fallback node identity, and search evidence. Each configured note's original file remains its mutation authority; derived projections never replace that source. The canonical semantic unit for typed note behavior is an ontology node or an internal fallback node, not an independently parsed legacy note DTO.

`pkg/notemeta` owns raw note file facts and format-stamped provider projections, including root metadata, authored links, fragment targets, and searchable regions when the provider supports them. `pkg/ontology` and `pkg/ontology/noderead` own canonical typed and fallback node identity, fields, relations, source locators, and read hydration. Search candidates, primary chunks, and embeddings are derived evidence keyed from canonical owners and embedded-text hashes. `pkg/anchors` remains valuable for Markdown code-anchor declarations and code-to-doc bridging, but it must not act as a parallel general note ingestion layer.

This spec complements `SPEC-0012` (`docs/specs/technical/indexing-pipeline-architecture.md`), `SPEC-0013` (`docs/specs/technical/structural-node-model-and-ontology-read-path.md`), `SPEC-0032` (`docs/specs/technical/primary-semantic-chunks-and-noderef-search.md`), and `SPEC-0040` (`docs/specs/technical/ontology-indexed-read-model-contract.md`). Those specs continue to govern writer-lane mechanics, node identity/read paths, source-owned primary retrieval, and ontology catalog ownership.

## Goals

- Make `pkg/notemeta` the single canonical owner of raw note source facts and persisted provider projections: format-neutral vault-relative path, format, content hash, mtime, title, root metadata, authored links, fragment targets, search regions, capabilities, and projection freshness.
- Introduce a registered format-provider projection boundary so root metadata, links, fragment targets, search regions, source ranges, and available capabilities are derived without making syntax-specific parsers cross-domain authorities.
- Make ontology nodes and internal fallback nodes the canonical semantic identity for note fields, relations, primary body chunks, source locators, navigation, validation, and source-preserving edit spans.
- Keep untyped notes first-class for internal read/search coverage through fallback nodes while excluding fallback types from public ontology type lists by default.
- Narrow `pkg/anchors` to code-anchor declarations, code-intel attachment, file context, and code-to-doc bridge persistence that consume canonical note facts.
- Treat primary chunks, embeddings, search candidates, and answer-packet evidence as derived data, never as identity authority.
- Preserve the existing one queued SQLite writer lane, explicit correctness barriers, `semanticruntime` lanes, and scoped sync behavior while changing domain ownership boundaries.
- Make row ownership and store-port ownership explicit even if the physical SQLite implementation remains unified.
- Ensure configured note ownership is exclusive: a path is represented as a note or as code, never both, and ownership transitions atomically retire stale derived rows from the former representation.

## Non-Goals

- Rename `pkg/anchors/sqlite` or move SQLite files before domain store ports exist.
- Remove untyped-note coverage from search, graph, semantic, or read surfaces.
- Re-render markdown from ontology projections or replace source-preserving edits with generated note writes.
- Change search ranking, answer-packet UI, or graph visualization behavior as part of this architecture contract alone.
- Introduce multiple hot SQLite writer lanes for note, ontology, anchor, or semantic indexing.
- Promise immutable chunk IDs across a deliberate migration from legacy doc-section chunks to node-backed chunks.
- Provide runtime third-party format plugins, automatic HTML trust discovery, or structural ontology projection for formats whose provider declares only root-note capability.

## Current Architecture Map

The current system already contains the pieces this spec wants, but the ownership boundaries are still tangled.

### Raw note source path

Current flow:

```text
obsidian.NoteReader
  -> raw note content/list/modtime/title
  -> notemeta.EnsureIndexed / notemeta.SyncPaths
  -> note metadata snapshot/delta
  -> note rows, raw properties, tags, aliases, raw links, Markdown targets, search terms
```

This is the correct home for format-neutral note-file facts. It should own path normalization, format identity, mtime/hash/title, and the current provider projection. Providers own syntax extraction for root metadata, aliases, raw links, fragment targets, and search regions; `notemeta` owns their persisted source-fact representation and freshness. Snapshot and delta publication must converge the same provider-target rows; note fragment targets are distinct from code anchors.

### Note and anchor projection path

Current flow:

```text
notemeta.NoteSourceSnapshot + NoteProjection
  -> format-specific adapters
  -> anchor declarations (when supported) + note-owned search/link facts
  -> domain-specific index work
  -> anchor rows, search rows, raw links, fragment targets, note freshness, file context
```

This path projects canonical raw-note facts into provider-declared capabilities. The Markdown adapter may produce code-anchor declarations and code-to-doc bridge data; an HTML provider does not. Raw path/content compatibility entrypoints remain removed, and no adapter constructs a competing raw-note DTO or code identity.

### Ontology projection path

Current flow:

```text
raw/indexed notes + schema
  -> ontology.ProjectNode / ProjectNote / SyncPaths
  -> node projections
  -> ontology catalog rows, field values, relations, source locators, semantic primary chunks
```

This is the correct home for typed meaning: node identity, typed fields, relations, validation, navigation, source locators, source spans, and source-preserving edit metadata.

### Search and evidence path

Current flow:

```text
retrievers
  -> search.Candidate
  -> merge/rank/pack
  -> semantic/vector/lexical/ontology evidence
  -> answer layer
```

Search should merge evidence from stable handles, not decide which parser owns note identity. The retrieval layer owns candidate/evidence semantics and maps canonical note/node/code owners into search handles.

### Embedding path

Two note embedding surfaces currently coexist:

- legacy/raw `doc_section` chunks from Markdown note chunking
- ontology-node primary body chunks from ontology projection

The intended direction is typed-note-first: ontology body chunks are the normal note semantic surface when ontology is available; raw doc-section chunks remain fallback or migration compatibility coverage. A provider may also emit note-owned search regions that feed the root/fallback owner's lexical and semantic body without becoming structural sections.

## Identity Inventory

| Identity concept | Recommended authority | Implementation notes |
| --- | --- | --- |
| Vault-relative note path | `pkg/paths` plus `pkg/notemeta` rows | Format-neutral file identity. It must not imply `.md`; do not use it as node identity except for note-root fallback owners. |
| Note title | `notemeta` for file title; ontology catalog for node display label | Avoid recomputing title differently in anchors, search, and ontology. |
| Content hash and mtime | `notemeta` for source freshness; embedding state for chunk freshness | `anchors.NoteIndexMeta` should become adapter/cache metadata, not canonical note freshness. Content hash is note state identity and the only source input to `note_metadata_state`; mtime and size are per-row freshness evidence that trigger a read but never, on their own, a re-materialization. |
| Raw root metadata and inline properties | Providers extract syntax-specific facts; `notemeta` stores them; ontology interprets typed fields | Markdown uses YAML frontmatter/inline properties. Other formats may expose root metadata only. Anchors may inspect Markdown code-anchor declarations but do not own general property parsing. |
| Tags and aliases | `notemeta` | Ontology may consume these as raw evidence, but raw storage belongs to note metadata. |
| Authored links | Provider projection plus `notemeta` for raw note graph; ontology for typed/structural edges | Providers preserve authored targets and source spans. Shared core owns resolution and candidate matching. Typed ontology edges win over duplicate fallback link edges. |
| Fragment targets | Provider projection plus `notemeta` | Markdown headings/block IDs and HTML `id`/legacy named anchors are navigation targets, not automatically ontology nodes or code anchors. |
| Search regions | Provider projection plus semantic/search stores | Visible and supplemental regions retain kind, media type, source range, and format provenance; they remain note-owned evidence. |
| Code-anchor note declarations | `anchors` adapter over canonical note facts | Keep extraction and persistence here, but shrink the input to code-anchor declarations. |
| Doc sections | ontology document snapshot/section model for source spans; doc-section rows as compatibility output | Duplicate section parsing between legacy chunks and ontology sections is a key cleanup target. |
| Ontology node ID | `ontology` only | Canonical owner ID for typed node catalog rows, semantic chunks, and read hydration. |
| Source locator | `ontology` catalog and `noderead` | Author-facing lookup/display identity, not the primary cache key when node ID or `NodeRef` JSON exists. |
| Search handle | search/knowledge adapters over canonical note/node/code owners | Keep handle stability, but map old note/doc-section handles toward node-backed owners. |
| Embedding chunk hash | owner-specific embedding state | Hash the actual embedded text. Do not substitute source fingerprints for embedding cache keys. |

## Target Contracts

### NoteSourceSnapshot And Provider Projection

Ontology projection, provider extraction, anchor extraction, and fallback chunking consume a canonical authored-source snapshot rather than inventing local note DTOs. Syntax-derived facts are a separately versioned projection so raw source identity and parser output have distinct freshness.

```go
type NoteSourceSnapshot struct {
    Path        NotePath // normalized, vault-relative, format-neutral
    Format      NoteFormatID
    Content     []byte
    ContentHash string
    Mtime       int64
    Size        int64
    Title       string
}

type NoteProjection struct {
    FormatVersion  string
    RootMetadata   map[string]any
    Links          []AuthoredLink
    FragmentTargets []FragmentTarget
    SearchRegions  []SearchRegion
    Capabilities   NoteCapabilities
}

type SearchRegion struct {
    Kind        SearchRegionKind // visible or supplemental
    MediaType   string
    Text        string
    SourceRange SourceRange
}
```

The exact package and field names may change during implementation, but the contract shape is important: authored bytes and file freshness remain source facts; provider output is deterministic, versioned, source-ranged derived data; typed meaning is not embedded in either raw contract. Callers that need the authored representation receive raw source. Search, semantic, graph, ontology, and file-context consumers receive the relevant projection and disclose its representation.

### Search Candidate Mapping

Search adapters should map ontology/fallback catalog rows into candidates at retrieval boundaries. Do not introduce a generic identity DTO until a concrete factory earns its keep.

`NodeID` is the stable owner key. `SourceLocator` is the author-facing lookup/display key. `NodeRefJSON` is the structured bridge for browser, MCP, search, graph, edit-session, and semantic consumers.

### Catalog and embedding deltas

The current ontology semantic sync avoids writing catalog rows because catalog sync owns `ontology_nodes` and field rows. Preserve that invariant by splitting the products conceptually:

```text
ProjectedNodeView
  -> NodeCatalogDelta      written only by ontology sync/catalog paths
  -> NodeEmbeddingDelta    written only by semantic primary-chunk sync paths
```

Semantic sync may read projected node views to build chunks. It must not call destructive catalog replacement APIs as a side effect of embedding work.

### Anchor declaration extraction

Anchor extraction should become an adapter over canonical note facts:

`pkg/anchors` accepts a structural `NoteSource` interface to avoid an import cycle while `notemeta` still has private SQLite adapter dependencies. Orchestration MUST pass canonical `notemeta.NoteSourceSnapshot` facts; competing raw path/content entrypoints are not retained.

```go
type AnchorNoteStore interface {
    UpsertNoteWithCleanup(ctx context.Context, note anchors.Note, keepLabels []string) (anchors.AnchorUpsertResult, error)
}
```

The current implementation still persists through the existing anchor-note adapter row shape, so the port is named `AnchorNoteStore` rather than pretending per-declaration persistence exists yet. The important boundary is that anchor extraction consumes note-source facts and returns code-anchor declarations/code-doc bridge facts, not a second general note metadata index.

## Structural Segmentation And Search Regions

Legacy raw note chunking, anchor intel sections, and ontology sections currently duplicate Markdown heading/body logic. The target is one source-aware projection model in which structure is a provider capability, not a universal note invariant:

- every configured note has a typed or fallback root owner
- typed embedded nodes and section owners exist only when its format provider emits structural projections
- untyped Markdown notes may project to internal fallback note-root and section owners; root-only providers produce only the fallback root
- legacy `doc_section` rows, while they exist, are compatibility output from the same Markdown structural snapshot
- section-own content excludes child section ranges in the same way ontology source utilities do
- Markdown single-H1 wrapper behavior is decided once by its shared snapshot builder
- fallback note-root chunks carry root/single-H1 wrapper prose; authored child sections are separate `_FallbackSection` owners
- non-structural providers feed ordered visible and supplemental search regions into the root owner's lexical and semantic body without disguising regions as sections or code owners

`ChunkNote` and any anchor-owned doc-section extraction should stop independently defining heading breadcrumbs, source spans, and section body semantics. The Markdown implementation now routes ontology sections and anchor intel doc-section rows through the shared `pkg/ontology/sectionparse` parser; other providers must not call it unless they explicitly adopt the same structure contract. Search-region ordering, weighting, truncation, and semantic inclusion must remain deterministic. If projected text changes enough to cause a one-time re-embed, the provider/projection version migration must be explicit and visible in validation/performance notes.

## Package And Store Boundaries

The physical SQLite database can remain unified. The public store dependencies should move toward domain ports so callers stop treating `pkg/anchors/sqlite` as the conceptual owner of every persisted thing.

Target port families:

- `notemeta.Store`
- `ontology.CatalogStore`
- `ontology.NodeReadStore`
- `semantic.NodeEmbeddingStore`
- `semantic.SearchStore`
- `anchors.AnchorNoteStore`

Implementation order matters. Define ports and move call sites first. Rename or wrap the SQLite package only after behavior has stabilized and the dependency inversion has real value. Transitional ports must not overclaim future row shapes: for example, content-only note snapshots are exposed as `NewContentOnlyNoteSourceSnapshot`, while full raw-note facts are produced by the `notemeta` indexing path.

## Search Handle Migration

Preferred owner hierarchy:

```text
NotePath
  -> OntologyNodeID / FallbackNodeID
       -> body chunk ids
       -> primary chunk ids
       -> generated evidence facts
```

Compatibility mapping:

- `NoteHandle(path)` resolves to the fallback/root node owner when no typed node target is available.
- `NoteChunkHandle(path, idx)` resolves to a fallback/doc-section node chunk when possible.
- `NodeChunkHandle(nodeID, path, granularity, idx)` is canonical for new typed and fallback note evidence.
- Node-chunk handles point directly at canonical node/source evidence and preserve NodeRef metadata.

Search result merging should remain handle-based while migration is active, but adapters must preserve `NodeID`, `NodeRefJSON`, `SourceLocator`, node kind/type, parent ID, and note path whenever that metadata exists. Ontology-node semantic chunks are valid only when catalog identity is complete; retrieval adapters must skip stale ontology-node chunks rather than downgrading them to `NoteChunkHandle`.

## User Stories

### US1 - Preserve a durable ownership contract that separates raw note facts, canonical node identity, anchor declarations, and derived search evidence before refactoring note-processing code
- id:: ^SPEC-0076-US1
- summary:: Preserve a durable ownership contract that separates raw note facts, canonical node identity, anchor declarations, and derived search evidence before refactoring note-processing code.
- status:: ready

#### Acceptance Criteria

- Raw note facts have one canonical owner. ^SPEC-0076-US1-AC1
  verification:: Inspect note-ingestion code and persistence ports. General note path/title/hash/mtime/properties/tags/aliases/raw links/search terms are owned by `pkg/notemeta`, with any non-notemeta writes documented as transitional adapter metadata.
- Node identity and node semantics have one canonical owner. ^SPEC-0076-US1-AC2
  verification:: Inspect ontology projection, catalog sync, and `noderead` consumers. `OntologyNodeID`, `NodeRef`, source locators, typed field values, typed edges, fallback nodes, and node-scoped hydration are produced by ontology-owned paths and read through ontology/noderead contracts.
- Anchors no longer produce a parallel general note model. ^SPEC-0076-US1-AC3
  verification:: Inspect `pkg/anchors` note-facing APIs. Code-anchor declaration extraction consumes canonical note source snapshots or note fact rows and persists only anchor/file-context/code-doc bridge facts, not independent general note metadata.
- Search and embeddings use canonical owners. ^SPEC-0076-US1-AC4
  verification:: Inspect search handle adapters and semantic writeback. Typed note evidence uses ontology-node owners, untyped evidence uses fallback-node owners, every primary chunk identifies its source owner, and raw `doc_section` handles are compatibility aliases or fallback evidence only.

### US2 - Keep untyped notes searchable and navigable through internal fallback nodes without making fallback types public ontology semantics
- id:: ^SPEC-0076-US2
- summary:: Keep untyped notes searchable and navigable through internal fallback nodes without making fallback types public ontology semantics.
- status:: ready

#### Acceptance Criteria

- Untyped notes project to internal fallback node surfaces. ^SPEC-0076-US2-AC1
  verification:: Index an untyped note fixture and inspect read/search rows. The note has a stable note-root fallback owner and section/body chunk evidence even when no public ontology type matches it.
- Fallback nodes stay hidden from public type semantics by default. ^SPEC-0076-US2-AC2
  verification:: Query public ontology type lists and typed-root discovery. Internal fallback types do not appear unless a diagnostic/debug API explicitly asks for internal coverage.
- Fallback source locators resolve. ^SPEC-0076-US2-AC3
  verification:: Resolve fallback note-root and provider-supported child locators through node-link/search/navigation helpers. The result opens the original authored source without broadening to unrelated notes.
- Primary chunks do not invent typed meaning for fallback nodes. ^SPEC-0076-US2-AC4
  verification:: Inspect primary chunk planning for untyped notes. Fallback nodes retain internal fallback identity and authored body without masquerading as public ontology types.

### US3 - Introduce domain store ports and row-ownership rules while preserving the unified SQLite implementation and indexing performance mechanics
- id:: ^SPEC-0076-US3
- summary:: Introduce domain store ports and row-ownership rules while preserving the unified SQLite implementation and indexing performance mechanics.
- status:: ready

#### Acceptance Criteria

- Domain ports exist before package renames. ^SPEC-0076-US3-AC1
  verification:: Review dependency direction. Callers depend on narrow `notemeta`, `ontology`, `semantic`, and `anchors` store ports before any broad package rename from `pkg/anchors/sqlite`.
- Row ownership is enforced by interfaces. ^SPEC-0076-US3-AC2
  verification:: Review store interfaces and tests. `notemeta` owns note rows/raw properties/tags/search terms/raw note graph edges; ontology sync owns `ontology_nodes`, `ontology_node_field_values`, and `ontology_edges`; semantic sync owns chunks and embeddings; anchors owns code-anchor declarations and file-context bridge rows.
- Writer-lane and barrier behavior stays intact. ^SPEC-0076-US3-AC3
  verification:: Run indexing tests or timing diagnostics for changed paths. High-volume writes still use the queued writer lane, ontology projection still waits behind durable ingest barriers, and semantic provider work uses shared runtime lanes.
- Semantic sync cannot prune ontology catalog rows. ^SPEC-0076-US3-AC4
  verification:: Tests cover ontology catalog rows and field values surviving primary semantic sync. Semantic writeback may update chunks/embeddings but cannot destructively replace catalog-owned rows.

### US4 - Ingest configured notes through one format-neutral authored-source and derived-projection contract
- id:: ^SPEC-0076-US4
- summary:: Ingest configured notes through one format-neutral authored-source and derived-projection contract without inventing a competing code identity or syntax-specific downstream pipeline.
- status:: ready

#### Acceptance Criteria

- Authored source and derived projection have distinct, format-stamped contracts. ^SPEC-0076-US4-AC1
  verification:: Index equivalent Markdown and HTML fixtures. Persisted source identity records the configured format and source hash, while metadata, links, fragment targets, capabilities, and search regions carry the provider/projection version that produced them.
- Search regions remain note-owned evidence across lexical and semantic indexing. ^SPEC-0076-US4-AC2
  verification:: Index visible and supplemental HTML content. Both are retrievable under the note/root owner with format and source-range provenance; neither creates code symbols, imports, call edges, structural sections, or an independent code result handle.
- Full, incremental, rename, delete, and ownership-transition paths converge. ^SPEC-0076-US4-AC3
  verification:: Exercise create, modify, rename, delete, provider-version change, note-include gain, and note-include loss. Batch and live paths converge on the same current rows and atomically retire stale note or code representations.
- Consumers receive the representation appropriate to their contract. ^SPEC-0076-US4-AC4
  verification:: Direct source reads return authored bytes, while search, semantic, ontology, graph, validation, and file-context surfaces consume named projections and disclose source format/representation without reparsing through Markdown-only helpers.

## Requirements

### Raw Note Fact Contract

- `pkg/notemeta` MUST own raw note file freshness: format-neutral normalized vault-relative path, format identity, content hash, mtime, size, and file title.
- The persisted note state hash MUST be derived from content identity (path plus content hash) and the selected provider fingerprint only. Mtime and size MUST NOT participate in it. They MUST still be persisted per row and MUST still make a path dirty so its bytes are re-read; a re-materialization decision MUST be made from the re-read content and provenance, never from timestamp equality alone.
- Registered providers MUST extract syntax-specific root metadata, aliases, authored links, fragment targets, searchable regions, and exact source ranges; `pkg/notemeta` MUST own their persisted source-fact representation and freshness.
- Raw note facts MUST remain source facts. They MUST NOT encode typed ontology meaning beyond durable source evidence that ontology projection can consume.
- `pkg/vault/obsidian` SHOULD stay Markdown-syntax focused. It MUST NOT remain the universal note discovery, path, or projection abstraction.
- Ontology projection SHOULD consume a canonical source snapshot rather than re-reading and re-parsing files when the pipeline already has current note facts.
- Anchor extraction and fallback chunking SHOULD consume the same source snapshot when they need note content.
- Raw note graph edges SHOULD remain available as fallback or ambient evidence, but typed ontology relations MUST be modeled in ontology-owned rows.
- Provider and projection versions MUST participate in freshness so extraction changes deterministically invalidate affected derived rows.
- Note ownership MUST be exclusive. A configured note path MUST NOT simultaneously retain code-file, coderef, code-symbol, or code-semantic ownership.

### Ontology Node Contract

- `pkg/ontology` MUST own `NodeRef`, `OntologyNodeID`, source locators, node catalog rows, typed field rows, typed structural/ambient edges, validation/assessment rows, and source ranges/fingerprints.
- `pkg/ontology/noderead` MUST remain the read boundary for batched hydration, type lists, locators, graph facts, traversal, and node-owner reads.
- File-backed note roots, embedded nodes, section-like nodes, and internal fallback nodes MUST resolve through the same canonical node identity model.
- `NodeSourceLocator` and other author-facing locators MUST remain lookup/display identities, not primary cache keys when canonical node IDs or `NodeRef` JSON are available.
- Each note's authored file MUST remain the source of truth. Edit flows MUST use provider-declared mutation capabilities to patch targeted spans or source ranges rather than re-rendering whole notes from catalog rows or search projections.
- Ontology catalog sync MUST be the only writer for `ontology_nodes`, `ontology_node_field_values`, ontology link-field dependencies, and catalog-owned source locators.
- Ontology projection MUST support an internal fallback note-root for every note that does not match a public ontology type; fallback sections exist only when the provider supplies structural sections.
- Public ontology type lists MUST exclude fallback-only internal types by default.

### Anchor Boundary

- `pkg/anchors` MUST keep code-anchor extraction, anchor matching, file context, code-intel attachment, and code-to-doc bridge behavior.
- `pkg/anchors` MUST NOT own general note title/hash/mtime/property/tag/link freshness once the canonical note fact contract is available.
- Anchor declaration extraction MUST accept canonical `NoteSourceSnapshot` facts from orchestration or a `notemeta` reader instead of defining a competing general-purpose `Note`; legacy path/content wrappers are removed.
- Transitional anchor metadata MAY exist only when it is adapter-local, cache-local, or needed to preserve code-doc bridge convergence during migration.
- Anchor-owned doc-section rows SHOULD become compatibility output from the shared ontology/fallback document snapshot, not independently parsed section truth.
- File-context behavior MUST keep existing code-to-doc workflows working while the note ingestion boundary narrows.

### Search Evidence Contract

- Search MUST treat ontology/fallback node identity as the canonical note evidence owner when ontology projection is available.
- Typed note body chunks MUST use `owner_type=ontology_node` or the future equivalent canonical node owner family.
- Primary ontology chunks MUST remain derived source-owned evidence and MUST point back to their owning node/source. They MUST NOT become identity authority.
- Raw `doc_section` chunks MAY remain as compatibility/fallback evidence during migration, but they MUST NOT become the preferred evidence surface for typed notes.
- Embedding cache freshness MUST continue to hash the actual embedded text, not unrelated source or projection fingerprints.
- Legacy `NoteHandle` and `NoteChunkHandle` MAY resolve as aliases during migration, but new note evidence should prefer node-backed handles once callers can accept them.
- Search adapters MUST preserve `NodeID`, `NodeRefJSON`, node kind/type, parent ID, source locator, and note path when converting ontology/fallback chunks into candidates.
- Primary chunks MUST preserve source provenance and navigation context, not become separate final answer items or source-of-truth records.
- Provider search regions MUST preserve format, representation kind, media type, and source ranges through lexical and semantic evidence.
- Supplemental regions MAY be searchable as note content but MUST receive their lower weight or budget before lexical and semantic lanes truncate candidates; they MUST NOT create code-intelligence identity or structural ontology meaning.
- Root ontology/fallback body construction MUST include eligible provider search regions so ontology-ready indexing does not silently drop supplemental semantic coverage.

### Row Ownership

- `notes`, raw property/tag/search-term rows, aliases, and raw note-link graph evidence are owned by `notemeta`.
- Code-anchor declarations, code-to-doc bridge rows, and file-context associations are owned by `anchors`.
- `ontology_nodes`, `ontology_node_field_values`, ontology link-field dependencies, and `ontology_edges` are owned by ontology sync/catalog paths.
- `intel_chunks` and `intel_embeddings` for ontology primary chunks, fallback chunks, and compatibility doc sections are owned by semantic sync paths.
- Cross-domain readers MAY read the unified SQLite database through domain ports, but writers MUST not replace another domain's rows as an incidental side effect.
- Semantic primary-chunk sync MUST NOT destructively replace ontology catalog rows or typed field rows.
- Deleted note paths and note/code ownership transitions MUST clean raw note metadata, provider projections, ontology/fallback nodes, chunks, embeddings, anchor declarations, code/coderef rows, file context associations, and compatibility doc-section rows through the correct domain owners.

### Performance And Ordering

- Refactors MUST preserve the indexing pipeline's single queued SQLite writer lane.
- Ontology projection MUST continue to read after explicit durable ingest barriers when it depends on note/code-intel rows.
- Semantic primary-chunk/provider work SHOULD continue to overlap where correctness permits through `semanticruntime` lanes.
- Refactors MUST NOT introduce N-per-file SQLite reads on hot paths when batch reads or scoped snapshots are available.
- Scoped sync for changed/deleted paths MUST remain the default; schema or manifest changes MAY still force broader sync where the governing specs require it.
- Full and live indexing MUST apply the same provider selection, projection version, and ownership-transition rules.

### Migration Order

1. Freeze this ownership contract, test fixtures, and row-ownership matrix before moving code.
2. Introduce narrow domain store ports while leaving the current SQLite implementation physically unified.
3. Generalize note path identity and introduce the compile-time provider registry before adding a second note format.
4. Make `notemeta` the canonical raw note metadata/projection source and update anchor note ingestion to consume canonical snapshots.
5. Consolidate Markdown body/section segmentation around ontology document snapshots while allowing root-only providers to supply search regions without structural sections.
6. Migrate search handles toward node-backed note evidence while keeping old note handles resolvable during rollout.
7. Only after behavior stabilizes, rename or wrap `pkg/anchors/sqlite` behind a neutral store package if it still improves dependency clarity.

### Validation Strategy

- Add fixture coverage for plain untyped Markdown notes with headings and links, typed Markdown note roots with scalar/link/enum fields, typed notes with section children, untyped notes containing embedded `@source` nodes, aliases resolving wikilinks on first pass, root-only HTML notes with visible/supplemental regions and links/fragments, changed/deleted paths, ownership transitions, schema/provider-version changes, and Markdown code-anchor notes.
- Assert identity invariants: normalized path stability, node ID stability across no-op reindex, source locator resolution, structural fingerprint changes on structure changes, content hash changes on content changes, and search handle resolvability.
- Assert fallback invariants: untyped notes remain searchable, fallback source locators resolve, fallback types stay hidden from public type lists, and fallback primary chunks do not masquerade as public typed nodes.
- Assert persistence invariants: notemeta owns raw note rows, ontology sync owns catalog/field/edge rows, semantic sync does not delete catalog rows, and deleted note paths clean all derived rows.
- Assert migration invariants: old note/doc-section handles resolve or degrade gracefully while new node-backed handles become the preferred result shape.
- Track performance before and after migration: full index wall time, SQLite write transaction count, `intel_chunks` churn for unchanged notes, provider calls and chunks embedded versus reused, final drain time, and mixed typed/untyped search latency p50/p95.

## Related Multi-Format Contracts

- [[../product/multi-format-html-notes|SPEC-0083 Multi-format HTML notes]] owns the user-visible scope and trust boundary.
- [[note-format-provider-registry|SPEC-0084 Note format provider registry]] owns provider registration, format-neutral path identity, capabilities, classification, and invalidation.
- [[html-note-format-provider|SPEC-0085 HTML note format provider]] owns HTML metadata, extraction, links, fragments, and supplemental search regions.
- [[format-aware-root-ontology-projection|SPEC-0086 Format-aware root ontology projection]] owns root-only typed/fallback projection semantics.
- [[format-aware-note-maintenance-mutations|SPEC-0087 Format-aware note maintenance mutations]] owns granular source-preserving metadata/link/move mutation contracts.
- [[../experience/trusted-html-note-viewer|SPEC-0088 Trusted HTML note viewer]] owns active HTML rendering and pane navigation behavior.

## Failure Modes

- Identity churn can break saved handles, web navigation, graph focus, or MCP search consumers.
- Chunk text changes can cause a large one-time re-embed. That is acceptable only when intentional and documented.
- Over-rotating to typed ontology can make untyped notes disappear unless fallback nodes are treated as first-class internal evidence.
- Narrowing `anchors` too quickly can break code-to-doc workflows because anchor/file-context persistence currently depends on note ingestion.
- Moving ownership without preserving flush barriers can make ontology projection read stale note or code rows.
- Renaming `pkg/anchors/sqlite` before domain ports exist creates churn without reducing coupling.
- Letting semantic sync write catalog rows can reintroduce destructive replacement bugs where embedding writeback deletes ontology rows installed by catalog sync.
- Treating derived HTML text as authored source can corrupt maintenance edits or return misleading direct-read content.
- Allowing a configured HTML note to retain its old code/coderef rows creates duplicate graph/search identities and inconsistent lifecycle cleanup.
- Omitting supplemental regions from ontology-root chunk construction makes typed HTML lexically searchable but semantically incomplete.

## Open Questions

- What is the exact retirement policy for legacy `NoteChunkHandle` and `doc_section` evidence once node-backed handles are universal?
- Should fallback-node exposure be configurable for diagnostics, or should internal fallback coverage remain visible only through dedicated debug endpoints?
- Which existing anchor/file-context tests best prove code-to-doc workflows survive after anchor note ingestion narrows?
- How much chunk text churn is acceptable when consolidating legacy Markdown chunking onto the ontology document snapshot?

None of these existing migration questions blocks the format-neutral authored-source and provider-projection foundation.

## Documentation Plan

- Update the vault-core, notemeta, indexing, ontology, search, validation, and web subsystem notes when their Markdown-only invariants move.
- Document the authored-source versus derived-representation distinction on CLI, MCP, API, and file-context read surfaces.
- Keep provider capabilities, projection-version invalidation, ownership transitions, and search-region provenance visible in diagnostics and maintainer guidance.
- Preserve Markdown-specific syntax rules in the Markdown provider/subsystem documentation rather than presenting them as universal note rules.
