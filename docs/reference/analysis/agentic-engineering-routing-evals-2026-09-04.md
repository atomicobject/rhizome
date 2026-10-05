---
summary: "Routing evaluation of the agentic-engineering starter on Claude Fable 5.1 and GPT-6 Astra, the Astra self-audit of the instruction surface, and the prunes they drove."
reference-kind: analysis
last-verified: 2026-09-04
code-paths:
  - pkg/app/cli/init/templates/starters/agentic-engineering/skills
  - pkg/app/cli/init/templates/starters/agentic-engineering/docs/engineering
---

# Agentic Engineering routing evals (2026-09-04)

Evidence for [[../../efforts/2026-09-04-17-52-engineering-concern-docs|EFF-2026-09-04-17-52]] task T006. Fifteen scenario prompts were sent to both models inside this repository with the installed skills, managed blocks, and concern docs loaded. Each model answered read-only with the route it would take, the documents it would consult, whether it would stop before working and why, whether it would write tests, and whether it would escalate to an effort. This measures the instruction surface, not full task execution.

Runners: `scripts/claude-fable` (Claude Code, plan mode, model `fable`) and `codex exec -s read-only` (default model `gpt-6-astra`, medium reasoning). Scenario prompts and raw answers are in the session evidence; the table below is the parsed result.

## Results

| Scenario | Model | Route | Stops before work | Tests | Escalates | Docs consulted |
| --- | --- | --- | --- | --- | --- | --- |
| S01-typo | astra | agentic-engineering implement | no | no | no | README.md, testing-policy.md, quality-gates.md, documentation.md, review-and-approval.md |
| S01-typo | fable | agentic-engineering implement | yes: README.md line 12 does not contain 'recieve' and grep finds no occurrence anywhere in README.md, so the requested edit has no target; I woul | no | no | README.md, review-and-approval.md, quality-gates.md, documentation.md |
| S02-bugfix | astra | agentic-engineering implement, composed with rhizome | no | yes | no | README.md, testing-policy.md, quality-gates.md, review-and-approval.md, architecture.md, documentation.md, release.md |
| S02-bugfix | fable | agentic-engineering implement (composed with vault-core-subsystem skill and psta | no | yes | no | README.md, testing-policy.md, quality-gates.md, review-and-approval.md |
| S03-ambiguous-feature | astra | agentic-engineering classify → specify → effort → plan; compose with rhizome | yes: This evaluation prohibits implementation; the normal route would prepare a concrete plan and obtain explicit approval before implementation, | yes | yes | README.md, review-and-approval.md, architecture.md, testing-policy.md, quality-gates.md, documentation.md, release.md |
| S03-ambiguous-feature | fable | agentic-engineering classify → agentic-engineering specify (then effort, plan; i | yes: Snooze changes the ActionItem ontology SDL and the action-items init starter templates (managed block, query recipes, views, skill), which d | yes | yes | README.md, review-and-approval.md, testing-policy.md, quality-gates.md, architecture.md |
| S04-braindump | astra | agentic-engineering specify (strawman) | no | no | no | README.md, review-and-approval.md, documentation.md, testing-policy.md, quality-gates.md, architecture.md |
| S04-braindump | fable | agentic-engineering specify (after the classify reference routes rough input to  | yes: The ask carries material uncertainty on a product choice the human owns: what 'stale spec' means observably and which surface (report, view, | no | yes | README.md, review-and-approval.md, testing-policy.md, architecture.md |
| S05-open-effort | astra | agentic-engineering effort, composed with rhizome | yes: The evaluation prohibits execution; in normal execution, effort-setup.md treats US2 as candidate scope and requires human approval before fr | no | yes | README.md, review-and-approval.md, testing-policy.md, documentation.md, quality-gates.md |
| S05-open-effort | fable | agentic-engineering effort (docs/specs/product/init-starter-workflow.md story SPEC-0038 US2 | yes: Per effort-setup.md the US2 scope argument is a candidate slice, not approved frozen scope: I would draft the effort note freezing links to  | no | yes | README.md, review-and-approval.md, documentation.md |
| S06-plan | astra | agentic-engineering plan, composed with rhizome | no | no | no | README.md, architecture.md, testing-policy.md, documentation.md, review-and-approval.md, release.md, quality-gates.md |
| S06-plan | fable | agentic-engineering plan (references/planning.md) | yes: The effort already carries an approved plan (plan-approved-by Drew Colthorp, T001-T005 done, T006-T007 open); rewriting it would overwrite a | no | no | README.md, review-and-approval.md, architecture.md, testing-policy.md, documentation.md |
| S07-implement-approved | astra | agentic-engineering implement | no | yes | no | README.md, testing-policy.md, quality-gates.md, review-and-approval.md, architecture.md, documentation.md, release.md |
| S07-implement-approved | fable | agentic-engineering implement (references/implementation.md), given the effort p | yes: The effort note already marks T004 [x] complete with an implementation execution note dated 2026-09-04T23:10Z saying refresh safety is in pl | yes | no | README.md, testing-policy.md, quality-gates.md, architecture.md |
| S08-finish | astra | agentic-engineering finish | no | no | no | README.md, quality-gates.md, review-and-approval.md, documentation.md, testing-policy.md, release.md |
| S08-finish | fable | agentic-engineering finish (references/closure.md, then closure-report-contract. | no | no | no | README.md, quality-gates.md, review-and-approval.md, documentation.md, release.md |
| S09-transcript | astra | ingest-transcript, composed with rhizome; source survey and synthesis | no | no | no | README.md, review-and-approval.md, documentation.md, testing-policy.md, quality-gates.md |
| S09-transcript | fable | ingest-transcript (single-phase skill, argument docs/meetings/2026-09-03-acme-in | yes: The attached transcript does not exist at docs/meetings/2026-09-03-acme-interview.md (only a private transcript is present), so the so | no | no | README.md, documentation.md, review-and-approval.md |
| S10-disguised-cross-cutting | astra | agentic-engineering classify → specify → effort → plan | yes: The evaluation prohibits implementation; for the actual task, review-and-approval.md requires an approved effort plan before changing a dura | yes | yes | README.md, review-and-approval.md, testing-policy.md, quality-gates.md, architecture.md, documentation.md |
| S10-disguised-cross-cutting | fable | agentic-engineering classify -> escalate: agentic-engineering specify, then effo | yes: review-and-approval.md classifies ontology SDL, init template, and persisted-identifier changes as always effort-driven; this touches the sp | yes | yes | README.md, review-and-approval.md, testing-policy.md, quality-gates.md |
| S11-testing-question | astra | agentic-engineering classify + stores-subsystem | yes: This request permits only routing evaluation and guidance reads; implementation is not authorized. | yes | no | README.md, testing-policy.md, quality-gates.md, review-and-approval.md, architecture.md |
| S11-testing-question | fable | agentic-engineering classify (question about testing strategy, not delivery yet) | yes: The request is a question about which tests to write and at what layer, not an authorization to implement; the deliverable is an assessment  | no | no | testing-policy.md, quality-gates.md |
| S12-foundation | astra | foundation-review, with agentic-engineering classify for workflow state | no | no | no | README.md, review-and-approval.md, testing-policy.md, architecture.md |
| S12-foundation | fable | foundation-review | yes: The effort note has status complete and already records the Phase 1 foundation review as rejected at 16:15Z then approved at 16:28Z, with Ph | no | yes | README.md, review-and-approval.md, quality-gates.md |
| S13-gates | astra | agentic-engineering gates | no | no | no | README.md, quality-gates.md, testing-policy.md, review-and-approval.md |
| S13-gates | fable | agentic-engineering gates (loads .agents/skills/agentic-engineering/references/q | no | no | no | README.md, quality-gates.md, release.md (only if a gate is a release condition) |
| S14-flag | astra | agentic-engineering implement, with rhizome and cli-subsystem guidance | no | yes | no | README.md, testing-policy.md, quality-gates.md, review-and-approval.md, documentation.md |
| S14-flag | fable | agentic-engineering implement (composed with cli-subsystem for cmd/index.go; pot | no | yes | no | README.md, testing-policy.md, quality-gates.md, review-and-approval.md, documentation.md |
| S15-stale-skill | astra | agentic-engineering classify → specify → effort → plan | yes: Release tooling is always effort-driven under docs/engineering/review-and-approval.md; prepare a concrete plan and obtain explicit approval  | yes | yes | README.md, review-and-approval.md, testing-policy.md, quality-gates.md, architecture.md, documentation.md, release.md |
| S15-stale-skill | fable | agentic-engineering (classify) → agentic-engineering effort + plan before implem | yes: The requested 'development-loop' skill is not installed, and docs/engineering/review-and-approval.md classifies release tooling as always ef | yes | yes | README.md, review-and-approval.md, testing-policy.md, quality-gates.md, release.md |

## Reading the results

- **Routing is correct on both models.** Every scenario landed on the intended router phase or workflow skill, including the retired `development-loop` request, which both models rerouted to the router instead of following the stale name.
- **Fable's stops were all premise failures, not hesitation.** It stopped because the typo did not exist, the transcript file was missing, T004 was already checked off, the target effort was closed, or an approved plan already existed. That is the judgment the guidance asks for.
- **Astra's stops were mostly an artifact of the eval prompt** ("do not perform the task"), which it reported as the reason. Two were real and drove edits below: it would re-plan an effort that already has an approved plan without asking (S06), and it escalated a retry-in-a-script change to a spec and effort because this repository's policy said release tooling is always effort-driven (S15).
- **Both models read more concern docs than the step needed.** Astra listed all seven in several scenarios. The docs total under 150 lines, so the cost is small, but the index now says to read the document for the concern at hand.
- **Test flags on specify-phase scenarios (S03, S10) describe eventual implementation** and are not a finding.

## Prunes applied from the evals

1. `references/planning.md`: revise an already-approved plan only with the user's confirmation, recorded as a deviation.
2. `docs/engineering/README.md` (template and repo): read the concern document at hand, not the whole set.
3. This repository's `review-and-approval.md`: effort-driven work is classified by effect (invariants, schema semantics, persisted data, generated behavior, what a release publishes), and mechanical edits that preserve those contracts stay local tasks.

## Astra self-audit

GPT-6 Astra audited AGENTS.md, the three skills, their references, and the concern docs against OpenAI's guidance. Applied from its findings:

- Narrowed the plan-authorization boundary in the router, managed block, and review-and-approval doc: resolve routine implementation choices; stop only for a material decision outside that authority.
- Shortened the three skill descriptions.
- Replaced the closure `DEGRADED` itinerary with "complete closure directly when delegation is unavailable or unnecessary".
- Removed the stale "main skill's partnership decision loop" reference from the strawman flow.
- Bug fixes reproduce first; competing hypotheses only when the reproduction does not prove the cause.
- Reconciliation and compounding ask only for decisions that have not already been made.
- Foundation review holds only edits that depend on an unresolved decision.
- Ingest-transcript treats an explicit request to save context as authorization for those writes.
- Moved the complex-domain compounding overlay slot from the router body into the compounding reference.
- Aligned this repository's gates with the unmanaged AGENTS.md rules: `make check` before committing Go or TypeScript changes, and a regression test for every bug fix.

Not applied, because they change the unmanaged, user-authored part of `AGENTS.md`; recommended for the owner:

- "Before editing a subsystem folder, load its note or skill" applies to mechanical edits too. Astra suggests loading the note when changing behavior, contracts, or invariants, or when ownership is unclear.
- "When explaining something to the user, use the Visualize skill" has no task boundary.
- Testing, formatting, file-size, compatibility, and staging rules now appear both in AGENTS.md and in the concern docs. Keep one owner per rule; the concern docs are the sanctioned place for policy that skills read.
- Delegation is framed as user-requested in AGENTS.md but commanded for review in the same file.

## Limitations

Routing answers are self-reports under a read-only instruction, not observed executions. Astra's stop answers are contaminated by the eval instruction itself. A follow-up eval should run a small set of scenarios end to end in a scratch repository and score observed behavior.
