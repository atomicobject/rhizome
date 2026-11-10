# AE experience plan review

VERDICT CHANGES

The goal is a complete installed Delivery experience for designers/leads and developers/agents, with an open unmerged PR. This is independent plan review, not implementation acceptance. Review ran in-thread on codex/gpt-6.1-sol high. No agents, repository edits, tests, live acceptance, commits, pushes, or external comments were made.

## P1. Blocker. HTML entry frozen scope has no specified readable boundary

Locations:

- `docs/efforts/2026-11-09-agentic-engineering-experience/evidence/experience-spec-draft.md:74` requires frozen story/criterion selections exclusively from focused frozen-section sourceLinks for both effort forms.
- `docs/efforts/2026-11-09-agentic-engineering-experience/evidence/effort-plan-draft.md:78-80` requires entry-owned workspace selections, refuses log override, and specifies section traversal as the implementation route.
- `docs/efforts/2026-11-09-agentic-engineering-experience/evidence/foundation-contract.md:30-38` retains this constraint while limiting shared changes to occurrence pagination.

Current source establishes the gap. The installed `agentic-engineering/references/effort-setup.md:20-23` says the HTML overview owns scope, the log mirrors it, and HTML headings are not ontology sections. In template sources, `module/ontology/spec-driven.graphql:824-879` gives EffortWorkspace explicit component/governing links but no frozen-section fields. The corresponding section fields belong to EffortMaterial at lines 807-816. `repo/docs/efforts/templates/workspace/overview.html.template` puts Frozen scope, document navigation, and other narrative in the same HTML document. `pkg/ontology/source_links.go:54-79` scopes Markdown sections by their snapshot ranges; pagination does not create such a section in an HTML entry.

Concrete case. The overview freezes story A, mentions story B outside Frozen scope, and its linked log mirrors an older selection C. The proposed adapter cannot identify A using a focused overview section. Reading every overview link admits B; reading the log substitutes C; reporting unknown forever fails the promised complete workspace brief and mismatch detection. Offset/count addresses quantity, not scope attribution.

Minimal fix. Before dependent implementation, select and document a small supported authoritative read contract for HTML entry selections. A narrowly scoped provider-backed boundary or an explicit authored metadata selection are viable choices. Update the workspace authoring/template and query ownership only as needed for that choice. Preserve log snapshots separately. Do not add a browser parser or silently promote the log to authority. Freeze a fixture with different overview scope, out-of-scope mention, and log selection, and prove the intended distinction through the actual public API.

## Architecture judgment

Root's five factual primary groups with supporting Current contracts and Record review are preferable to the viable seven-view alternative. Both can preserve truth if the alternative labels Building/Closing as checklist observations and To reconcile as investigation. Root keeps active effort membership independent of checklist reads, reduces initial detail reads and classifier cases, and keeps history directly available. Both use the same small offset/count API and generation accessor. Both remain testable with pure rules. Root sacrifices some visual similarity to the prototype's Closing tile but keeps its map, dense rows, two-column composition, inline criteria, and panel. That is a reasonable tradeoff. The draft's exclusive computed pipeline is not the strongest competing architecture; the independent seven-view sketch is.

The remaining product scope covers prompts and selection, both forms, navigation, empty/error/partial/loading states, themes, accessibility, narrow screens, and installation lifecycle without an invented approval gate. The final foundation contract gives independent ownership to domain, UI, query, and packaging. Keep that ownership authoritative over the older Batch 2 wording that combines several lanes. Replace the draft plan's obsolete page-info paragraph with the chosen offset/count contract before handing it to implementers.

## Required foundation and final proofs

- SourceLinks count/list must use the same focused projection and preserve occurrence order, duplicates, unresolved links, overlays, aliases, fragments, and existing first semantics. Test zero, exact cap, overflow, paging boundaries, invalid offset, count-only zero target hydration, and the unchanged browser operation in `web/src/api/graphql/operations.ts:101`. SDL, Prepare, introspection, discovery, and query docs must agree.
- Exporting `responseGeneration` is supported by its current implementation in `web/kit/coverage.ts`. Check original page responses and the aggregate returned by readNodes. `readNodes.complete` alone does not establish generation coherence. Prove target changes without source edits, source edits, missing generations, partial errors, late responses, and explicit recovery without indefinite retries.
- Prove P1 through the public read surface before accepting the two-form adapter. Also prove criterion-only scope, governing/frozen disagreement, body-only mentions, mixed story states, selected versus unselected open stories, and all-checked active efforts.
- Build and install into disposable fresh/update/ejected fixtures, run init --check, and inspect the actual installed UI. Preserve native view IDs and overrides. Exercise panel/page subjects, deep links, tabs/history, prompts/clipboard failure, both themes, narrow/zoomed layouts, keyboard/focus, and partial/error recovery. Inspect matched screenshots against all six supplied comps.
- Complete local policy gates, independent implementation review, Greptile, effort closure, and current-head CI before calling the open unmerged PR ready. Allocate and promote typed records through the live authoring path; current-user absence does not cancel authorization or justify fabricated approval metadata.

All five requested artifacts were read in full using split reads after output limits, and all six comps were opened. Compounding lookup reported an Atomic Object authentication failure before claiming no matches. That is a retrieval gap. Live allocation was not rechecked. Static code inspection supports this review; no runtime behavior or acceptance pass is claimed.
