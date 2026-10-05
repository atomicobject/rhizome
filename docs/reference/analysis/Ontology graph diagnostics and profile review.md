---
type: ReferenceDoc
summary: "Implementation review guide for ontology graph profile behavior, diagnostics, endpoint-scoped reads, and consumer debugging."
reference-kind: analysis
derived-from:
  - docs/specs/technical/ontology-indexed-read-model-contract.md
  - docs/specs/technical/noderef-batch-traversal-read-api.md
  - docs/efforts/2026-04-26-13-29-noderead-indexed-graph-read-model.md
last-verified: 2026-04-29
status: active
---

# Ontology graph diagnostics and profile review

## Summary

This note is a code-review map for ontology graph diagnostics and profile behavior. The governing contract is [[ontology-indexed-read-model-contract]]; this reference explains how the current implementation satisfies it across `pkg/ontology/noderead`, `pkg/ontology/readmodel`, `pkg/anchors/sqlite`, web graph endpoints, and CLI graph consumers.

Use it when graph output looks too broad, too sparse, missing code edges, missing embedded nodes, or hard to explain. The fastest debugging path is usually: identify the caller profile and flags, inspect endpoint scope, then compare diagnostics against the rows available from the indexed read model.

## Reviewed Implementation Surface

- `pkg/ontology/noderead/types.go` defines `GraphProfile`, `GraphRequest`, `GraphFactsRequest`, `GraphDiagnostics`, and the endpoint/edge envelopes.
- `pkg/ontology/noderead/graph.go` normalizes requests, chooses profile options, loads indexed rows, merges endpoints, applies edge precedence, records diagnostics, and caches graph/facts results by normalized request.
- `pkg/ontology/readmodel/graph.go` defines `GraphStore`, the storage boundary between sqlite-owned rows and noderead-owned profile merge behavior.
- `pkg/anchors/sqlite/graph_readmodel.go` implements ontology node/edge/doc-edge reads, including endpoint selector lookup and code-edge opt-in.
- `pkg/anchors/sqlite/graph_readmodel_doc_edges.go` merges markdown graph edges, note-code mention/coderef edges, and code-code intel edges into fallback read-model rows.
- `pkg/app/web/graph.go` and `pkg/app/web/ontology_graph.go` adapt noderead graph results into browser graph responses, cache by caller-visible flags, and expose diagnostics when requested.
- `cmd/graph_web.go`, `cmd/graph_path.go`, and `cmd/graph_surprises.go` consume `Graph` or `GraphFacts` for local graph inspection, shortest paths, and surprise scoring.
- Regression coverage lives mainly in `pkg/ontology/noderead/scope_test.go`, `pkg/anchors/sqlite/graph_inputs_test.go`, and `cmd/graph_path_test.go`.

## Profile Matrix

`GraphProfileOntologyNative` is the default profile for `GraphRequest`. It includes ontology catalog endpoints and typed ontology edges. It excludes fallback doc-link edges, untyped-only endpoints, and code endpoints. Use it to review typed ontology truth without ambient graph fill.

`GraphProfileNotesOnly` includes typed ontology endpoints and edges plus untyped note endpoints and markdown/doc-link fallback edges. It excludes code endpoints and code-code edges. Web sets this profile when `notesOnly` is true.

`GraphProfileCodeAware` includes `notes_only` behavior plus note-code evidence such as mentions and coderefs. It still excludes code-code call/import/type/member edges unless `GraphRequest.IncludeCodeEdges` is true.

`GraphFactsRequest` derives the lower-level profile from include flags:

- `IncludeCode` or `IncludeCalls` selects `code_aware`.
- `IncludeCalls` maps to `GraphRequest.IncludeCodeEdges`.
- `IncludeEmbedded` controls whether `embeds` edges and embedded/section nodes survive the facts-level filter.
- If neither `IncludeOntology` nor `IncludeDocLinks` is set, both default to true.

Important pitfall: `GraphProfileCodeAware` is not enough to include code-code edges. Callers must set `IncludeCodeEdges` or `IncludeCalls`. This is intentional per [[ontology-indexed-read-model-contract]].

## Endpoint Scope

Graph sources become endpoint-scoped when a `NodeRef` carries embedded/section kind, node ID, fragment, or structural fingerprint. Endpoint-scoped reads build selectors from:

- note path
- node ID
- kind
- fragment
- structural fingerprint
- `source_locator`

The sqlite read model matches selectors against `ontology_nodes` by catalog node ID, block/fragment, `source_locator`, and `node_ref_json` fields. `noderead.Scope.Graph` then keeps only edges incident to resolved source endpoint IDs. If the endpoint cannot be resolved, the result should stay empty/degraded instead of widening to the whole parent note.

Review checks:

- Embedded-node local graph requests should center on `embedded:<node_id>` when the catalog row exists.
- Missing embedded endpoints should not return note-level ontology or wikilink neighborhoods from the parent note.
- Parent note endpoints may be added for containment context, but they must not replace embedded source or target identity.
- Section endpoints are visible only when directly requested or needed to preserve source identity; ordinary structural sections should not become default graph nodes.

## Merge Order And Edge Precedence

The merge in `Scope.Graph` is intentionally ordered:

1. Load ontology node catalog rows and typed ontology edge rows from `GraphStore`.
2. Add typed note endpoints and parent note context for child endpoints.
3. Add visible embedded endpoints and `embeds` containment edges.
4. Add typed ontology edges with node-scoped endpoint resolution.
5. Add fallback doc/code edges only when the active profile permits them and the request is not endpoint-scoped.

Typed ontology edges win over duplicate fallback doc-link edges for the same visible source/target pair. When diagnostics are enabled, suppressed fallback edges appear in `GraphDiagnostics.Dedupe` with reason text such as "ontology edge supersedes doc link".

Fallback doc/code rows are evidence, not ontology truth. They come from `graph_doc_edges`, `doc_links`, and `intel_edges`, and they should retain their original kind, weight, and confidence rather than being recast as ontology relations.

## Diagnostics Behavior

Diagnostics are opt-in through `GraphRequest.Diagnostics` or `GraphFactsRequest.Diagnostics`. They are also part of the graph/facts cache key, so diagnostic-enabled reads do not reuse non-diagnostic cache entries.

Diagnostic buckets:

- `Nodes`: endpoint additions with source and reason, such as `ontology_node`, `ontology_edge`, `graph_doc_edge`, or `indexed_path`.
- `Edges`: edge additions with kind, relation name, provenance, and reason.
- `Skips`: excluded nodes or edges, including missing note paths, non-visible ontology kinds, out-of-scope endpoint edges, empty/self endpoints, excluded code endpoints, and profile-filtered untyped endpoints.
- `Dedupe`: fallback edges suppressed by typed ontology edges.

Diagnostics explain merge decisions; they should not change semantic result shape for callers that do not request them. Stable reason strings matter because tests, UI debugging, and future search diagnostics can compare them directly.

`GraphFacts` diagnostics come from the lower-level `Graph` call before the facts result applies final `IncludeOntology`, `IncludeDocLinks`, `IncludeCode`, `IncludeCalls`, and `IncludeEmbedded` filters. Reviewers should treat diagnostics as an explanation of available merge evidence, then compare returned facts/nodes when debugging final consumer output.

## Consumer Review Guide

For browser graph regressions:

- Check whether the request is global, module, local path, or local `NodeRef` mode.
- Confirm `notesOnly` maps to `GraphProfileNotesOnly` and `IncludeCodeEdges=false`.
- Confirm non-`notesOnly` graph modes set `GraphProfileCodeAware` and set `IncludeCodeEdges=true` only where code-code edges are expected.
- Confirm the web cache key includes the flags that change graph shape, especially `notesOnly` and `diagnostics`.
- For embedded-node local graphs, inspect `localGraphCenterID` behavior after noderead returns endpoints.

For CLI graph path and surprise regressions:

- `graph path` uses `GraphFacts` with ontology, doc links, code endpoints, and embedded endpoints included, but does not include calls by default.
- `graphFactsPathInputs` aliases note paths, endpoint IDs, and source locators so users can resolve embedded endpoints by author-facing locators.
- `graph surprises` currently scores source/target paths from `GraphFacts`; embedded endpoints retain endpoint IDs in the facts result, but scoring still collapses to source/target path fields where present.

For store-level regressions:

- `GraphOntologyNodes` should prefer endpoint selectors over path/prefix/global reads.
- `GraphOntologyEdges` should use endpoint selectors when present so incident embedded edges can be found without loading the whole parent graph.
- `GraphDocEdges` should include code endpoints only when `IncludeCode` is true and code-code edges only when `IncludeCodeEdges` is true.

## Known Gaps And Follow-Ups

- `GraphDiagnostics` currently records merge decisions but not store query counts, query timings, or catalog freshness/hash mismatch. Scope-level diagnostics cover some cache/load counters, but graph diagnostics remain reason-oriented rather than timing-oriented.
- Endpoint-scoped graph reads intentionally skip fallback doc edges. If a future UI needs note-code or wikilink context around a specific embedded node, it should add an explicit scoped-context mode rather than weakening the endpoint-scope invariant.
- Public graph profile names are not yet documented as stable external API constants. SPEC-0040 keeps this as an open question.
- `graph surprises` still reasons mostly in path terms. It can consume embedded metadata from `GraphFacts`, but its scoring model may need a later endpoint-native pass.

## Review Checklist

- Profile/flag pair matches caller intent.
- Endpoint-scoped sources use selectors and do not broaden on catalog misses.
- Embedded endpoints and `embeds` edges are present when `IncludeEmbedded` or profile visibility allows them.
- Typed ontology edges suppress duplicate fallback doc-link edges, not node-scoped ontology edges.
- Code-code edges require `IncludeCodeEdges` or `IncludeCalls`.
- Diagnostics-enabled requests remain cache-distinct from non-diagnostic requests.
- Node/edge limits are applied after enough overfetch to survive filtering, and facts-level limits are applied after kind/profile filtering.
- Tests cover the changed profile, endpoint, and diagnostic behavior before relying on manual graph inspection.
