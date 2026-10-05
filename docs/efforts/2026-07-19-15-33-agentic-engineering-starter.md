---
type: EffortNote
id: EFF-2026-07-19-15-33
name: Agentic Engineering starter
created-at: 2026-07-19T15:33:00Z
status: complete
summary: Replace the spec-driven workflow starter identity and duplicated skill suite with a lean agentic-engineering router, phase resources, thin adapters, concise team extension docs, and an explicit migration.
plan-approved-by:
aliases:
  - EFF-2026-07-19-15-33
---
# Agentic Engineering starter

## Scope

Deliver the canonical `agentic-engineering` starter and migrate existing `spec-driven` installations without silently losing local process guidance. Consolidate shared workflow procedure into one router with lazy resources, preserve manually useful phase adapters, move universal Rhizome leverage guidance to the core `rhizome` skill, replace the sprawling starter process-doc set with concise plain-Markdown extension points, and keep specification-specific ontology/query asset names where accurate.

Out of scope: redesigning `complex-domain`, renaming specification ontology/query assets, adding structured process-policy data, or maintaining `spec-driven` as a permanent equal alias.

## Spec Set (Frozen)

Frozen on 2026-07-19 at 2026-07-19T15:33Z.

- [[init-starter-workflow|SPEC-0038]] (`last-updated: 2026-07-19`)
- [[init-template-architecture|SPEC-0039]] (`last-updated: 2026-07-19`)
- [[base-rhizome-agent-guidance|SPEC-0080]] (`last-updated: 2026-07-18`; universal-mechanics ownership constraint)

## Stories In Scope (Frozen)

- [[init-starter-workflow#^SPEC-0038-US2|SPEC-0038.US2]]
  - [[init-starter-workflow#^SPEC-0038-US2-AC1|SPEC-0038.US2.AC1]]
  - [[init-starter-workflow#^SPEC-0038-US2-AC2|SPEC-0038.US2.AC2]]
  - [[init-starter-workflow#^SPEC-0038-US2-AC3|SPEC-0038.US2.AC3]]
- [[init-starter-workflow#^SPEC-0038-US8|SPEC-0038.US8]]
  - [[init-starter-workflow#^SPEC-0038-US8-AC1|SPEC-0038.US8.AC1]]
  - [[init-starter-workflow#^SPEC-0038-US8-AC2|SPEC-0038.US8.AC2]]
  - [[init-starter-workflow#^SPEC-0038-US8-AC3|SPEC-0038.US8.AC3]]
- [[init-template-architecture#^SPEC-0039-US5|SPEC-0039.US5]]
  - [[init-template-architecture#^SPEC-0039-US5-AC1|SPEC-0039.US5.AC1]]
  - [[init-template-architecture#^SPEC-0039-US5-AC2|SPEC-0039.US5.AC2]]
  - [[init-template-architecture#^SPEC-0039-US5-AC3|SPEC-0039.US5.AC3]]
  - [[init-template-architecture#^SPEC-0039-US5-AC4|SPEC-0039.US5.AC4]]
  - [[init-template-architecture#^SPEC-0039-US5-AC5|SPEC-0039.US5.AC5]]

## Spec Coverage Checklist

- [x] [[init-starter-workflow#^SPEC-0038-US2|SPEC-0038.US2]] installs one coherent Agentic Engineering workflow surface with concise team-owned docs and phase-appropriate skills.
- [x] [[init-starter-workflow#^SPEC-0038-US8|SPEC-0038.US8]] converges legacy repositories on one canonical starter while preserving modified local process guidance for review.
- [x] [[init-template-architecture#^SPEC-0039-US5|SPEC-0039.US5]] enforces router/resource ownership, self-contained thin adapters, bounded closure context, coupled identity migration, and fresh-install/migration tests.
- [x] [[base-rhizome-agent-guidance#^SPEC-0080-US4|SPEC-0080.US4]] remains true: universal Rhizome mechanics stay in the core skill and workflow deliverables remain starter-owned.

## Plan

### Goal / outcome

`rzm init` exposes `agentic-engineering` as the single canonical workflow starter. New repositories receive a compact router-and-resources skill suite plus roughly 200-300 lines of team-editable engineering policy. Existing `spec-driven` repositories migrate identity, fences, state, and fingerprints together; unchanged legacy docs retire automatically and modified docs remain reviewable with an explicit manifest.

### Current-state gap and architecture decisions

The current starter conflates its specification substrate with the larger engineering workflow, distributes shared procedure across many invocation-sized skills, repeats Rhizome setup contracts, and ships 18 process documents totaling roughly 1,450 lines. Init resolution and update state key starter ownership by id, so a directory rename alone would strand managed fences, ejected state, fingerprint keys, inference, and legacy docs.

Decisions:

- `agentic-engineering` is canonical; `spec-driven` is migration input only.
- `agentic-engineering` owns workflow sequencing and lazy phase resources. Core `rhizome` owns universal retrieval, validation, mutation, documentation-binding, and structural-analysis mechanics.
- `specify`, `effort-new`, `plan`, `implement`, `effort-finish`, `foundation-review`, and `ingest-transcript` remain thin self-contained adapters. Other current starter skills are absorbed or retired as specified.
- Local process extensions are plain Markdown under `docs/engineering/`, create-only after install, and limited to purpose, defaults, commands, and team policy. Schema mechanics stay managed.
- Migration uses a checked-in historical shipped-content fingerprint catalog. Exact catalog matches can be deleted only after replacements exist; modified or unproven docs remain with a sentinel-guarded retirement notice and tracked Markdown manifest entry. No semantic auto-merge.
- Preserve `spec-driven.graphql` and `spec-driven.yaml`; update `complex-domain` dependency only.

### Phase 0 - External design review

- [x] T001 Ask Fable to pressure-test these exact specs and this plan, with emphasis on migration ordering, ownership boundaries, adapter closure, context growth, and legacy-doc safety.
- [x] T002 Reconcile every actionable Fable finding into the frozen plan/specs before implementation. Record any scope-affecting change as a deviation.

Exit: Fable review is complete and no blocking design ambiguity remains.

### Phase 1 - Migration foundation

Objective: establish the canonical starter identity and deterministic migration seam before content depends on it.

- [x] T003 Orchestrator: move the embedded starter source root to `pkg/app/cli/init/templates/starters/agentic-engineering/`, rename canonical constants/prompt labels, retain domain asset filenames, update `complex-domain/template.yaml`, and mechanically update path literals until the existing focused tests compile.
- [x] T004 [[init-starter-workflow#^SPEC-0038-US8-AC1|SPEC-0038.US8.AC1]] Add failing table-driven migration tests in new `pkg/app/cli/init/migration_test.go` and `legacy_docs_test.go` for legacy selection/ejection/dependency state, managed and ejected fence behavior, fingerprint transfer/drop/pruning, canonical and legacy inference, retired-skill cleanup, failure replay, and idempotent reruns.
- [x] T005 [[init-starter-workflow#^SPEC-0038-US8-AC2|SPEC-0038.US8.AC2]] Generate a checked-in historical sha256 catalog for every shipped version of each legacy process-doc path and test absent/uncataloged/matched/mismatched classification.
- [x] T006 [[init-template-architecture#^SPEC-0039-US5-AC4|SPEC-0039.US5.AC4]] Implement read-only planning plus idempotent filesystem actions in `pkg/app/cli/init/`, including tracked migration-manifest support, sentinel notices, closed retired-skill cleanup, managed/ejected fence handling, and path-continuous fingerprint rekeying; commit canonical workflow state before removing legacy config keys so failures replay safely.
- [x] T007 Run focused init tests, then `foundation-review` against migration ordering, failure behavior, ejected-fence preservation, and ownership state before dependent content work.

Exit: both fresh canonical input and legacy input resolve to one effective starter; repeated migration is stable; the foundation review approves the seam.

### Phase 2 - Router, resources, and adapters

- [x] T008 [[init-template-architecture#^SPEC-0039-US5-AC1|SPEC-0039.US5.AC1]] Add the lean `agentic-engineering/SKILL.md` router and self-contained resources for workflow state, specification, effort setup, planning, implementation, quality gates, alignment, reconciliation, compounding, `closure.md`, and `closure-report-contract.md`; preserve degraded sequential closure behavior without preloading every closure resource.
- [x] T009 Convert the seven retained manual entrypoints into thin self-contained adapters that route by skill name, keep trigger/deliverable/authority/argument/stop boundaries, and declare every stable extension slot targeted by a dependent starter overlay.
- [x] T010 Retarget or retire all nine complex-domain overlays with this disposition: keep `effort-new`, `implement`, `ingest-transcript`, `plan`, and `specify` on their retained adapters and preserve their slots; retire `alignment-audit` in favor of complex-domain `traceability-review`; retire `backport` in favor of `domain-backport`; retarget `compound` to a compact `agentic-engineering` compounding-route slot; fold `debugging` evidence into the retained `implement` input slot.
- [x] T011 Remove absorbed/retired starter skills and strengthen structural template tests for adapter thinness, overlay target/slot closure, bundle-local references, router reachability, retired-skill absence, and bounded closure loading. The skill worker solely owns `helper_templates_test.go`.
- [x] T012 [[init-template-architecture#^SPEC-0039-US5-AC2|SPEC-0039.US5.AC2]] Promote code-docs' reusable binding mechanics into core `rhizome/references/documentation-bindings.md` and refactor-planning's reusable evidence mechanics into `rhizome/references/structural-analysis.md`; keep closure-time documentation and refactor sequencing in starter resources.

Exit: installed skill bundles are closed, shared workflow content has one owner, and invocation context is smaller at every phase including closure.

### Phase 3 - Team extension docs and managed routes

- [x] T013 [[init-starter-workflow#^SPEC-0038-US2-AC1|SPEC-0038.US2.AC1]] Replace the legacy process scaffold with concise `docs/engineering/README.md`, `workflow.md`, `efforts.md`, `quality-gates.md`, `testing-policy.md`, and `documentation.md` totaling roughly 200-300 lines; `quality-gates.md` includes concrete repository command placeholders used by closure.
- [x] T014 Update the starter managed `AGENTS.md` block to a compact phase table and canonical local-doc/skill routes; update adjacent init guides, hubs, `choosing-your-starter.md`, release/migration notes, and `pkg/app/cli/init/CONTEXT.md` to the new ownership model and canonical dependency chain.
- [x] T015 [[init-starter-workflow#^SPEC-0038-US8-AC2|SPEC-0038.US8.AC2]] Verify migration cleanup runs only after replacement docs exist, writes tracked `.rhizome/migrations/agentic-engineering/README.md` with classification/fingerprint/follow-up evidence, and leaves modified/uncataloged docs reviewable until the team deletes the reconciled manifest.
- [x] T016 Keep ontology/query recipe files and their domain tags named `spec-driven` where accurate, while updating only starter-id references and paths.

Exit: new installs are concise and coherent; migrated installs preserve every local divergence for explicit review.

### Phase 4 - Integration, review, and delivery

- [x] T017 Refresh repository-owned generated mirrors through `rzm init`; do not hand-edit generated skill copies.
- [x] T018 Run focused init/template tests, migration tests, `./scripts/rzm validate`, `make check`, and `git diff --check`; classify any unrelated pre-existing validation finding.
- [x] T019 Run an independent code review against the combined diff, fix actionable findings, and rerun affected gates.
- [x] T020 Commit with Conventional Commits, push the existing `codex/spec-driven-skill-audit` branch, update PR #145 to describe the new scope, and mark it ready for review.
- [x] T021 Wait for the first Greptile review round, address every actionable issue from that round with focused regression coverage, push the fixes, and stop without waiting for another round.

Exit: remote PR head contains the reviewed implementation and first-round Greptile fixes, with no PR merge performed.

### Parallel ownership

Use `gpt-5.6-terra` high workers for the bulk implementation after Fable review:

- Serial orchestrator setup: source-root move, canonical constant/prompt rename, metadata dependency update, and mechanical path-literal updates before workers fork.
- Migration worker, then foundation review: `pkg/app/cli/init/**/*.go`, new `migration_test.go` / `legacy_docs_test.go`, historical fingerprint catalog, manifest generation, retired-skill cleanup, fence/state/fingerprint migration. It does not edit `helper_templates_test.go`.
- Skill worker after foundation approval: canonical starter `skills/**`, complex-domain `skill-overlays/**`, core `rhizome` references, and sole ownership of `helper_templates_test.go`.
- Docs worker in parallel with the skill worker: canonical starter `docs/engineering/**`, managed starter block, `pkg/app/cli/init/CONTEXT.md`, starter/init guides and hubs, choosing-your-starter, and release/migration notes. It edits no test files.

The orchestrator owns spec/effort truth, source-root moves, integration boundaries, foundation review, conflict resolution, mirror regeneration, verification, final review, git/PR operations, and Greptile handling. Workers receive disjoint write sets and do not edit generated mirrors.

### Validation plan

- Focused: `go test ./pkg/app/cli/init` plus any package tests touched by migration helpers.
- Template behavior: fresh install, legacy migration, idempotent rerun, generated mirror parity, bundle closure, and context-bound assertions.
- Markdown/ontology: `./scripts/rzm validate` and targeted `rzm agent validate` selectors surfaced by failures.
- Full repository gate: `make check` before commit.
- Review: independent finding-first code review, followed by first-round Greptile review and remediation.

### Open decisions / assumptions

No product decision remains open. The plan assumes the current-user identity remains unconfigured, so the explicit approval in this task is recorded below without fabricating `plan-approved-by`.

## Original Intended Delivery

Deliver the complete canonical Agentic Engineering starter, migration, router/resources suite, thin adapters, concise local extension docs, test coverage, external design review, independent code review, PR update, and first-round Greptile remediation.

## Actual Delivered

Delivered the canonical Agentic Engineering starter, migration, router/resources suite, retained adapters, six team extension documents, core Rhizome leverage resources, dependent-starter overlays, generated harness mirrors, historical fingerprint audit, and conservative legacy-document retirement to PR #145. The first Greptile round's single actionable finding is fixed with regression coverage.

## Execution Notes

- 2026-07-19T15:33Z [decision] The user explicitly approved creating the specs/effort/plan and implementing the full approved design after a second Fable review. `rzm agent current-user show` reported no configured identity, so `plan-approved-by` remains blank rather than inferring a person from chat or OS context.
- 2026-07-19T15:33Z [decision] EFF-0052 froze the prior SPEC-0038.US2 starter contract. Its deviation section now records that EFF-0065 owns the canonical identity and workflow-surface evolution while preserving EFF-0052's delivered dependency/collision behavior.
- 2026-07-19T15:33Z [validation] `frozen-scope-drift` reported one unrelated pre-existing issue for EFF-0028/SPEC-0063; no unacknowledged drift was reported for SPEC-0038 or SPEC-0039 before this effort was opened.
- 2026-07-19T15:33Z [degraded] Typed semantic prior-art survey could not reach the configured Voyage embedding endpoint. Direct typed story packs, runtime authoring context, subsystem guidance, existing specs, and exact code/doc reads established the governing contracts.
- 2026-07-19T15:44Z [review] Fable returned `ready with corrections`. Three blockers were accepted: complex-domain overlays targeted retired skills, ejected legacy fences conflicted with fence-id migration, and current registry-derived cleanup would strand retired skills. High findings also required a historical fingerprint catalog, path-aware fingerprint rekey/drop semantics, and explicit split ownership for closure orchestration/report contracts. The specs and plan now incorporate these corrections before implementation.
- 2026-07-19T16:15Z [review] The Phase 1 foundation review rejected the first migration pass. The v0.49 direct migration helper could remove legacy config keys before canonical workflow state was durable; legacy disabled-addon state could bypass the forced migration path; the historical catalog lacked mechanical integrity/provenance coverage; and retirement notices were appended too far from the document entrypoint. Phase 2 remains paused while a focused Terra-high repair pass addresses all four findings with regression tests.
- 2026-07-19T16:28Z [review] Phase 1 foundation approved after remediation. Canonical workflow state is durable before legacy config keys are removed, unrelated config patches leave workflow state untouched, failure injection is call-scoped, disabled-addon legacy state forces migration, retirement notices appear at the document entrypoint, and the 18-path/180-digest catalog has checked-in commit/blob provenance verified against full Git history. Focused migration/config/provenance tests and all `TestRun_*` tests pass with pinned Go 1.24.2; Phase 2 may begin.
- 2026-07-19T17:05Z [implementation] Terra-high workers delivered the router/resources/adapters and the six-document engineering policy surface in disjoint scopes. Integration retired nine skills, retargeted complex-domain overlays, moved reusable documentation and structural-analysis mechanics into core Rhizome guidance, and corrected preserved ontology bindings to the canonical starter source and local engineering docs.
- 2026-07-19T17:05Z [validation] The full `pkg/app/cli/init` package passes with pinned Go 1.24.2. `./scripts/rzm validate` reports zero issues after stale historical skill links and newly ambiguous SPEC-0009 links were retargeted; `git diff --check` passes.
- 2026-07-19T17:42Z [review] A second comprehensive Fable editorial review compared commit `798c4026` with the new suite using the current OpenAI and Claude prompting guides. Codex agreed that the topology improved while the first resource drafts were under-provisioned. The synthesis restored phase-specific query recipes, `frozen-scope-drift`, lifecycle immutability, finding classification, bounded closure parallelism, and the foundation-review decision protocol without restoring retired standalone skills or duplicated setup contracts. Codex adjusted Fable's drafts to preserve requirements-only efforts and the established planning retrieval sequence.
- 2026-07-19T17:42Z [validation] Added reverse-reachability coverage for all eight phase-critical recipes plus `frozen-scope-drift`, updated the durable query-bundle phase map, and corrected stale complex-domain/public routes. The six team extension docs remain 201 lines total; router resources remain below the existing 120-line aggregate budget.
- 2026-07-19T17:48Z [implementation] Built the current CLI with pinned Go 1.24.2 and ran `rzm init` against this repository. It resolved `complex-domain` through `agentic-engineering`, refreshed tracked `.agents` and `.claude` mirrors, removed retired managed skills, created the six local engineering docs, and persisted canonical source fingerprints without triggering legacy ProcessSpec retirement.
- 2026-07-19T17:35Z [validation] Focused `pkg/app/cli/init` and `pkg/ontology` tests pass. `make check` passes outside the filesystem sandbox, including race-enabled Go tests, integration packages, web lint/tests/typechecks, and `greptile-check`; the sandbox-only attempt failed where tests bind loopback listeners. `./scripts/rzm validate` reports zero issues, `git diff --check` passes, and a second `rzm init` run made no generated-surface updates.
- 2026-07-19T17:35Z [validation] `rzm validate frozen-scope-drift` reports only the previously classified EFF-0028/SPEC-0063 issue; it reports no drift for this effort's frozen SPEC-0038/SPEC-0039 scope.
- 2026-07-19T18:34Z [review] Sol's final migration review found two deletion-safety gaps: retained legacy documents did not preserve exact-shipped dependencies transitively, and Markdown reference-definition destinations were invisible to the inline structured-link scanner. Both are fixed with focused regressions. A separate orchestrator review found that deleted exact-template records disappeared from the migration manifest on replay; those audit records now persist until the team explicitly removes the manifest.
- 2026-07-19T18:34Z [validation] Built the final CLI and replayed this repository's migration twice. The manifest and README hashes are byte-stable across reruns; four referenced exact specs remain archived, five unreferenced exact templates are deleted with persistent audit records, and nine modified documents remain archived for reconciliation. `./scripts/rzm validate` reports zero issues and `git diff --check` passes.
- 2026-07-19T18:34Z [validation] Focused init, ontology, Obsidian, CLI, historical-provenance, and 23 installer integration tests pass. The first final `make check` attempt hit one unrelated `pkg/app/indexing` concurrency timeout; the exact race-enabled test then passed 20 consecutive runs and the complete `make check` rerun passed, including race-enabled Go tests, integration packages, web lint/tests/typechecks, and `greptile-check`.
- 2026-07-19T18:39Z [review] The follow-up Sol confirmation found two remaining testable edges: CommonMark reference definitions may place the destination on the next line, and transitive retention needed an explicit multi-hop fixture. The protected reference-definition parser now handles the one-line continuation, the migration test proves `local policy -> exact A -> exact B`, and Sol reported no other actionable defects in the broader starter diff.
- 2026-07-19T18:39Z [validation] The final `make check` passes after the last Sol-requested fixes, including race-enabled Go tests, integration packages, web lint/tests/typechecks, and `greptile-check`; focused init, ontology, and Obsidian tests also pass.
- 2026-07-19T18:51Z [delivery] Commit `527579c6` was pushed to PR #145 after confirming `origin/main` was already merged. The PR remained open and ready for review.
- 2026-07-19T18:51Z [review] The first Greptile bypass review completed on `527579c6` at 4/5 confidence and identified one actionable P1: v0.49 split-workflow detection read `layout.ExistingLocal` before it was assigned, making the `management` migration path unreachable. A red-green regression now proves detection from the parsed config, and the implementation reads `cfg.Rhizome.Version` directly. Per the approved delivery boundary, no second Greptile round is requested.
- 2026-07-19T18:51Z [validation] `go test ./pkg/app/cli/init` and the complete `make check` pass after the Greptile fix, including race-enabled Go tests, integration packages, web lint/tests/typechecks, and `greptile-check`.

## Deviations

- 2026-07-19T15:44Z [decision] The frozen plan/specs were refined before implementation from the requested Fable review. The corrections narrow unsafe behavior and preserve dependent-starter/closure capabilities without changing the approved product direction: tracked Markdown manifest, historical fingerprint catalog, byte-identical ejected fences, closed retired-skill cleanup, complex-domain overlay dispositions, atomic final workflow-state rewrite, and serial source-root/migration integration order.
- 2026-07-19T17:35Z [review] Sol review found that adjacent active SPEC-0007, SPEC-0062, and SPEC-0063 still required the retired skill topology. Their routing and overlay contracts were reconciled to the approved Agentic Engineering architecture; EFF-0028 records the corresponding frozen-scope deviation while this effort owns the successor behavior.

## Closure Checklist

- [x] Focused tests and `make check` pass.
- [x] Rhizome validation and frozen-scope alignment are complete or classified.
- [x] Specs, docs, generated mirrors, and actual delivery are aligned.
- [x] Independent code review and first-round Greptile findings are addressed.
- [x] Remote PR head contains all implementation and review fixes.

## Compounding Follow-ups

None yet.

## Status

Complete. Specification, two Fable reviews, migration foundation and real replay, router/resources, adapters, overlays, team extension docs, generated mirrors, broad gates, two Sol review rounds, PR delivery, and first-round Greptile remediation are complete. PR #145 remains open and unmerged.
