## pkg/noteformat

Closed descriptor registry and optional projection runtime for authored note
files.

- Providers declare identity, extensions, independent provider/projection versions, and capabilities; they never read files or write persistence.
- `AuthoredSource` owns a sealed canonical byte snapshot: path, format, hash, size, mtime, and bytes are construction-only facts exposed through read-only accessors. `Runtime` rechecks canonicality and freshness before a projector call.
- Optional `Projector` implementations compose the descriptor-only `Provider` contract and receive sources only through `Runtime`. `Project` may run concurrently across authored sources, so implementations must synchronize mutable state. The runtime verifies the exact registered descriptor, exposes availability through `CanProject`, and never exposes a raw projector escape hatch.
- `Projection` owns the versioned stale/current/fatal result envelope plus copied unresolved authored facts. Root metadata values use the closed, canonical `MetadataValue` domain rather than arbitrary Go values. Root field names are nonempty; nested objects preserve every string key, including the empty string. Integer and float durable semantics stay distinct, date-only values preserve their calendar date, and timestamps preserve their authored offset and fractional precision. Diagnostics carry a closed category, affected operation, optional original-byte range, and blocking flag. Only whole-projection fatal results carry blocking diagnostics.
- Links declare one closed shared resolution semantic (`note_reference`, `relative_path`, or `uri`) plus provider syntax/subtype provenance and exact resolver input. URI facts carry provider-decoded scheme, authority, path, query, and fragment components, their encoding contract, and exact component ranges. `DocumentBaseFact` carries the first usable authored base. Shared URI resolution never routes through the Markdown resolver, and query remains navigation state rather than graph identity.
- Search regions are note evidence, never code-intelligence input. Their closed authored/derived origin, visible/supplemental retrieval kind, and optional source range let future HTML or script-derived content participate in note search without pretending it is source code.
- `RootMetadataPatchPlanner` is the pure mutation boundary: it receives sealed source, provider-current projection, and canonical root-field changes, then returns exact nonoverlapping byte patches. Providers cannot read or write files; applications adapt validated patches to the journaled writer.
- Capabilities distinguish source reading, search projection, metadata/link/fragment reads, metadata/link/move mutations, and structural mutation; none implies user authorization.
- The registry case-folds IDs and extension claims, rejects conflicts at construction, and returns copied descriptor slices.
- Ownership policy is generic: Markdown uses `default`, while HTML requires `explicit_include`; consumers must not branch on provider IDs.
- `pkg/notemeta` owns source bytes, shared resolution, vault policy, and persistence. Projectors emit no resolved targets or persisted rows.
- Built-ins assemble only in `pkg/noteformat/builtin`; consumers receive the immutable registry/runtime rather than registering providers themselves.
