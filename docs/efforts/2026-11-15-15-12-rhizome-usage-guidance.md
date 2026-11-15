---
type: EffortNote
id: EFF-2026-11-15-15-12
name: Rhizome usage guidance
created-at: 2026-11-15T20:12:34Z
status: complete
summary: Teach agents in the desktop chat or a standalone session to explain Rhizome, recommend and install starters from live catalog data, and turn goals into views, schema changes, or automation.
aliases: [EFF-2026-11-15-15-12]
plan-approved-by: Drew Colthorp
governing-specs:
  - "[[base-rhizome-agent-guidance]]"
---

# Rhizome usage guidance

## Scope

Deliver [[base-rhizome-agent-guidance#^SPEC-0080-US8|SPEC-0080 US8]]. The research and the design council (Claude Fable 5.2 and GPT-6 Astra) are recorded in `docs/reference/analysis/rhizome-usage-skill/`, with the decided design in `Council outcome.md`.

Out of scope: running routing evaluations on Claude Code and Codex. SPEC-0080 US6 makes model runs manual and separately approved, so they are a follow-up.

## Spec Set (Frozen)

- [[base-rhizome-agent-guidance|SPEC-0080]], revision dated 2026-11-15, US8 and the Requirements changes that name the four new references and live starter catalog data.

## Stories In Scope (Frozen)

- [[base-rhizome-agent-guidance#^SPEC-0080-US8]]

## Spec Coverage Checklist

- [x] AC1: routing rows and description for orientation, setup and starters, customization, and automation.
- [x] AC2: in-app orientation in `references/usage.md`.
- [x] AC3: `fit` and `cautions` on starters and workflow choices, emitted by `rzm init --check --json`; optional add-ons in first-run plans and on the setup sheet, unselected.
- [x] AC4: request-scoped authority in `references/setup-and-starters.md`.
- [x] AC5: customization and automation references.
- [x] AC6: desktop chat instructions route to the `rhizome` skill with the shared-directory fallback.
- [x] AC7: starter-id guard and `rzm` command-literal tests.

## Plan

1. Done on this branch: research notes (`02549aec2`) and the four references with the router changes (`9b1e756d4`), drafted by Claude Fable 5.2 and reviewed in thread.
2. Init catalog (Claude Opus 5.5, isolated worktree): `fit` and `cautions` in `template.yaml` and on the workflow choices, emitted on `WorkflowOption` and `AddonOption`; first-run plans list optional add-ons through `allAddonOptions`, unselected; the desktop setup sheet shows them unselected; tests.
3. Chat and guards (Claude Opus 5.5, isolated worktree): one routing sentence with the `.agents/skills/rhizome/SKILL.md` fallback in `chatInstructions`; `remark-gfm` in the chat timeline; the starter-id guard and the command-literal test; the `rzm agent next-id` prefix in the agentic-engineering effort-setup reference; AO KB in the human starter guides.
4. Merge both, add the changelog line, regenerate this repository's generated skill copies if a check requires it, run the required gates, get an independent review, run Greptile, close this effort, and open the pull request for Drew to merge.

## Plan Approval

- plan-approved-by:: Drew Colthorp, 2026-11-15, in chat: "Yeah, that all sounds good. Can you do that stuff?", in reply to the remaining-work list (starter metadata, optional add-ons on first run, the chat prompt sentence, the guard and command-literal tests, `remark-gfm`, the `next-id` prefix, AO KB in the guides, and the SPEC-0080 story). Approved scope: SPEC-0080 US8 and this plan, delivered in one pull request left for Drew to merge.

## Original Intended Delivery

A person who asks the app's chat or a standalone agent "what is Rhizome", "which starter fits", "add a pipeline board", or "check stalled deals every week" gets an accurate answer grounded in what this repository has, a recommendation drawn from live starter data, and a safe path to apply it.

## Actual Delivered

All seven US8 criteria are delivered on `t3/plan-rhizome-usage-skill`.

- AC1, AC2, AC4, AC5: the `rhizome` skill's description and router gain four routes, to `references/usage.md`, `setup-and-starters.md`, `customization.md`, and `automation.md` (`9b1e756d4`, refined in `03d18114b`, `528cc4822`, `85b41cb9e`). `ontology-authoring.md` now places schemas in module folders, and `code-mode.md` uses a neutral example.
- AC3: every starter's `template.yaml` carries `fit` and `cautions`; `rzm init --check --json` emits them with `types` (built at run time from each starter's embedded ontology, required starters included) and `optional` on workflow and add-on entries, in first-run and configured plans. First-run plans list optional add-ons unselected through `allAddonOptions`, and Rhizome Desktop's setup sheet shows them unchecked and keeps a chosen one across workflow changes (`3d4f3be17`, `528cc4822`, `e74966418`).
- AC6: `chatInstructions` routes questions about Rhizome to the `rhizome` skill and falls back to `.agents/skills/rhizome/SKILL.md`; the chat timeline renders GFM tables that scroll inside the pane.
- AC7: a starter-name guard in `helper_templates_test.go`, and `cmd/skill_command_literals_test.go`, which checks every `rzm` invocation in shipped guidance (chains, redirects, and shipped module actions and inputs) against the real command tree, with negative cases (`94c659146`, `942ab6ef0`, `29c0d0a77`).
- Small fixes: the `rzm agent next-id` prefix in the agentic-engineering effort-setup reference; AO KB and the `--addons` exact-set rule in README, getting-started, and choosing-your-starter; this repository's generated skills regenerated (`1c20eaa0f` and later).

Verification: `make check` (Go unit tests in 160 packages, web unit tests 273 files and 2,244 tests) passed at `528cc4822` and again at `85b41cb9e`; `rzm init --check` exits 0 after regeneration; `rzm validate` and `rzm validate frozen-scope-drift` report 0 issues; desktop `npm run check` (typecheck, lint, 203 tests) passed. GPT-6.1 Sol review: 8/10 at `1c20eaa0f` (three findings, fixed), 9/10 with none remaining at `528cc4822`. Greptile: 4/5 with three findings (fixed), then 5/5 with no comments at `85b41cb9e`.

## Deviations

- Added a `types` field to workflow and add-on entries, derived from embedded ontologies, after review found that type names in `cautions` prose were incomplete for the collision check; cautions no longer list types.
- Updated SPEC-0118 and SPEC-0038 US12-AC1, which said the setup sheet hides optional add-ons.
- Extracted `moduleCommandTree` from `runModuleCommand` in `cmd/module.go` so the command-literal test checks shipped module commands against the real tree; execution behavior is unchanged.
- Fixed a setup-sheet bug that offering optional add-ons exposed: changing the workflow dropped a chosen optional add-on.
- The Agent page timeline still renders without GFM tables; left as a follow-up to keep scope to the chat pane.

## Execution Notes

- 2026-11-15: Six Claude Opus 5.5 research agents surveyed the app, customization, starters, the shipped skills, and a month of Claude Code and Codex history. A council of Claude Fable 5.2 and GPT-6 Astra chose references in `rhizome` over a sibling skill, and starter metadata over a SPEC-0080 amendment.

## Compounding Follow-ups

- GFM tables on the Agent page timeline (`web/src/components/agent/AgentTimeline.tsx`).
- The command-literal check reads an attached `<file` followed later by `>` as a placeholder.
- Routing evaluations on Claude Code and Codex, standalone and in-app, with negatives for ordinary change work (needs separate approval under SPEC-0080 US6).
- A live check that a Claude chat in a repository set up with `--agents shared` follows the fallback, and that a Codex chat can reach the runtime.

## Closure Checklist

- [x] Approved scope delivered or deviations recorded.
- [x] Independent review completed and accepted findings fixed.
- [x] Delivery branch and commit recorded.

## Status

Complete on `t3/plan-rhizome-usage-skill` for [PR #152](https://github.com/atomicobject/rhizome/pull/152), left open and unmerged for Drew. Routing evaluations remain a follow-up that needs separate approval.
