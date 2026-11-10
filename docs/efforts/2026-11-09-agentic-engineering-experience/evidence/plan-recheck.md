# AE experience P1 plan recheck

VERDICT APPROVE

The goal remains Drew's complete installed AE experience for shapers and builders, delivered with real installed proof and an OPEN UNMERGED PR. This approves plan completeness only. Review ran in-thread on codex/gpt-6.1-sol high. No subagents, repository edits, commits, pushes, or external comments.

## P1 resolved

The authoritative foundation-contract.md lines 15-25 and promoted experience spec US4 lines 86-90 now supply the missing readable authority boundary. An optional explicit frozenScope relation selects one EffortMaterial. Only that material's typed frozen sections supply selected occurrences. Entry mentions, governingSpecs metadata, and a different work log remain separate evidence. The template's explicit selection of its existing log is authored authority, rather than a fallback inferred from workLog. Same-source roles share one canonical source. Legacy records retain unknown authority without migration.

This is locally implementable using existing contracts. The starter SDL already classifies work-log.md and materials/*.md as EffortMaterial and binds its frozen H2 sections (module/ontology/spec-driven.graphql lines 800-816). EffortWorkspace already uses metadata-only @link bindings for plan and log at lines 867-870. Add the optional selector using the same source/ambient-link controls. No HTML heading implementation or new lifecycle is needed.

Missing-state support is available, but consumers must request it. query/schema_workspace.go exposes fields.present, values, links, sectionNodes, issues, sourceRevision and source capabilities. NodeFieldLink retains authored values with null refs for unresolved or ambiguous targets. projection_bindings.go retains all matched section refs; build_assessment.go reports duplicate_section for a singular section with multiple headings. Therefore a null typed relation or a single returned narrative is insufficient evidence of absent or complete scope. Inspect the selected field and section states before declaring membership known. A selected HTML material or other unsupported source remains visibly incomplete.

## P2 no new blocking finding

The new relation does not require widening the shared API design. Keep offset plus occurrence count, the existing focused resolver, and the existing responseGeneration accessor. Governing links and inbound UserStory.efforts remain unsuitable evidence for frozen membership. Current stale source wording is implementation work explicitly required by the contract, rather than a remaining plan contradiction.

## Minimum coordinated implementation

All starter paths below are under pkg/app/cli/init/templates/starters/agentic-engineering/.

- module/ontology/spec-driven.graphql. Add nullable frozenScope: EffortMaterial with @link(source: "frozen-scope", includeBodyLinks: false, includeBacklinks: false). Update EffortMaterial and EffortWorkspace authority docstrings. Correct UserStory.efforts guidance so a generic inbound relation is not advertised as proof of exact selection. Preserve the existing relation's behavior.
- repo/docs/efforts/templates/workspace/overview.html.template. Add frozen-scope using {{work_log_path_json}} and describe HTML scope prose as a summary. Update work-log.md.template so it is authoritative when explicitly selected and a snapshot otherwise. No extra file or placeholder.
- agents/skills/agentic-engineering/references/effort-setup.md and repo/docs/efforts/README.md. Replace overview-owned-link/mirrored-log instructions with explicit source selection, source-role deduplication and legacy uncertainty. Refresh managed installed copies through init, not hand edits. Update the corresponding team-owned docs/efforts/README.md explanation under the authorized source change.
- module/recipes/spec-driven.yaml. Update effort-execution-context and closure-drift-pack, including expected paths and partial-result prose. Return selected scope identity/sections and field-state provenance for failures, separately from log snapshots and governing links. The frozen-spec-index-pack remains a spec reader; its backlinks must not become selection evidence.
- New delivery/lib and data adapter. Read selected material field/section evidence, then focused sourceLinks. Deduplicate canonical sources while retaining roles and occurrence order. Keep unresolved and partial cases unknown; preserve criterion-only scope.
- Extend pkg/app/cli/init/effort_template_smoke_test.go and focused starter recipe/domain tests. Prove the filled template's selector, both recipes, explicit-only resolution, and historical record preservation. Existing effort_assets_test.go provides installation/idempotence coverage to extend where necessary.

## Foundation proofs still required

Prove through the real public query and installed adapter that selected A is frozen, entry-mentioned B is not, and separate-log C remains a snapshot. Include absent selector, broken and unsupported target, missing and duplicate frozen headings, same-source roles, criterion-only scope, and governing/frozen disagreement. Assert the authored broken reference survives and duplicate sections cannot establish complete membership.

Retain the existing pagination foundation matrix at 0/99/100/101/200/201, first/offset edge cases, duplicate and unresolved occurrences, aliases and exact fragments, focused boundaries, count-only reads without target hydration, and unchanged existing callers. Prove coherent source revision and graph generation across pages and hydration, including target-only changes, missing generation, partial errors, late responses and explicit recovery. Independent implemented-foundation review still precedes dependent UI integration.

Final acceptance still requires disposable fresh/update/eject installs, init --check, actual installed UI proof in both themes and major states, native alternatives, applicable policy gates, implementation review, Greptile 5/5 and current-head CI. The PR must remain open and unmerged. Pending effort allocation is a setup gap, not contract rejection. Do not fabricate approval metadata or add another human approval pause.

Static review only. No runtime tests, live allocation, installed UI or PR status was verified. Compounding lookup failed on Atomic Object authentication; Rhizome retrieval was unavailable. The changed contract and cited source files supplied this review's evidence.
