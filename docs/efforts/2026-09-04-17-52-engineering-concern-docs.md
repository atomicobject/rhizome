---
type: EffortNote
id: EFF-2026-09-04-17-52
name: Engineering concern docs and adapter inlining
created-at: 2026-09-04T21:52:32Z
plan-approved-by: Drew Colthorp
status: complete
summary: "Refactor the agentic-engineering starter's team-editable docs into concern-shaped notes consulted from several phases, collapse phase adapters into router arguments, make stops decision boundaries, protect team edits from doc refresh, and reconcile this repository's legacy process specs."
aliases:
  - EFF-2026-09-04-17-52
---

# Engineering concern docs and adapter inlining

## Scope

Land on PR #145 before merge. Replace the phase-shaped `docs/engineering/` set with concern-shaped documents that teams edit to tune skill behavior, wire every phase to the concern documents it needs, state precedence in managed guidance, rewrite stop sections as decision boundaries, collapse the five phase adapters into router arguments so no phase needs a second skill hop, make requested doc refresh skip locally edited files, and reconcile this repository's 13 retained legacy ProcessSpecs.

Out of scope: typed `EngineeringGuideline` nodes and a discovery recipe (fixed filenames only, per decision), render-time embed machinery, changes to complex-domain or project-kb starters beyond overlay retargeting, and merging PR #145.

## Spec Set (Frozen)

Frozen on 2026-09-04 at 2026-09-04T21:52Z.

- [[../specs/product/init-starter-workflow|SPEC-0038]] (`last-updated: 2026-09-04`)
- [[../specs/technical/init-template-architecture|SPEC-0039]] (`last-updated: 2026-09-04`)
- [[../specs/product/base-rhizome-agent-guidance|SPEC-0080]] (`last-updated: 2026-07-18`; universal-mechanics ownership constraint)

## Stories In Scope (Frozen)

- [[../specs/product/init-starter-workflow#^SPEC-0038-US2|SPEC-0038.US2]]
  - [[../specs/product/init-starter-workflow#^SPEC-0038-US2-AC1|SPEC-0038.US2.AC1]]
  - [[../specs/product/init-starter-workflow#^SPEC-0038-US2-AC3|SPEC-0038.US2.AC3]]
- [[../specs/technical/init-template-architecture#^SPEC-0039-US5|SPEC-0039.US5]]
  - [[../specs/technical/init-template-architecture#^SPEC-0039-US5-AC2|SPEC-0039.US5.AC2]]
  - [[../specs/technical/init-template-architecture#^SPEC-0039-US5-AC3|SPEC-0039.US5.AC3]]

## Spec Coverage Checklist

- [ ] [[../specs/product/init-starter-workflow#^SPEC-0038-US2-AC1|SPEC-0038.US2.AC1]] concern-factored `docs/engineering/` set installs from the starter tree.
- [ ] [[../specs/product/init-starter-workflow#^SPEC-0038-US2-AC3|SPEC-0038.US2.AC3]] adapters stay thin and self-contained with embedded phase text.
- [ ] [[../specs/technical/init-template-architecture#^SPEC-0039-US5-AC2|SPEC-0039.US5.AC2]] managed guidance stays a compact route table with an explicit precedence line.
- [ ] [[../specs/technical/init-template-architecture#^SPEC-0039-US5-AC3|SPEC-0039.US5.AC3]] concern docs are small, state defaults as editable sentences, name their readers, and survive doc refresh.

## Plan

Decisions by Drew Colthorp on 2026-09-04: land on PR #145; six concern docs; fixed filenames; keep this repository's retained legacy ProcessSpecs as archived history; subtract the phase adapters rather than inline into them; consult concern docs contextually rather than as pre-reading; foundation pause is opt-in per plan.

- [x] T001 Amend SPEC-0038.US2.AC1, SPEC-0038.US2.AC3, and SPEC-0039 (extension docs by concern, router-by-argument, refresh safety); record the deviation on active EFF-2026-05-06-12-15.
- [x] T002 Author the concern docs in the starter template: `README.md` index (concern, file, phases that consult it), `testing-policy.md` (layers and what to test with what), `quality-gates.md` (commands plus a "safe to run without asking" section), `documentation.md`, `review-and-approval.md` (risk classes, approvers, when a pause is wanted), `architecture.md`, `release.md`. Each under 40 lines, defaults as editable sentences, a "Consulted by" line, and a team extension section. Remove `workflow.md` and `efforts.md`; fold routing and escalation into the workflow-state and effort-setup references and their policy sentences into `review-and-approval.md`.
- [x] T003 Router by argument: retire the `specify`, `effort-new`, `plan`, `implement`, and `effort-finish` skills; move their deliverable and decision-boundary text into the matching references; keep `specify`'s strawman and spec-template references and `implement`'s traceability reference inside the router bundle; retarget complex-domain overlay slots to the router; add the retired names to `retiredStarterSkillNames` so migration cleans them. Router `argument-hint: [phase] [path]`, one-sentence description. Phase text points to two or three concern docs contextually ("when deciding coverage, consult..."). Replace `## Stop` with `## Decision boundaries`, add "complete all authorized work before stopping", and make foundation review a plan-requested exit rather than a standing rule. Managed AGENTS block: clear-local-task row first, precedence line (user request, then `docs/engineering/`, then skill defaults), and "when a skill makes you stop, name the skill file". Update tests, hubs, guides, and CHANGELOG.
- [x] T004 Refresh safety: `--refresh-template-docs` skips any doc whose on-disk hash matches no shipped fingerprint unless confirmed interactively; batch mode reports skipped files. Regression test using an edited `docs/engineering/testing-policy.md`.
- [x] T005 Reconcile this repository: rewrite `docs/engineering/` to the concern set with real gates (`make check`, `make web-e2e`, `./scripts/rzm validate`, `rzm ci`) and policy folded from the retained ProcessSpecs; keep the 13 retained ProcessSpecs as archived historical records with a "historical record, not guidance" notice; mark migration manifest entries reconciled; update the notice text in `migration.go` for downstream installs; regenerate mirrors.
- [x] T006 Behavioral evals: three scenarios per remaining skill and per router phase, run through `scripts/claude-fable` and `codex exec`, scoring route chosen, unnecessary stops, unrequested tests; plus an Astra self-audit of AGENTS.md and the three skills. Record results under `docs/reference/analysis/` and prune from evidence.
- [x] T007 Gates and delivery: `go test ./pkg/app/cli/init/...`, `make check`, `./scripts/rzm validate`, `rzm init --yes` idempotent, push to `origin/codex/spec-driven-skill-audit`, PR comment.

## Original Intended Delivery

Concern-factored team docs consulted from several phases, a router that takes the phase as an argument with only two separate workflow skills, safe doc refresh, explicit precedence, decision-boundary stop sections, a reconciled repository, and scenario evidence for Fable 5.1 and GPT-6 Astra, all on PR #145.

## Actual Delivered

Delivered on PR #145 as planned, with the adapter subtraction approved mid-plan. The starter ships three skills (router with phase arguments, `foundation-review`, `ingest-transcript`) and a concern-factored `docs/engineering/` set that phases consult contextually and that outranks skill defaults. Overlay fragments can target reference files, retired starter skills are cleaned up on rerun, and a requested doc refresh preserves team edits. This repository's concern docs carry its real gates and policy, and its retained legacy ProcessSpecs are marked as historical records. Fifteen routing scenarios on Fable 5.1 and GPT-6 Astra plus an Astra self-audit drove the recorded prunes; AGENTS.md changes were left as owner recommendations.

## Execution Notes

- 2026-09-04T22:30:00Z [decision] Drew Colthorp approved the revised plan in chat after the GPT-6 Astra skills guidance was applied: subtract the five phase adapters instead of embedding phase text, consult concern docs contextually, and make the foundation pause opt-in.

- 2026-09-04T21:52:32Z [decision] Drew Colthorp chose: land on PR #145 rather than a new PR; six concern docs; fixed filenames only; fold and delete the retained legacy ProcessSpecs.
- 2026-09-04T21:52:32Z [surprise] The retained ProcessSpecs are wikilinked as frozen spec ids from roughly 40 closed efforts (for example `[[../specs/process/effort-lifecycle|SPEC-0002]]`). Deleting them would break historical links and fail validation, and closed efforts must not be rewritten. T006 therefore keeps them as archived historical records with a not-guidance notice instead of deleting them. This deviates from the "fold and delete" decision and needs confirmation with plan approval.
- 2026-09-04T21:52:32Z [learning] `expandAgentTemplateEmbeds` resolves Obsidian-style embeds only against RHIZOME.md template chunks, so adapter inlining needs a starter-scoped embed source (T004) rather than reuse as-is.
- 2026-09-04T21:52:32Z [learning] `rzm init --refresh-template-docs` overwrote locally edited guide docs during the main reconciliation earlier today; T005 is grounded in that observation.

- 2026-09-04T23:10:00Z [implementation] Concern docs, router-by-argument, overlay file targeting, refresh safety, and this repository's reconciliation are in place. Focused init tests pass; `./scripts/rzm validate` reports zero issues; `rzm init --yes` is idempotent after the mirrors and managed blocks were regenerated.
- 2026-09-04T23:10:00Z [surprise] Retired starter skills were never removed on rerun because the merged cleanup only recognized currently shipped starter skill names; fixed with a regression test. The five retired adapters and the earlier nine now clean up once the router is accepted on disk.
- 2026-09-04T23:10:00Z [decision] The two retained ProcessSpecs main still treats as active (`effort-lifecycle`, `id-allocation`) keep their active frontmatter; the other eleven carry the "historical record, not guidance" notice.

- 2026-09-04T23:55:00Z [validation] Fifteen routing scenarios ran on Fable 5.1 and GPT-6 Astra; both routed every scenario correctly. Findings and the Astra self-audit are recorded in [[../reference/analysis/agentic-engineering-routing-evals-2026-09-04|the routing evals analysis]]; the prunes they drove are applied to the starter and to this repository's concern docs, with AGENTS.md changes left as recommendations for the owner.

## Deviations

- 2026-09-04T21:52:32Z [decision] Retained legacy ProcessSpecs will be archived as historical records rather than deleted; see Execution Notes. Confirmed with plan approval.

## Closure Checklist

- [x] Focused init tests and `make check` pass (final gate 2026-09-05, exit 0).
- [x] `./scripts/rzm validate` and `validate frozen-scope-drift` report zero issues.
- [x] Specs, concern docs, generated mirrors, and actual delivery align; `rzm init --yes` is idempotent.
- [x] Eval results recorded with the prune decisions they drove.
- [x] PR #145 head contains the work (pushed to `origin/codex/spec-driven-skill-audit`).

## Compounding Follow-ups

- Typed `EngineeringGuideline` nodes with an activity recipe, if teams want to add concerns without touching managed skills.
- Post-closure 2026-09-05: the remaining shipped skills (core `rhizome`, complex-domain, project-kb, engagement skills) still have the pre-refactor shape, three skills ingest sources, and the retired review-feedback job has no owner. Handed off as [[2026-09-05-11-42-starter-skill-consolidation|EFF-2026-09-05-11-42]] with the inventory and mechanisms. The eval runner used for T006 is committed at `scripts/routing-evals/`.

## Status

Complete. PR #145 remains open for review; the AGENTS.md recommendations from the Astra audit are the owner's call.
