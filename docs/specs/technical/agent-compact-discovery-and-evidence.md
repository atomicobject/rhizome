---
type: TechnicalSpec
id: SPEC-0093
aliases: [SPEC-0093]
summary: "Selective discovery and compact evidence let agents consume the relevant Rhizome contract and search results without repeated source bodies."
spec-status: active
last-updated: 2026-09-07
---

# Compact agent discovery and evidence

## Summary

Deliver the follow-ups approved by Drew after [[persistent-agent-code-mode|SPEC-0092]]: compact command and ontology discovery, consistent search exclusions, an explicit compact search response, and installed code-mode examples that project useful evidence. The measured [[../../reference/analysis/agent-tool-signal-to-noise|signal-to-noise assessment]] supplies the problem evidence. Retain the existing detailed API by default and use the same compiled contracts and shared application handlers for each applicable surface.

## Goals

- Let an agent discover only the operations and ontology types needed for a task.
- Distinguish executable query fields from Markdown authoring requirements.
- Apply search narrowing to every result representation and continuation page.
- Return source body content once in compact mode, with references and evidence qualifications preserved.
- Teach low-ceremony code-mode composition and verify it with fresh installations.

## Non-Goals

Removing Bash or the direct CLI, mandatory persistent scripts for simple operations, new ranking weights or long-tail thresholds, new result-limit defaults, extra model evaluation launches, edits to the consumed pilot evidence, main merge, or release publication.

## Requirements

### Selective discovery

Keep `agent code surface` as the small operation inventory and `code describe --operation` as the selected-method contract. Generated declarations and discovery share the existing catalog; do not introduce a second schema registry.

The ordinary agent surface accepts an exact command selector, including a selected nested command. A selected response includes that command's syntax, flags, applicable common flags and child commands, without unrelated command inventories or full capability/workflow text. Unknown selectors fail explicitly. The unselected full surface remains available and compatible. Code mode exposes the same selector through the existing `surface` operation.

Ontology discovery supports one exact type at a time through existing operations. The compact authoring reference includes the type identity and schema hash, required fields and sections, property/heading bindings, identifier shape, relationship targets, referenced enum values, applicable constraints/policies, and references for detailed guidance. It omits expanded prose and UI metadata. Related object types are named for explicit expansion rather than recursively inlined. Unknown or internal-only types fail explicitly; compact authoring discovery requires a type selector.

Selected executable-query discovery derives fields, arguments, root access and referenced enums/input shapes from the compiled executable GraphQL schema. It identifies itself as a selected fragment, lists unexpanded referenced types, and never implies that a partial fragment is the complete executable schema. Related object types expand only when explicitly requested. Query requirements and authored Markdown requirements remain distinct. Ordinary unselected query-schema output stays compatible.

Code-mode contracts expose these selectors with useful declared result shapes. CLI and MCP use the same shared implementation where the operation already exists on both; local-only operations remain local rather than becoming MCP tools just for code-mode access.

### Search consistency and compact representation

Apply scope, test exclusions and exact-symbol requirements before answer selection, pagination and text construction. Matches, answer roles, summaries, counts and emitted source bodies must describe the same eligible evidence. Never leave an excluded source body in a broader answer packet after filtering matches. Preserve ranking, owner diversity, internal candidate limits, score cutoffs, caller budgets, warnings and source freshness behavior.

Continuation tokens retain the effective filters. Token-only requests continue the same eligible sequence; explicitly conflicting controls fail rather than silently widening or narrowing the sequence. This applies to ordinary and multiple-query responses. Legacy tokens retain their supported default behavior.

An optional `compact` input projects the shared search response into one source collection with stable references and role references. Body content appears only once per source; body kind, truncation, session deduplication, warnings, lane status, counts and continuation remain available. Every emitted role reference resolves to an included source. Default detailed output remains compatible. CLI, MCP and code mode share this behavior.

### Installed examples and verification

The source templates for base Rhizome, Agentic Engineering and Complex Domain direct composed workflows through the shared code-mode guide. Examples discover only needed operations, use one short script/client with `finally` cleanup, select relevant findings and retain outcome/freshness/warning/truncation evidence. Avoid requiring an interactive driver or a new helper protocol. Direct CLI remains suitable for simple operations and Bash remains available for development tools.

Tests cover selected command/type success and explicit misses, dynamically compiled ontology changes, required authoring contracts and referenced enums, query-field/root accuracy, consistent exclusions in every representation, compact body uniqueness and role integrity, continuation controls, and detailed-mode compatibility. Real compiled-binary/generated-client checks demonstrate representative parity and reduced response size. Fresh base/AE/Domain installations execute the examples; repeated init is idempotent. Complete repository gates and fresh independent review before handoff. Existing PR review findings and CI failures are addressed within the same authorized branch; preserve prior effort history and frozen campaign evidence.
