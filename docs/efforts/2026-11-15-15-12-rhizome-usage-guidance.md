---
type: EffortNote
id: EFF-2026-11-15-15-12
name: Rhizome usage guidance
created-at: 2026-11-15T20:12:34Z
status: active
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
- [ ] AC3: `fit` and `cautions` on starters and workflow choices, emitted by `rzm init --check --json`; optional add-ons in first-run plans and on the setup sheet, unselected.
- [x] AC4: request-scoped authority in `references/setup-and-starters.md`.
- [x] AC5: customization and automation references.
- [ ] AC6: desktop chat instructions route to the `rhizome` skill with the shared-directory fallback.
- [ ] AC7: starter-id guard and `rzm` command-literal tests.

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

In progress.

## Deviations

None yet.

## Execution Notes

- 2026-11-15: Six Claude Opus 5.5 research agents surveyed the app, customization, starters, the shipped skills, and a month of Claude Code and Codex history. A council of Claude Fable 5.2 and GPT-6 Astra chose references in `rhizome` over a sibling skill, and starter metadata over a SPEC-0080 amendment.

## Compounding Follow-ups

- Routing evaluations on Claude Code and Codex, standalone and in-app, with negatives for ordinary change work (needs separate approval under SPEC-0080 US6).
- A live check that a Claude chat in a repository set up with `--agents shared` follows the fallback, and that a Codex chat can reach the runtime.

## Closure Checklist

- [ ] Approved scope delivered or deviations recorded.
- [ ] Independent review completed and accepted findings fixed.
- [ ] Delivery branch and commit recorded.

## Status

Active on `t3/plan-rhizome-usage-skill`.
