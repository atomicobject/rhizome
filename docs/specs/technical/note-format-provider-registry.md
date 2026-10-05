---
type: TechnicalSpec
summary: "Defines the compile-time note-format provider registry, format-neutral note identity and capability contracts, configured note ownership, and lifecycle rules that let Markdown and HTML share core Rhizome services without dual classification."
id: SPEC-0084
spec-status: active
last-updated: 2026-08-14
aliases:
  - SPEC-0084
  - Note format provider registry
  - note-format-provider-registry
---
# Note format provider registry

## Summary

Rhizome supports multiple authored note formats through an internal, compile-time provider registry. A provider owns the format-specific mechanics needed to recognize and parse a configured note source, expose root metadata and links, describe supported source-preserving mutations, and identify the parser version that produced its derived data. Core note services own vault-relative identity, discovery policy, classification precedence, lifecycle orchestration, link resolution, candidate matching, confirmation policy, and mutation safety.

The registry makes Markdown one provider rather than the implicit definition of a note and adds HTML as a peer for explicitly configured `.html` and `.htm` note sources. Configuration remains the intentional ownership and trust boundary, but upgrades do not reinterpret classic-vault defaults or broad legacy globs as active HTML authorization. That upgrade guarantee prevents accidental HTML activation; it does not freeze a legacy case-sensitive Markdown extension-ownership set. Rhizome never indexes one path simultaneously as both note and code.

This is a foundation contract for [[multi-format-html-notes|SPEC-0083]]. It provides the provider and ownership boundaries consumed by [[note-node-indexing-architecture|SPEC-0076]], which owns canonical source facts and their derived note/node read models. Format-specific HTML extraction, metadata, and viewer behavior are specified separately.

## Goals

- Represent Markdown, HTML, and later authored note formats behind one compile-time provider registry.
- Make note paths and source snapshots format-neutral without weakening vault-relative path safety.
- Let providers advertise granular read, metadata, link, fragment, and mutation capabilities instead of treating a format as globally editable or non-editable.
- Make extension-explicit configured note inclusion the authoritative classification and trust decision for active formats without silently broadening ownership on upgrade.
- Prevent duplicate note/code identity and stale derived data when configuration or extensions change ownership.
- Preserve equivalent initial, incremental, rename, move, and delete behavior across note formats.
- Keep link resolution, candidate matching, confirmation heuristics, transaction safety, and move policy in shared core services.

## Non-Goals

- Runtime loading, discovery, installation, or execution of third-party provider plugins.
- A public provider SDK or compatibility promise for external provider implementations.
- Enabling HTML notes through classic-vault defaults, broad legacy include globs, a feature flag, or separate per-file script and external-resource trust controls.
- Defining HTML parsing, searchable-region extraction, JSON root metadata, iframe rendering, or bridge behavior.
- Making every provider support structural editing or general-purpose content editing.
- Preserving simultaneous code and note projections for the same source path.
- Defining ontology projection semantics beyond the format-neutral source and capability inputs it consumes.

## Key Contracts

### Registry and provider identity

The application assembles a closed registry of built-in providers at compile time. Each provider has a stable format identifier, the filename extensions it can own, a parser/projection version, and a capability profile. Registration order MUST NOT become a hidden precedence mechanism; overlapping provider claims are an invalid application configuration unless an explicit future contract defines deterministic disambiguation.

The initial registry contains:

| Format | Supported extensions | Enablement |
| --- | --- | --- |
| Markdown | `.md`, matched case-insensitively | Existing configured note behavior |
| HTML | `.html`, `.htm`, matched case-insensitively | Only when selected by an extension-explicit configured note include |

Provider selection is based on normalized vault-relative path plus configured note ownership. A provider may reject malformed or unreadable content during parsing, but content sniffing MUST NOT silently transfer an included path to a different format or to code ownership.

Provider IDs and extension claims are compared case-insensitively during registry assembly. Claims such as `.html` and `.HTML` overlap and make application assembly invalid even though authored filename casing is preserved. Provider selection intentionally case-folds a terminal claimed extension for every provider: Markdown therefore recognizes `.md`, `.MD`, and mixed-case forms when a configured pattern or default selects Markdown. Literal basename and non-extension path matching retain their existing doublestar semantics.

### Format-neutral note identity

The canonical note path type represents a normalized, vault-relative file path and does not append `.md` or guarantee a Markdown extension. It preserves the authored filename extension while preventing absolute paths and vault traversal. Markdown-specific target types may remain within the Markdown provider, but shared note, graph, search, ontology, validation, CLI, MCP, and web contracts MUST accept format-neutral note identity.

Canonical source facts include provider identity and provider/parser version in addition to path, content, content hash, filesystem freshness, title, root metadata, authored links, and navigable targets. `SPEC-0076` owns the final source-snapshot and derived-read-model shape; this spec requires those contracts to carry enough format provenance to dispatch provider behavior and invalidate stale projections.

### Capability model

Providers advertise granular capabilities for the operations they implement. The initial capability vocabulary MUST distinguish at least:

- source reading and searchable-content projection
- root metadata reading
- authored-link and fragment-target extraction
- root metadata mutation
- link retargeting
- file rename/move participation
- structural content mutation

Capability checks describe available mechanics; they do not authorize an operation by themselves. Core policy still decides whether a requested mutation is safe, requires confirmation, or must be rejected. In particular, the HTML provider can support root metadata edits, link retargeting, and file moves without advertising structural or general-purpose HTML editing.

### Classification and trust ownership

Configured note include and exclude rules are evaluated before code-language classification for extensions owned by registered note providers. When an included path matches a provider, note ownership wins and the code classifier MUST NOT emit a second file, symbol, coderef, or search identity for that path. A supported path not included as a note remains eligible for ordinary code classification and MUST NOT be rendered or executed by note-viewer behavior.

HTML note ownership requires an include whose file-pattern component explicitly selects `.html` or `.htm`, case-insensitively. Exact filenames and extension-constrained globs qualify. Broad patterns such as `docs/**` or `**/*` do not activate HTML by themselves, even when they happen to match an HTML path. Excludes retain their existing precedence after a format has been explicitly enabled.

Classic vaults retain their existing Markdown-only ownership until the repository adopts configured includes that explicitly select HTML. Existing configurations therefore preserve their pre-upgrade HTML ownership set: installing a Rhizome version with the HTML provider MUST NOT cause a previously code-owned HTML path to become an executable note without a configuration edit. This guarantee is limited to accidental HTML activation; it does not prohibit the intentional case-insensitive provider-extension matching that recognizes uppercase or mixed-case Markdown extensions.

Extension-explicit HTML inclusion is the single product-level trust and rollout boundary for active HTML note viewing. The registry exposes that the source has note ownership; it does not add provider-specific trust prompts, per-file approvals, or feature flags.

The ownership plan remains filesystem-free. Filesystem discovery adapters MUST retain legacy dotfile and hidden-directory traversal exclusion before submitting candidates to the plan, and separately supply unified-ignore decisions through `IgnoreFunc`. Unified ignore semantics are not a replacement for those traversal exclusions.

### Lifecycle and freshness

Full indexing and live indexing use the same ownership and provider-selection rules for create, modify, rename, move, and delete events. Derived freshness includes the source content fingerprint, provider identity, provider/parser version, and any shared projection version that changes output semantics. A version change invalidates affected derived rows even when source bytes and mtime are unchanged.

Ownership transitions are replacement operations:

- When configuration gives a path note ownership, Rhizome removes code/coderef projections for that path and publishes note-owned source and derived state as one orchestrated transition.
- When configuration removes note ownership, Rhizome removes note metadata, ontology, search, graph, validation, and provider-derived state before allowing the code classifier to reclaim the path.
- A rename or move re-evaluates provider selection and note includes for both the old and new paths; it may therefore become an ownership transition.
- When an owned source becomes unreadable or fatally unparseable, Rhizome retains its note identity and blocking diagnostic while removing stale derived content that can no longer be justified by the current source.

The persistence implementation may use multiple bounded transactions, but readers MUST NOT observe durable simultaneous code and note ownership. Crash recovery or the next reconciliation pass MUST converge to the configured owner without requiring a manual rebuild.

Every committed ownership transition MUST advance a durable reconciliation generation in the same transaction that retires prior ownership. The transition result identifies affected source paths and note-declared anchor associations for immediate cache invalidation and rederivation. A restart that observes an unacknowledged generation performs a complete ownership reconciliation; acknowledgement succeeds only for the exact current generation so an older run cannot clear newer transition work.

### Shared link and mutation policy

Providers return authored link targets, target kinds, source spans, and format-specific encoding/replacement mechanics. Shared core services resolve vault paths and fragments, find candidates, rank ambiguity, determine safe versus confirmation-required actions, build transaction previews, detect source-hash drift, and revalidate the applied result.

Rename and move behavior follows the same confirmation and candidate heuristics regardless of source format. A provider MUST NOT invent a second resolver or weaken the shared mutation policy. Format-specific code may decline a mutation it cannot represent safely; it MUST fail closed rather than rewrite nearby or runtime-generated content.

### Cross-format note addressing

Shared resolution uses one candidate model across provider formats:

- an authored target with a supported explicit extension addresses that exact provider/path form
- extensionless Markdown links and wikilinks search the shared note candidate set across providers; no format receives hidden precedence
- when `report.md` and `report.html` are both viable for `report`, the extensionless target is ambiguous and callers must surface candidates rather than guessing
- aliases are provider-neutral candidate claims and participate in the same ambiguity rules
- fragments are interpreted by the selected target provider after file identity resolves
- HTML `href` values, Markdown links, and wikilinks may address any supported note format through the same canonical resolver
- v1 does not infer `.html`, `.htm`, or `index.html` from a directory-like target; those destinations require an explicit filename

Providers own parsing and encoding their authored target syntax. The shared resolver owns path normalization, relative-source context, exact-extension matching, extensionless candidates, aliases, ambiguity, and canonical target identity. The initial implementation establishes this shared resolver by extracting and reconciling the existing Markdown read resolver and validation candidate matcher; it is not assumed to exist already.

## User Stories

### US1 - Register a built-in note format without teaching every consumer its extension

- id:: ^SPEC-0084-US1
- summary:: Register a built-in note format once and let shared note services dispatch through its stable capabilities.
- status:: satisfied

#### Acceptance Criteria

- The compiled registry exposes one unambiguous provider for Markdown and one for `.html` and `.htm`, with HTML extension matching independent of filename case. ^SPEC-0084-US1-AC1
  verification:: Registry tests enumerate the built-in formats and resolve representative lowercase, uppercase, and mixed-case filenames.
- Adding a provider does not require extension switches in note search, graph, ontology, validation, CLI, MCP, or web consumers. ^SPEC-0084-US1-AC2
  verification:: Architecture tests or dependency review confirm consumers dispatch through format-neutral source contracts or registry-owned selection.
- Duplicate format identifiers or overlapping extension claims fail during application assembly with an actionable diagnostic. ^SPEC-0084-US1-AC3
  verification:: Unit tests register conflicting providers and assert deterministic startup failure.

### US2 - Classify configured HTML as one trusted note identity

- id:: ^SPEC-0084-US2
- summary:: Treat explicitly included HTML as a note and never retain a competing code identity for the same path.
- status:: ready

#### Acceptance Criteria

- An extension-explicit configured note include that selects an `.html` or `.htm` path assigns it to the HTML provider; classic-vault defaults, broad legacy globs, and the same extension outside qualifying includes remain Markdown-only or eligible for code classification. ^SPEC-0084-US2-AC1
  verification:: Discovery fixtures vary classic/collection mode, exact and extension-constrained includes, broad legacy includes, excludes, and filename casing around identical HTML files and assert the selected owner.
- An included path produces no code-file, symbol, coderef, code-search, or duplicate graph identity. ^SPEC-0084-US2-AC2
  verification:: Full and incremental integration tests inspect all note/code ownership tables and retrieval handles for one canonical owner.
- No separate HTML feature flag, script trust switch, external-resource switch, or per-file prompt is required after extension-explicit configured note ownership is established, and an upgrade without a configuration edit preserves the prior ownership set. ^SPEC-0084-US2-AC3
  verification:: Configuration migration and viewer enablement tests show extension-explicit note inclusion is the sole product trust gate and broad existing configuration does not activate HTML.

### US3 - Keep provider-derived state correct across source and ownership changes

- id:: ^SPEC-0084-US3
- summary:: Converge full and live indexing when note files, provider versions, or note-inclusion rules change.
- status:: satisfied

#### Acceptance Criteria

- Create, modify, rename, move, and delete events produce the same final note ownership and provider-derived state as a clean full index. ^SPEC-0084-US3-AC1
  verification:: Paired full/incremental fixture tests compare canonical source, search, graph, ontology, and code ownership outputs.
- Changing the provider/parser version invalidates and rebuilds affected projections even when file bytes and filesystem timestamps are unchanged. ^SPEC-0084-US3-AC2
  verification:: Freshness tests bump a fixture provider version and assert stale rows are replaced.
- Adding or removing note ownership removes the previous domain's derived rows and publishes only the new configured owner; a rename across rules follows the same transition. ^SPEC-0084-US3-AC3
  verification:: Integration tests change note includes and rename files across matched/unmatched paths without a manual database rebuild.
- Fatal current-source failure retains path identity and a blocking diagnostic but does not serve stale extracted content, links, graph edges, ontology fields, or search evidence. ^SPEC-0084-US3-AC4
  verification:: Corruption/read-failure tests assert identity/diagnostic retention and derived-row cleanup in full and live flows.

### US4 - Apply one link-resolution and mutation-safety policy across formats

- id:: ^SPEC-0084-US4
- summary:: Resolve and retarget provider-authored links through shared candidate, confirmation, and transaction infrastructure.
- status:: ready

#### Acceptance Criteria

- Markdown and HTML links with equivalent authored targets produce the same candidate set, selected note identity, ambiguity classification, and confirmation requirement, including explicit-extension, extensionless, alias, cross-format, and same-basename collision cases. ^SPEC-0084-US4-AC1
  verification:: Table-driven resolver tests feed equivalent provider link facts into the shared core and prove that `report.md` plus `report.html` makes `report` ambiguous while explicit targets remain deterministic.
- Rename and move previews use shared inbound/outbound resolution and confirmation heuristics while delegating only source-span encoding and replacement to the provider. ^SPEC-0084-US4-AC2
  verification:: Cross-format move fixtures compare preview decisions and assert byte-scoped provider rewrites.
- A missing capability, ambiguous source span, or source-hash drift fails closed before mutation and reports the unsupported or conflicting provider operation. ^SPEC-0084-US4-AC3
  verification:: Mutation-session tests exercise absent capabilities, overlapping spans, and replay against changed source.

## Requirements

### Must

- The provider registry MUST be assembled from built-in implementations at compile time.
- Provider IDs, supported extensions, parser/projection versions, and capability names MUST be stable persisted values rather than Go type names or registration order.
- Provider extension matching MUST be case-insensitive for every provider, including Markdown; the authored path and extension casing MUST remain unchanged. This intentional normalization does not promise to preserve a legacy case-sensitive extension-ownership set.
- Provider registration MUST reject case-folded duplicate provider IDs and overlapping case-folded extension claims.
- Shared note paths MUST be normalized vault-relative paths without an implicit `.md` suffix or Markdown-only type guarantee.
- Configured note includes and excludes MUST determine whether a supported provider path has note ownership; active HTML ownership MUST require an extension-explicit `.html` or `.htm` include.
- Classic-vault defaults and broad legacy include patterns MUST NOT activate HTML, and upgrading the provider registry without a configuration edit MUST preserve the prior ownership of `.html` and `.htm` paths. This HTML-activation guard does not constrain intentional provider-extension case normalization.
- Filesystem discovery adapters MUST preserve legacy dotfile and hidden-directory exclusions independently of their unified-ignore `IgnoreFunc` decision before delegating candidate ownership to the format-neutral plan.
- Configured note ownership MUST take precedence over code classification, and a path MUST NOT have simultaneous durable note and code ownership.
- HTML MUST remain disabled as a note format until configuration includes it; no independent feature flag or trust prompt is introduced.
- Provider capabilities MUST be granular enough to allow root-metadata edits, link retargeting, and moves without implying structural editing support.
- Core services MUST own path and fragment resolution, candidate matching, ambiguity ranking, confirmation policy, transaction preview, source-hash drift checks, and post-apply validation.
- Explicit-extension targets MUST resolve only to their addressed format/path; extensionless and alias targets MUST use one cross-format candidate set with no hidden provider precedence, and collisions MUST remain ambiguous.
- V1 resolution MUST NOT infer HTML extensions or directory `index.html` targets.
- Providers MUST own format-specific parsing, authored source spans, and safe serialization or replacement of only the source constructs they advertise.
- Initial and incremental indexing MUST apply the same provider selection, ownership, freshness, and cleanup rules.
- Freshness MUST include provider identity and parser/projection version so implementation changes can invalidate derived state.
- Ownership changes MUST clean all stale rows from the previous note or code domain and converge automatically without requiring a full manual rebuild.
- Unreadable or fatally unparseable owned notes MUST retain identity and a blocking diagnostic while stale derived knowledge is removed.
- Shared consumers MUST disclose format/provider identity where representation matters, while keeping canonical note identity independent of format.

### Should

- Provider registration should remain a small composition-root concern with no provider imports in downstream feature packages.
- Capability negotiation should return actionable unsupported-operation diagnostics suitable for CLI, MCP, validation, and web surfaces.
- Reconciliation should be idempotent and should repair interrupted ownership transitions on the next startup, index, or watcher pass.
- Provider versions should change only when derived output semantics change, not for refactors that leave persisted results identical.
- Test fixtures should compare full and incremental outputs for every registered format instead of maintaining provider-specific lifecycle assertions only.

### May

- A future spec may add a runtime provider SDK, but it must define isolation, compatibility, distribution, and trust contracts before changing this closed registry.
- Providers may expose additional capabilities after the shared capability vocabulary and caller behavior are specified.
- The physical database may retain unified tables as long as ownership, provider provenance, cleanup, and read isolation remain explicit.

## Related Contracts

- [[multi-format-html-notes|SPEC-0083]] owns the cross-surface product behavior and rollout boundary for first-class HTML notes.
- [[note-node-indexing-architecture|SPEC-0076]] owns canonical source facts, ontology/fallback identity, and derived note/node read models; it consumes this spec's format-neutral path, provider identity, and capability contracts.
- [[indexing-pipeline-architecture|SPEC-0012]] owns staged full/live orchestration, bounded queues, durable writer discipline, and correctness barriers.
- [[validation-fix-plan-and-apply-session|SPEC-0018]] owns shared preview, confirmation, drift detection, and apply-session safety where provider-backed fixes participate in validation.

## Foundation Review Boundary

The first implementation phase must establish the format-neutral path/source types, closed provider registry, capability vocabulary, classification precedence, ownership-transition contract, and versioned freshness inputs with Markdown parity tests. Pause for `foundation-review` after those seams are implemented and before downstream HTML extraction, ontology, mutation, or viewer work depends on them. Search/read support may ship before the active viewer when its indexing and ownership contracts are independently complete.

## Documentation Plan

- Update the vault configuration reference with provider-owned note extensions, case-insensitive matching, extension-explicit enablement, classic-vault and broad-glob upgrade behavior, include/exclude precedence, and the rule that explicit HTML note inclusion is the trust boundary.
- Update subsystem references for paths, vault discovery, note metadata, indexing, code classification, validation, and web routing when their contracts become format-neutral.
- Update CLI and agent/MCP reference material to disclose source format and unsupported provider capabilities consistently.
- Add an implementation reference for built-in provider registration, capability vocabulary, provider versioning, and ownership-transition diagnostics.
- Document the migration from Markdown-only `NotePath` assumptions and extension switches after the foundation implementation settles the concrete APIs.
- Document the cross-format addressing matrix, including explicit extensions, extensionless ambiguity, aliases, fragments, and the absence of implicit directory/index resolution.

## Open Questions

- None.
