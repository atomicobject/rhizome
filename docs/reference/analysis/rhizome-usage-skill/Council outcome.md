---
type: ReferenceDoc
summary: "Outcome of the 2026-11-15 design council (Claude Fable 5.2 and GPT-6 Astra) on Rhizome usage guidance: where both agreed, where they split, the corrections they made to the research, and the recommended design."
reference-kind: analysis
status: draft
last-verified: 2026-11-15
---

# Council outcome

## Summary

Both members independently endorsed new references inside the core `rhizome` skill, a short routing addition to the in-app chat prompt, and no sibling skill. They split on how to honor SPEC-0080's rule against naming starters, whether optional add-ons belong on the first-run setup sheet, how much consent an in-app change needs, and how to split the references. The recommended design below takes a side on each.

Members ran read-only from the same brief, against `Rhizome usage skill research.md` and the notes beside it. Fable ran as `claudeAgent/claude-fable-5-2` at high effort and Astra as `codex/gpt-6-astra` at high effort on the standard tier.

## Corrections to the research, verified in code

- `rzm init --check --json` omits optional add-ons only on first run. A configured repository's plan comes from `settingsPlan`, which calls `allAddonOptions` in `pkg/app/cli/init/settings_json.go` and lists them all. First run uses `addonOptions` in `pkg/app/cli/init/first_run_json.go`, which skips them.
- A sibling skill cannot be named with a `rhizome` prefix. `pkg/app/cli/init/helper_templates_test.go` requires any core skill whose name starts with `rhizome` to be exactly `rhizome`.
- The starter-neutral rule is already broken. `references/code-mode.md` names Agentic Engineering and queries `Effort` as its worked example.
- Chat tables are a renderer gap, not a constraint. `web/src/components/chat/ChatTimeline.tsx` renders `ReactMarkdown` without `remark-gfm`, while `web/src/components/markdown/Markdown.tsx` already uses it.
- A carried command cheat sheet would conflict with SPEC-0080 US1-AC4, which makes `rzm agent surface` the authority instead of a frozen inventory.

## Agreement

- Extend `rhizome`. The questions already reach its router because they name Rhizome, and only same-skill references can link the existing mechanics by path.
- Do not add deictic triggers such as "what can I do here" to the description. The chat prompt routes those, and they would misfire in a standalone repository.
- Add one or two sentences to `chatInstructions`. They load the `rhizome` skill for questions about Rhizome itself, fall back to reading `.agents/skills/rhizome/SKILL.md` when the harness does not list the skill (the `--agents shared` gap), and say guidance is missing when neither exists.
- The chat agent names controls for the person to press. It does not try to operate the app.
- Recommend from repository evidence and the team's existing workflow. Choose the smallest set. Preview with `rzm init --check --json`. Preserve defaults explicitly, because an explicit `--addons` list replaces them. Report files installed, starter setup ready, and index ready as separate states.
- Verify routing with evals on both Claude Code and Codex, in standalone and in-app chat, including negatives for ordinary change work. Then run behavioral checks on disposable fixtures.
- Do not build a sibling usage skill, starter overlay slots, a guide-serving command, a recommender in Go, a carried command inventory, a long chat prompt, or agent control of the app.

## Disagreements and the recommended side

**Starter names under SPEC-0080.** Fable keeps the rule. Each `template.yaml` would gain `fit` (the repository signals that suggest it) and `cautions` (credentials, spend, egress), and the JSON plans would emit them. The skill would carry only the procedure. Astra amends the spec instead, so that one reference may name starters and explain suitability.

Recommendation: Fable's metadata. It keeps each starter's facts with the starter, covers ejected and team modules as well as built-in ones, and stays current as starters change. Because `code-mode.md` already names a starter, also replace that example with a neutral one and add a test that keeps starter ids out of the skill. If fixture runs show that `fit` strings recommend worse than the research heuristic, fall back to Astra's amendment.

**Optional add-ons on the setup sheet.** Fable emits them in the first-run plan but filters them out of the sheet. Astra reuses `allAddonOptions` and shows them unselected.

Recommendation: Astra's. The maintainer asked for this on 2026-10-30 ("the init workflow should show optional add-ons so you can activate them"), and it removes the duplicate builder.

**Consent in the chat's auto permission mode.** Fable requires a plain-text yes before any apply and prefers handing the person to Settings. Astra says a setup request already authorizes preview and apply, and asks only for unresolved consequential choices.

Recommendation: Astra's, with one boundary. An explanation or "which starter?" request stays read-only and ends in a recommendation. A request to set up or add something authorizes previewing and applying the plan it describes. A workflow switch removes files, so it still needs a yes. This matches the repository rule that approval covers the work it describes.

**Reference split.** Fable proposes three new references, folding customization into the product guide's goal-to-mechanism table. Astra proposes four, with customization on its own.

Recommendation: four. Schema evolution needs its own procedure and stop points, and that procedure would crowd an orientation file.

**First slice.** Fable ships only the product guide first, with no CLI change. Astra ships orientation and onboarding together.

Recommendation: Astra's. Onboarding and starter recommendation are half of the stated goal, and the CLI change is small.

## Recommended design

```text
pkg/app/cli/init/templates/skills/markdown/rhizome/
  SKILL.md                         description widened, four routing rows, onboarding row reworded
  references/usage.md              new: what Rhizome is, where you are running, what each view kind offers, goal to mechanism
  references/setup-and-starters.md new: inspect, recommend from live fit metadata, preview, apply, refresh, prove readiness
  references/customization.md      new: lowest rung first, schema-change procedure with stop points, one worked pipeline
  references/automation.md         new: code mode, saved scripts, module actions, scheduled tasks, unattended runs, cost and egress
  references/ontology-authoring.md fixed: module schema locations, small-extension and recipe examples
  references/code-mode.md          fixed: neutral example
pkg/app/cli/init/templates/starters/*/template.yaml   fit and cautions
pkg/app/cli/init/first_run_json.go, settings_json.go  emit fit, cautions, optional; first run lists optional add-ons
pkg/app/agentchat/page_relay.go                       chat routing sentence with shared-directory fallback
web/src/components/chat/ChatTimeline.tsx              remark-gfm
docs/specs/product/base-rhizome-agent-guidance.md     new user story, reference list
tests                                                 expectedRefs, starter-id guard, fit coverage, chat prompt cap,
                                                      a check that backticked rzm commands and flags in skill templates exist
```

Phasing:

1. Independent fixes: the `next-id` prefix in `effort-setup.md`, `remark-gfm` in the chat timeline, and AO KB in the human guides.
2. Orientation and onboarding: `usage.md`, `setup-and-starters.md`, the description and rows, the chat sentence, `fit` and `cautions`, optional add-ons on first run, the SPEC-0080 update, tests, and routing evals.
3. Customization: `customization.md` and the `ontology-authoring.md` and `code-mode.md` fixes.
4. Automation: `automation.md`, drafted against SPEC-0127 and exercised with one real action and one scheduled task, plus the command-literal test.

## Open risks

- A widened description may shift routing on ordinary change work. The evals gate it.
- A repository whose `SKILL.md` was edited locally keeps its old router, so new references arrive without the row that loads them.
- The shared-directory fallback for Claude chat and runtime access from a Codex chat are both unverified at run time.
- App detail in `usage.md` will rot unless it sticks to stable nouns and leans on the view block and `page_text`.
