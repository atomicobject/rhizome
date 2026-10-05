---
type: ReferenceDoc
reference-kind: analysis
summary: "Measured discovery-response overhead and a bounded proposal for higher-signal agent tools."
status: active
last-verified: 2026-09-07
---

# Agent tool signal to noise

## Finding

Compact discovery and less repeated search content are concrete opportunities. Search already filters and deduplicates results; whether its lower-ranked tail helps agents needs corpus evidence. These recommendations are proposed follow-up work, not implemented changes to the persistent code-mode candidate.

Drew supplied a colleague's agent feedback on September 7: `agent surface` returned over 30,000 characters to resolve a small syntax question; `ontology-query-schema` returned the entire GraphQL schema when the agent needed the EffortNote contract, required sections, lifecycle values and a few fields.

## Measured discovery overhead

Measurements use candidate `8d84d11e` against Rhizome's own repository configuration. Other ontologies will produce different sizes. Counts are output characters, not token estimates.

| Response | Characters |
| --- | ---: |
| `rzm agent surface` | 53,240 |
| The existing `file-context` record extracted from that response | 1,680 |
| `rzm agent file-context --help` | 1,278 |
| `rzm agent code surface` | 4,868 |
| `rzm agent code describe --operation file_context` | 1,852 |
| `rzm agent ontology-query-schema` | 50,883 |
| `rzm agent ontology-reference --type EffortNote` | 26,573 |

The delivered code-mode catalog already supports selective operation discovery: `agent code describe --operation file_context` is a small contract. The ordinary CLI and ontology discovery should gain equivalent narrowing.

`cmd/agent_surface.go` has no command selector and tells agents to prefer the full surface over command help. `cmd/agent_ontology.go` exposes the full executable schema without a type selector. The existing type-filtered reference is useful but includes extensive field guidance, annotations and repeated descriptions. Selecting a type alone does not produce a compact shape contract.

## Proposed changes

1. **Command-specific discovery.** Add a selector to the existing authoritative surface, derived from the same catalog/Cobra metadata. Return the selected command, relevant shared flags and subcommands. Keep a full inventory available explicitly. Update installed guidance to use targeted discovery for a flag question.
2. **Compact type contracts.** Extend existing ontology discovery with a shape-focused response. Preserve required fields/sections, source mappings, relationship targets, lifecycle enum values and references to governing guidance. Expand descriptions, authoring examples and the full schema on demand. Query-shape and Markdown-authoring contracts must remain distinguishable.
3. **Consistent search filtering and one representation of source content.** Apply caller scope, exact-symbol and test exclusions before answer/body construction. Return a concise answer packet with actionable result references; expand source bodies or rich diagnostic matches explicitly. Preserve an explicit detailed format for existing consumers.
4. **Smaller initial search pages, calibrated against the corpus.** Evaluate a role-covered first page around 8–12 results, with continuation for the rest. This is an experiment target, not an accepted cutoff. Measure useful evidence retained, task success, irrelevant-body characters, response size and follow-up fetches before choosing a score floor or default limit.

## Search evidence

At the measured candidate, the CLI has no default explicit result limit and applies a 150,000-character budget floor (`cmd/agent_context.go`, `cmd/agent.go`, `cmd/runtime_helpers.go`). Broad search retrieves up to 300 internal candidates, then applies a dynamic score-drop cutoff and considers at most 50 results. The cutoff deliberately retains flat and gradually declining score sequences (`pkg/app/mcp/semantic_query_unified.go`, `semantic_query_scoring.go` and their tests). Candidate count is not the same as returned result count or evidence quality.

Existing protections include bounded same-handle evidence merging, `MaxPerOwner=1`, path-level rollup, session body-fingerprint deduplication, within-packet variant collapse and continuation. These are meaningful controls and should be preserved.

The response contains answer roles, rendered `text`, previews and rich `matches`; these can repeat source content (`semantic_query_types.go`, `semantic_query_unified.go`). `applySemanticControls` currently runs after `semanticQueryUnified` builds the answer packet and text. It updates matches and counts, while the earlier text and answer fields can still include excluded material (`pkg/app/mcp/tool_semantic.go`). Scope filtering even warns that broad packets remain orientation evidence. A compact response should make narrowing consistent throughout the packet.

## Verification boundary

The discovery sizes above were measured. Search behavior was inspected in source and an independent read-only audit. No ranking change, corpus experiment or additional model launch was performed for this assessment. Keep the approved six-run comparison on its fixed candidate; use its traces as further evidence for the follow-up design.


## Approved follow-up implementation

[[../../specs/technical/agent-compact-discovery-and-evidence|SPEC-0093]] implements recommendations 1–3 and updates the installed code-mode example to return selected lines with outcome, freshness, omitted-match and truncation evidence. Search-tail calibration remains deferred; no model runs were added or frozen evaluation evidence changed.

A compiled-binary check on the implementation worktree measured minified JSON byte counts from the live repository ontology:

| Discovery request | Full response | Selected response |
| --- | ---: | ---: |
| Agent surface → `query-recipe run` | 53,558 | 1,303 |
| Executable query schema → `EffortNote` | 50,657 | 2,832 |
| Authoring reference for `EffortNote` → compact | 26,572 | 9,306 |

All three selected CLI payloads matched their generated-client payloads exactly. These are response-size observations for this ontology, not token counts or task-performance measurements. The authoring contract preserves requirements and policy; the query fragment exposes the executable shape separately. Fresh base, AE and Domain installations ran the installed selected-line example successfully, and second init was byte-identical in all three.

Search now applies exclusions before packing and continuation, and compact output stores body content once per source with role references. Budget reductions retain warnings, lane state and pagination, flag omitted sources, and fail explicitly if the required envelope cannot fit. This fixes the inspected consistency/duplication problem; it does not establish ranking quality or a new default page size.
