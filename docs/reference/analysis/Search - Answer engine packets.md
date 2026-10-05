---
type: ReferenceDoc
summary: "How Rhizome turns ranked search candidates into agent-ready answer packets with evidence roles, coverage, confidence, and follow-up queries."
reference-kind: analysis
derived-from:
  - docs/efforts/2026-04-24-10-52-answer-engine-search.md
last-verified: 2026-04-29
status: active
---

# Search - Answer engine packets

## Summary

Rhizome's answer engine is an assembly layer above unified search. `pkg/search` retrieves and ranks candidates; `pkg/app/answer` turns those candidates into a task-shaped packet for agents: `mustRead`, `supporting`, `coverage`, `confidence`, and `nextQueries`. The packet is evidence-first, not generated prose.

## Runtime path

1. `rzm search` or `rzm agent semantic-query` builds one or more query specs.
2. `pkg/app/unifiedsearch` normalizes facets/seeds/intent and prepares provider/index dependencies.
3. `pkg/search/planner` chooses retrievers and rankers for the intent.
4. `pkg/search.Service.Search` returns ranked candidates with evidence.
5. `pkg/app/unifiedsearch` and `pkg/app/mcp` convert ranked candidates into answer inputs.
6. `pkg/app/answer.Build` fills required roles, reports missing coverage, and suggests next queries.

## Evidence roles

- `overview`: local `CONTEXT.md`, hubs, or README-style orientation docs.
- `documentation`: task-specific docs and reference notes.
- `implementation` / `entrypoint`: code files, anchors, or symbols likely to be edited or inspected.
- `test`: tests, examples, and behavior checks.
- `decision`: ADRs or durable constraints.
- `example`: worked examples or demos.

Role selection is coverage-driven. `mustRead` should include the best candidate for each required role before filling remaining slots by specificity and score. This prevents a packet of six similar docs or six similar code hits when the task needs both.

Code answer items should stay directly actionable in rendered labels: prefer `path:line Symbol` or `path:line FQN` when symbol metadata is available, and fall back to direct query specificity when upstream evidence specificity is absent.

## Query frame behavior

Broad natural-language queries are framed deterministically by `pkg/search/queryframe`. The frame normalizes terms, adds code-form variants, detects explanatory questions, and exposes query-specificity evidence. For example, "how are notes embedded?" can match `note`, `embed`, `note_embed`, `noteemb`, and `embed_notes`.

Explanatory `how`, `what`, `where`, `why`, and `explain` queries require docs plus code/pipeline evidence. They do not require tests unless the query mentions tests or the mode is `tests_for_code`.

## Primary semantic evidence

Primary semantic chunks are source-owned surfaces for modules, important anchors, and ontology nodes. They improve recall through bounded factual identity and context while remaining ordinary ranked evidence for the owning file, anchor, or note.

Ontology-node primary chunks preserve typed provenance (`nodeID`, `nodeType`, `nodeKind`, `nodeRef`) and compact parent/ancestor context. Presentation hydrates the already-ranked node into bounded node-local context after ranking so retrieval stays fast and selective.

Low-specificity semantic matches are skipped for explanatory packets when they cannot cover a required role. This keeps broad embedding similarity from crowding out concrete implementation and documentation evidence.

Ontology-aware packets may carry additive canonical `nodeRef` metadata when retrieval has resolved it. Answer assembly treats it as provenance only; it does not query the ontology or synthesize a second retrieval object.

## Confidence and gaps

Confidence reflects target resolution plus required role coverage:

- `high`: required evidence slots covered and no warnings.
- `medium`: useful packet, but some coverage or warning signals are incomplete.
- `low`: unresolved/ambiguous target, no selected evidence, or several required roles missing.

`nextQueries` are part of the answer contract. Agents should follow them when coverage is incomplete instead of issuing a broader retry.

## Diagnostics relationship

Raw and explain diagnostics are a mirror of the same run, not a second answer path. Use [[search-quality-evaluation-corpus]] to compare answer-shaped output with raw ranked output, and use [[search-diagnostics-explain-architecture]] for the trace contract. A source present in raw output but absent from `mustRead` should be explainable by answer role coverage, duplicate selection, low-specificity filtering, weak-decision deferral, or support-window overflow.

## Invariants

- Answer assembly must not perform retrieval, embedding, or filesystem-wide reads; bounded presentation-time reads for already-ranked ontology nodes are allowed.
- Answer packets must preserve provenance from ranked candidates.
- Multi-query answer packets should merge duplicate sources and preserve facet-match evidence before role selection.
- Required roles are intent/task-frame specific; there is no universal "missing tests means low confidence" rule.
- Budget trimming must shorten previews and prose before dropping structured `matches`; skeletal or compact matches should outlive rendered text when they fit because agents can act on path, symbol, and line fields.
- Raw ranked output remains available for debugging, but agent-facing defaults should be answer-shaped.

## Governing specs

- [[search-answer-workflow]]
- [[search-quality-evaluation-corpus]]
- [[search-diagnostics-explain-architecture]]
- [[unified-search-answer-architecture]]
- [[primary-semantic-chunks-and-noderef-search]]
