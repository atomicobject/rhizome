## pkg/noteformat

Closed descriptor registry and optional projection runtime for authored note
files.

- Providers declare identity, extensions, independent provider/projection versions, and capabilities; they never read files or write persistence.
- `AuthoredSource` owns a sealed canonical byte snapshot: path, format, hash, size, mtime, and bytes are construction-only facts exposed through read-only accessors. `Runtime` rechecks canonicality and freshness before a projector call.
- Optional `Projector` implementations compose the descriptor-only `Provider` contract and receive sources only through `Runtime`. `Project` may run concurrently across authored sources, so implementations must synchronize mutable state. The runtime verifies the exact registered descriptor, exposes availability through `CanProject`, and never exposes a raw projector escape hatch.
- `Runtime.ProjectCached` is `Project` for read paths that re-project unchanged files on every request (noderead provider snapshots). It keeps one validated result per format, path, content hash and mtime in a bounded memo that lives as long as the runtime and empties when full, and returns a detached `Copy` on every call, so no caller shares or mutates a cached result. It assumes a projector is a pure function of the authored source. Indexing and anything that must observe a projector rerun call `Project`, which never reads or fills the memo.
- `Projection` owns the versioned stale/current/fatal result envelope plus copied unresolved authored facts. Root metadata values use the closed, canonical `MetadataValue` domain rather than arbitrary Go values. Root field names are nonempty; nested objects preserve every string key, including the empty string. Integer and float durable semantics stay distinct, date-only values preserve their calendar date, and timestamps preserve their authored offset and fractional precision. Diagnostics carry a closed category, affected operation, optional original-byte range, and blocking flag. Only whole-projection fatal results carry blocking diagnostics.
- Links declare one closed shared resolution semantic (`note_reference`, `relative_path`, or `uri`) plus provider syntax/subtype provenance and exact resolver input. URI facts carry provider-decoded scheme, authority, path, query, and fragment components, their encoding contract, and exact component ranges. `DocumentBaseFact` carries the first usable authored base. Shared URI resolution never routes through the Markdown resolver, and query remains navigation state rather than graph identity.
- Search regions are note evidence, never code-intelligence input. Their closed authored/derived origin, visible/supplemental retrieval kind, and optional source range let future HTML or script-derived content participate in note search without pretending it is source code.
- `RootMetadataPatchPlanner` is the pure mutation boundary: it receives sealed source, provider-current projection, and canonical root-field changes, then returns exact nonoverlapping byte patches. Providers cannot read or write files; applications adapt validated patches to the journaled writer.
- Capabilities distinguish source reading, search projection, metadata/link/fragment reads, metadata/link/move mutations, and structural mutation; none implies user authorization.
- The registry case-folds IDs and extension claims, rejects conflicts at construction, and returns copied descriptor slices.
- Ownership policy is generic: Markdown uses `default`, while HTML, YAML and JSON require `explicit_include`; consumers must not branch on provider IDs.
- `pkg/notemeta` owns source bytes, shared resolution, vault policy, and persistence. Projectors emit no resolved targets or persisted rows.
- Built-ins assemble only in `pkg/noteformat/builtin`; consumers receive the immutable registry/runtime rather than registering providers themselves.

## Structural reads (SPEC-0133, verified 2026-11-06)

`ProjectionFacts.Structure` is the schema-free owner/value envelope. Owners have source-local keys, syntax ancestry, source order, optional original-byte ranges and locator evidence. Values preserve ordered containers, scalar kinds and present null separately from missing. Graph identity and typed binding remain ontology responsibilities.

`Runtime.Project` validates and deeply detaches structure before publication. It rejects dangling/cyclic/duplicate owners and values, invalid membership and ranges, and structure without `structural_reading`. `CanReadStructure` requires the declared capability and an executable projector; a descriptor-only structural declaration fails composition. Reading never grants mutation support. HTML remains root-only. Markdown syntax payloads are optional evidence, never requirements for a readable child.

## SPEC-0133 boundary, last verified 2026-11-06

Structural reading requires validated provider-owned owners and source values plus an executable projector. Source values preserve ordered containers and distinguish null, empty string, empty list and empty map. StructureFacts deep copies isolate provider output; no provider imports ontology or mutation engines.

YAML/JSON built-ins adapt `structured.ParseYAML`/`ParseJSON` once into owner/value facts with RFC 6901 addresses. Their only capabilities are source and structural reading. Broad globs and classic vaults do not activate them; generated JSON requires an explicit include. The YAML scalar profile is provider-local and leaves Markdown frontmatter unchanged.
