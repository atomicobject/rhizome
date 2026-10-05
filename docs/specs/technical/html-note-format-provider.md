---
type: TechnicalSpec
summary: "Defines the HTML note provider contract for canonical JSON root metadata, deterministic static search extraction, and source-mapped HTML links and fragments without executing authored content."
id: SPEC-0085
spec-status: active
last-updated: 2026-09-11
aliases:
  - SPEC-0085
  - HTML note format provider
  - html-note-format-provider
---

# HTML note format provider

## Summary

The HTML note format provider turns configured `.html` and `.htm` note sources into format-neutral note facts. It preserves the authored HTML as the raw representation while deriving root metadata, display title, searchable text, local link facts, and fragment targets with source ranges. Those facts feed the ownership and indexing architecture in [[note-node-indexing-architecture|SPEC-0076 Note/node indexing architecture]] through the provider boundary defined by [[note-format-provider-registry|SPEC-0084 Note format provider registry]].

HTML extraction is static and deterministic. It does not execute JavaScript, render a live DOM, load external resources, or use runtime mutations as knowledge. Inline scripts and source-embedded data blocks may still contribute bounded supplemental _note content_ so a self-contained document's authored dynamic content is searchable. They never acquire code identity or code-intelligence semantics.

This contract implements the HTML-specific ingestion behavior required by [[../product/multi-format-html-notes|SPEC-0083 Multi-format HTML notes]]. It intentionally projects only a note root. HTML headings may guide chunk boundaries and navigation context, but they do not become ontology sections, embedded nodes, or independently addressable structural owners in v1.

## Goals

- Parse valid and malformed repository HTML deterministically without requiring browser execution or network access.
- Define one canonical plain-JSON root metadata representation whose field meanings align with Markdown root metadata.
- Produce source-mapped visible and supplemental search regions that preserve one note owner across lexical and semantic retrieval.
- Extract authored `<a href>` links, document bases, and fragment targets for shared resolution, validation, graph, navigation, and maintenance infrastructure.
- Keep raw authored HTML distinct from derived searchable text and disclose that representation on every read surface.
- Preserve enough exact source evidence for diagnostics and narrowly scoped, source-preserving maintenance mutations.

## Non-Goals

- Executing JavaScript, evaluating templates, rendering CSS, fetching external scripts, or indexing a runtime DOM.
- Producing symbols, imports, calls, references, virtual JavaScript files, or any other code-intelligence identity from inline scripts.
- Converting HTML to Markdown or treating derived text as an editable source representation.
- Creating ontology sections or embedded nodes from headings, element IDs, microdata, RDFa, JSON-LD, DOM selectors, or arbitrary elements.
- General-purpose HTML editing, HTML conformance repair, element-ID mutation, DOM restructuring, or external URL validation.
- Indexing the bodies of external `src` scripts or other linked assets.
- Decoding legacy or heuristically detected character encodings; v1 accepts UTF-8 source with an optional UTF-8 BOM.

## Provider Contracts

### Raw and derived representations

The authored HTML bytes are the canonical source. V1 provider parsing accepts valid UTF-8 with an optional leading UTF-8 BOM; invalid UTF-8 produces a fatal provider diagnostic rather than locale-dependent replacement or encoding detection. Direct note reads and source views still return the exact raw representation. Search, semantic retrieval, agent context, file context, and answer evidence consume derived search regions. Responses that expose either representation MUST identify the note format and whether the content is `raw` or `derived`.

Every derived fact that can participate in evidence, diagnostics, navigation, validation, or mutation MUST retain a source range against the raw HTML bytes. Mutable facts additionally retain the authored spelling, decoded semantic value, and encoding/replacement context needed by the maintenance contract. Text decoding and whitespace normalization may make derived text differ from its source slice; the source range identifies the authored evidence rather than promising character-for-character reverse mapping.

The provider emits no structural note children in v1. A chunk planner MAY use heading boundaries and heading breadcrumbs to keep search evidence intelligible, but all such chunks remain evidence owned by the HTML note root. An element `id` or legacy anchor `name` is a navigation target, not an ontology node.

### Canonical root metadata

An HTML note has canonical root metadata only when it contains exactly one recognized block with this shape:

```html
<head>
  <script id="rhizome-metadata" type="application/json">
    {
      "type": "ReferenceDoc",
      "title": "Example"
    }
  </script>
</head>
```

The recognized `<script>` MUST be a direct child of `<head>`, its `id` MUST be exactly `rhizome-metadata`, and its media type MUST be `application/json`. HTML element and attribute names follow HTML's case-insensitive parsing rules; the identifier and JSON keys remain case-sensitive. Attribute ordering and insignificant HTML whitespace do not affect recognition.

The script body MUST parse as one JSON object. The provider retains the exact script-body span, decoded object, and surrounding newline/indentation context rather than treating reserialized JSON as source. Metadata keys have the same canonical meanings and ontology field mappings as their Markdown root-metadata equivalents. `type` declares the public ontology type for root-only projection. Values use ordinary JSON strings, numbers, booleans, nulls, arrays, and objects subject to the selected ontology field contract. The provider MUST NOT infer ontology fields from JSON-LD, microdata, RDFa, `<meta>` elements, CSS selectors, or other script blocks.

A missing metadata block is valid and produces an untyped note root. Multiple recognized blocks, invalid JSON, or a non-object top-level value produce a metadata diagnostic and leave the note untyped. Visible text and link indexing continue, while metadata-dependent mutations validate the source and fail closed until the ambiguity is fixed. This local failure does not require per-capability projection status. Every script block matching the canonical `id` and media-type marker is excluded from supplemental search content, including duplicate or malformed candidates, so invalid metadata never becomes a second body-evidence path.

Title resolution uses the first non-empty usable value in this order:

1. a string `title` in valid canonical root metadata
2. text from the document `<title>`
3. text from the first statically visible `<h1>`
4. the note filename

Invalid or non-string metadata `title` values produce the normal field diagnostic and do not prevent the later title fallbacks.

Using the first visible `<h1>` as title evidence does not remove that heading from the visible-text projection. Search planning MUST identify the repeated title/body evidence so the same text does not receive an accidental ranking boost merely because it supplied both title fallback and visible content.

### Static visible text

Visible search regions are derived from the parsed document in source order. They include human-facing text from headings, prose, lists, tables, captions, controls, and other rendered body content; preserve meaningful whitespace in `<pre>` and `<code>`; retain useful non-empty image `alt` text; and add deterministic boundaries between block, list-item, table-row, and table-cell content.

Character references are decoded. Collapsible HTML whitespace is normalized deterministically outside preformatted content. Comments, metadata, `<script>`, `<style>`, `<template>`, non-content head elements, and subtrees marked `hidden` or `aria-hidden="true"` are excluded from visible regions. Static extraction does not evaluate stylesheets or run layout, so class-based or externally styled visibility is not an indexing dependency.

The parser MUST use tolerant HTML error recovery. Recoverable malformed markup is interpreted using deterministic HTML parsing rules rather than rejected as a whole document. Provider/parser format versions participate in freshness so a parser or extraction-rule change invalidates the corresponding derived facts.

### Supplemental inline script and data content

Each source-embedded `<script>` body without a `src` attribute produces a supplemental search region, except the canonical `rhizome-metadata` block. Executable classic/module JavaScript and non-executable JSON or other declared data blocks are included as raw authored text with their declared media type, document order, and exact source range. Event-handler attributes, CSS, external script bodies, runtime-generated content, and fetched data are not supplemental regions.

Supplemental regions are note content, not code indexing input. They MUST share the HTML note root's canonical owner and MUST NOT produce symbols, imports, call edges, code references, link facts, ontology structure, or a second code-file result. Lexical and semantic retrieval MUST index them, while every retrieval lane MUST apply the lower supplemental weight before candidate truncation so implementation details remain discoverable without routinely displacing comparable visible prose.

Supplemental text is chunked under the normal note-content budgets. Per-region and per-note caps MUST be deterministic. Truncation preserves earlier source order, retains source provenance for emitted chunks, and produces diagnostics with original and indexed sizes. Oversized or malformed script/data content MUST NOT prevent visible text, metadata, or link indexing.

### Links, bases, and fragments

The provider extracts each authored `<a href>` as a raw link fact with its authored target, decoded semantic target, exact attribute-value source span, quote/entity/encoding context, enclosing element context, and document order. It also extracts the first usable `<base href>` under HTML document semantics and all local fragment targets declared by any element `id` or legacy `<a name>`. Script strings, CSS URLs, template text, and runtime-created anchors are never link facts.

Shared note infrastructure, not the provider, owns candidate matching, ambiguity handling, confirmation policy, and final target identity. Resolution MUST apply these HTML semantics:

- a relative path resolves from the source note's directory, after applying an internal document base when present
- a leading `/` resolves from the vault root
- a query string is preserved for navigation but excluded from canonical note identity
- a fragment is decoded and retained separately from file identity
- percent-encoded path and fragment components are handled without double decoding or changing authored spelling during read-only extraction
- same-document fragments target the current note
- fragments may resolve to any element `id` or legacy `<a name>` and do not create structural nodes
- the first usable `<base href>` determines relative-link resolution; an external base makes relative targets external
- absolute, scheme-relative, or non-file-scheme targets are external and create no internal note edge
- a local-looking target that escapes the vault root is invalid as an internal target and produces a diagnostic
- a supported explicit filename extension selects that path/provider, while extensionless and alias targets enter the shared cross-format candidate set with no hidden format precedence
- directory-like targets do not imply `.html`, `.htm`, or `index.html` in v1

Queries do not affect backlinks or graph identity. External targets are retained as authored external links for presentation and navigation, but the provider does not fetch or validate them. Broken internal paths, missing fragments, duplicate fragment declarations, invalid percent encoding, and ambiguous candidate matches remain distinct diagnostics so validators and maintenance tools can choose safe behavior.

## User Stories

### US1 - Derive stable root facts from authored HTML without changing the source

- id:: ^SPEC-0085-US1
- summary:: Derive canonical root metadata, title, raw representation, and source evidence from configured HTML notes without converting or rewriting them.
- status:: ready

#### Acceptance Criteria

- A valid direct-head `rhizome-metadata` JSON object supplies root metadata and `type`, while no other HTML metadata mechanism affects ontology projection. ^SPEC-0085-US1-AC1
  verification:: Index fixtures containing canonical metadata, JSON-LD, microdata, `<meta>` values, and unrelated data scripts; only the canonical object appears as root metadata.
- Missing metadata leaves the note valid and untyped, while duplicate, malformed, or non-object metadata emits a diagnostic, leaves the note untyped, and causes metadata-dependent mutations to fail without suppressing text and link facts. ^SPEC-0085-US1-AC2
  verification:: Compare provider results for missing, duplicate, malformed, and scalar JSON fixtures.
- Display title follows metadata `title`, document `<title>`, first visible `<h1>`, then filename precedence. ^SPEC-0085-US1-AC3
  verification:: Use one fixture for every fallback boundary, including invalid and empty candidate values.
- Direct note reads return raw HTML, derived consumers receive representation-labeled projections, and emitted facts retain raw-source ranges. ^SPEC-0085-US1-AC4
  verification:: Compare API/read responses and source slices for metadata, title evidence, text, and links.

### US2 - Search the complete authored note without executing it

- id:: ^SPEC-0085-US2
- summary:: Make static visible content and source-embedded script or data content searchable as one note while keeping supplemental content subordinate to prose.
- status:: ready

#### Acceptance Criteria

- Visible extraction includes representative headings, prose, lists, tables, captions, preformatted text, controls, and useful image alt text in deterministic document order. ^SPEC-0085-US2-AC1
  verification:: Assert golden visible-region text and source ranges for a representative fixture.
- Comments, styles, templates, canonical metadata, ordinary scripts, and syntactically hidden subtrees do not appear in visible regions. ^SPEC-0085-US2-AC2
  verification:: Search unique sentinel terms placed in every excluded context.
- Inline JavaScript and inline data-block bodies appear as lower-weight supplemental note evidence with media type and source range, excluding external `src` scripts and every canonical-marker metadata candidate; weighting occurs before lexical and semantic lane truncation. ^SPEC-0085-US2-AC3
  verification:: Run lexical and semantic searches for unique visible, inline-script, inline-data, external-script, and metadata terms and inspect owner identity and evidence kind.
- Supplemental evidence never produces code symbols, code edges, code references, structural nodes, or a second result identity for the HTML path. ^SPEC-0085-US2-AC4
  verification:: Inspect code-intelligence, graph, ontology, and merged search outputs for a script-heavy HTML fixture.
- Deterministic content budgets truncate supplemental evidence with diagnostics without dropping visible evidence or failing the note. ^SPEC-0085-US2-AC5
  verification:: Index an oversized inline-script fixture twice and compare chunks, spans, diagnostic counts, and hashes.

### US3 - Resolve authored HTML links through shared note infrastructure

- id:: ^SPEC-0085-US3
- summary:: Surface local anchors and fragment targets with sufficient authored evidence for shared graph, validation, navigation, and retargeting behavior.
- status:: ready

#### Acceptance Criteria

- Relative, vault-root, query-bearing, percent-encoded, fragment-only, and cross-note-fragment links resolve to the expected canonical note and fragment identity. ^SPEC-0085-US3-AC1
  verification:: Run a table-driven fixture matrix from nested directories and compare authored targets, canonical identities, queries, fragments, and source spans.
- An internal base adjusts relative paths, while an external base makes relative targets external; absolute external schemes create no internal graph edge. ^SPEC-0085-US3-AC2
  verification:: Index equivalent links under no base, internal base, and external base fixtures and inspect graph facts.
- Any element `id` and legacy `<a name>` can satisfy a fragment without creating an ontology section or embedded node. ^SPEC-0085-US3-AC3
  verification:: Validate and navigate links to heading, arbitrary-element, and legacy-anchor fragments, then inspect projected node inventory.
- Invalid, out-of-vault, missing, duplicate-fragment, and ambiguous targets retain distinct diagnostics and never become guessed graph edges. ^SPEC-0085-US3-AC4
  verification:: Exercise each failure class and confirm candidate confirmation remains in shared infrastructure rather than provider-specific policy.
- Link facts preserve the exact authored attribute-value span needed for a later source-preserving retarget operation. ^SPEC-0085-US3-AC5
  verification:: Round-trip links using single quotes, double quotes, entities, percent encoding, and surrounding attributes; only the target value span is selected.

### US4 - Recover predictably from malformed or oversized HTML

- id:: ^SPEC-0085-US4
- summary:: Preserve available note knowledge and actionable diagnostics when HTML, metadata, links, or supplemental regions are degraded.
- status:: ready

#### Acceptance Criteria

- Recoverable malformed HTML produces the same deterministic facts on full and incremental indexing. ^SPEC-0085-US4-AC1
  verification:: Reindex malformed fixtures through full and incremental paths and compare provider projections and hashes.
- A metadata failure disables typing and metadata mutation only; text, links, fragments, raw reads, and diagnostics remain available. ^SPEC-0085-US4-AC2
  verification:: Corrupt only the canonical metadata body and compare capability and projection results before and after.
- A source-range mapping failure is reported and disables only mutations that require the missing exact span; read-only derived content remains available when trustworthy. ^SPEC-0085-US4-AC3
  verification:: Inject a span-mapping failure and inspect capability degradation, search results, and diagnostics.
- The provider performs no network request and executes no authored script during parsing, validation, full indexing, or incremental indexing. ^SPEC-0085-US4-AC4
  verification:: Run fixtures with external resources and side-effecting scripts under a network/execution sentinel.

## Requirements

### Provider output and ownership

- The provider MUST emit format-neutral root metadata, title evidence, search regions, link facts, fragment targets, diagnostics, freshness inputs, and source ranges without introducing a parallel HTML-specific note identity.
- Every derived search region MUST carry `kind` (`visible` or `supplemental`), media type, derived text, source range, document order, and the canonical note-root owner.
- Provider and extraction-format versions MUST participate in invalidation under SPEC-0084 and SPEC-0076.
- Full and incremental provider execution MUST converge to byte-equivalent facts for the same source, configuration, and provider version.
- HTML parser recovery MUST be deterministic and MUST NOT depend on operating-system browser behavior.

### Metadata and representation

- The provider MUST recognize only the canonical direct-head JSON block as HTML root metadata and MUST require a top-level JSON object.
- Root metadata output MUST preserve the decoded JSON object, exact script-body span, and newline/indentation/replacement context required for source-preserving maintenance.
- HTML parsing MUST accept only valid UTF-8 with an optional UTF-8 BOM in v1; invalid UTF-8 MUST retain raw-read availability but produce no derived provider facts.
- Canonical metadata field meanings MUST remain format-neutral; HTML-specific metadata aliases or ontology semantics MUST NOT fork from Markdown equivalents.
- Root metadata diagnostics MUST identify the source span and failure class without echoing more source content than needed.
- Raw and derived read surfaces MUST identify representation and format; no caller may silently mistake extracted text for writable HTML source.
- Root typing MUST remain note-root-only; headings, IDs, and DOM nesting MUST NOT produce ontology children in v1.

### Search extraction

- Visible and supplemental extraction MUST be static, bounded, source-mapped, and deterministic.
- Supplemental content MUST be searchable through the same note-owned lexical and semantic surfaces as visible content, with lower pre-truncation ranking weight and explicit evidence-kind disclosure.
- Every script matching the canonical metadata marker MUST be excluded from supplemental regions, including duplicate and malformed metadata candidates.
- External resource contents and runtime DOM changes MUST NOT affect indexing completeness, freshness, or success.
- Script and data bodies MUST NOT enter code-intelligence pipelines in v1.

### Link extraction

- The provider MUST expose authored HTML link syntax and spans; shared infrastructure MUST own resolution, candidate matching, confirmation, canonical target identity, and graph policy.
- Mutable link facts MUST preserve authored and decoded targets plus quote/entity/encoding context; a provider that cannot prove the exact value span MUST withhold link-retarget capability for that fact.
- Resolution MUST preserve authored queries for navigation while excluding queries from note identity and graph deduplication.
- Resolution MUST use the shared cross-format explicit-extension, extensionless, alias, and ambiguity rules and MUST NOT infer HTML extensions or directory indexes in v1.
- Fragment validation MUST accept element IDs and legacy named anchors without promoting them into ontology structure.
- Link extraction and maintenance MUST ignore URLs embedded in scripts, styles, templates, event attributes, and runtime DOM state.

### Quality and observability

- Fixture coverage MUST include `.html` and `.htm`, mixed-case extensions, UTF-8 with and without BOM, invalid UTF-8, valid and malformed documents, missing/duplicate/malformed metadata, each title fallback, visible/hidden content, inline scripts/data, bases, external resources, encoded links, cross-format collisions, directory-like targets, and fragments.
- Golden tests MUST verify derived text, evidence kind, media type, source range, document order, owner identity, and deterministic fingerprints.
- Integration tests MUST prove lexical and semantic retrieval, raw reads, graph/link validation, and full/incremental convergence without script execution or network access.
- Diagnostics MUST distinguish metadata, parser, source-map, link-resolution, missing-fragment, and budget-truncation failures. Operation-local failures MUST be enforced by the affected operation's source validation rather than a persisted per-capability projection-status matrix.
- Before HTML extraction is delivered, diagnostics needed by reads, validation, repair eligibility, and lifecycle convergence MUST either survive durable projection state or be deterministically recomputable from the current source and projection inputs. The implementation MAY choose that read model then; this requirement does not require a per-capability projection-status matrix or make metadata-local damage a whole-projection fatal failure.

## Failure Modes

- **Duplicate or invalid canonical metadata:** preserve raw read, text, links, and fragments; leave the root untyped; emit a metadata diagnostic; fail metadata-dependent mutations when attempted.
- **Malformed document structure:** apply tolerant deterministic recovery and emit useful diagnostics only when recovery loses or ambiguously maps required facts.
- **Unresolvable internal link or fragment:** retain the authored link and source span, omit the guessed graph edge, and emit the specific resolution diagnostic.
- **External or unavailable resources:** do not fetch them and do not degrade indexing; external resource success belongs only to live viewing.
- **Supplemental-content overflow:** truncate by deterministic source-order budgets, report indexed versus available size, and preserve visible evidence.
- **Unavailable exact source mapping:** keep trustworthy read-only projections, expose degraded mutation capability, and never approximate a rewrite span.
- **Fatal unreadable source:** return a provider diagnostic and no stale derived provider facts; lifecycle cleanup remains governed by SPEC-0076 and SPEC-0084.

## Dependencies

- [[../product/multi-format-html-notes|SPEC-0083 Multi-format HTML notes]] defines the user-visible product boundary and trust model.
- [[note-format-provider-registry|SPEC-0084 Note format provider registry]] defines ownership, provider dispatch, capabilities, path identity, and freshness.
- [[note-node-indexing-architecture|SPEC-0076 Note/node indexing architecture]] defines canonical source ownership, derived read models, note/root identity, and search evidence ownership.
- The existing indexing, notemeta, search, ontology, noderead, validation, and shared link-resolution subsystems consume provider facts without re-parsing HTML independently.

## Open Questions

None.

## Documentation Plan

- Update the notemeta and indexing subsystem references to document HTML provider outputs, provider-version invalidation, and full/incremental convergence.
- Update the search subsystem reference to document visible versus supplemental regions, ownership, weighting, budgets, and evidence-kind disclosure.
- Update the ontology and noderead subsystem references to state that HTML v1 projects only a note root and treats fragments as navigation locators.
- Update validation and mutation documentation with canonical metadata and HTML link diagnostic classes plus source-span capability degradation.
- Add an HTML note authoring guide showing the canonical JSON block, title precedence, searchable-content boundary, link/base rules, and raw-versus-derived representations.
