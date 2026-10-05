---
type: ProductSpec
id: SPEC-0069
summary: "Skill-forward legacy codebase assessment workflow: a new `legacy-codebase-assessor` skill orchestrates the four assessment reports + findings synthesis + assessment-vault bootstrap, replacing copy-pasted `rzm agent report` CLI invocations. The companion playbook rewrite reduces user-visible CLI to `rzm init` + `rzm index` and routes everything else through skills."
spec-status: active
last-updated: 2026-09-05
aliases:
  - SPEC-0069
  - skill-forward-legacy-assessment
---

# Skill-Forward Legacy Codebase Assessment

## Summary

Today, [`docs/reference/guides/playbook-legacy-codebase-assessment.md`](../../reference/guides/playbook-legacy-codebase-assessment.md) walks an AO engineer through six steps to assess a legacy codebase. Steps 3–6 (orient session, run four reports, interpret results, produce assessment vault) are command-forward: 21 `rzm` invocations across 257 lines, requiring the user to manage session ids, copy outputs between commands, and synthesize JSON reports by hand.

CLI commands are for agents, not for end users directly, so guide-style playbooks should be skill-forward (show which skill to invoke) rather than command-forward. The friction was confirmed empirically during EFF-0033's dogfood test on WordPress (`wp-tst`): running three reports manually surfaced two failure modes — session-id juggling and scoping confusion (`--path wp-content` silently excluded all `wp-includes` results) — both of which an orchestrator skill eliminates.

This spec introduces a new `legacy-codebase-assessor` binary-embedded skill that owns the assessment workflow end-to-end, then rewrites the playbook to be skill-forward per Drew's contract. The skill composes with [`client-harness-builder`](../../../pkg/app/cli/init/templates/skills/markdown/client-harness-builder/SKILL.md) and the action-items-backed [`assumption-tracker`](../../../pkg/app/cli/init/templates/starters/action-items/agents/skills/assumption-tracker/SKILL.md) (both from [SPEC-0067](client-engagement-tooling.md)), completing the AO client-engagement skill triad: **assess → flag assumptions → package harness**.

## Goals

- An AO engineer can invoke a single `legacy-codebase-assessor` skill to run the full Step 3–6 assessment instead of typing four separate `rzm agent report` calls
- The skill diagnoses scope before running reports (warns when the indexed corpus is narrower than expected — e.g. core code missing, path filter excludes major areas) so partial-result reports don't look like "nothing's there"
- The skill produces a `docs/assessment/findings-summary.md` synthesis note (executive summary, hotspot analysis, doc-coverage gaps, recommendations) — no manual `rzm note create` needed
- The skill's output **actively seeds** the next skill in the chain: priority files come with suggested `assumption-tracker` types, and the handoff prompt names the starting subsystem rather than handing back a vague "go document things"
- The skill supports two modes — `assess` (initial) and `refresh` (re-run on existing assessment) — so engagements past their first kickoff stay supported
- The playbook stays useful as an orientation doc, but only `rzm init` and `rzm index` remain visible to the user; all other steps route through skills (`legacy-codebase-assessor`, `assumption-tracker`, `client-harness-builder`)
- The skill ships binary-embedded so any AO team running `rzm init` on a future engagement gets it without rediscovering the pattern

## Non-Goals

- Does not modify the existing `client-harness-builder` or `assumption-tracker` skills (SPEC-0067 owns their contracts)
- Does not change the underlying `rzm agent report` CLI surface — the skill calls those commands internally; the CLI remains available for direct agent use
- Does not add new report types — the four existing reports (`doc_coverage`, `hotspots`, `complexity`, `code_similarity`) remain the canonical assessment surface; `code_similarity` is still optional (requires embeddings)
- Does not produce client deliverables — that's `client-harness-builder`'s job, which runs **after** assessment
- Does not auto-fix the partial-index issue surfaced in wp-tst (separate bug fix; see SPEC-0068 / EFF-0033 deviation) — the skill only **detects and warns** when scope looks suspicious

## User Stories

### US1 - Run full assessment via one skill invocation

- id:: ^SPEC-0069-US1
- summary:: An AO engineer points the `legacy-codebase-assessor` skill at an indexed repo and gets all four reports run, interpreted, and synthesized into a findings note without re-typing session ids or copying JSON between calls.
- status:: satisfied

When the codebase is indexed (Steps 1–2 of the playbook done), the engineer should be able to say "run the assessment" and have the skill handle session management, the four report invocations, JSON parsing, and the interpretation framing prompt (currently a copy-paste prompt block in Step 5).

#### Acceptance Criteria

- **Skill exists and is binary-embedded**: The skill ships at `pkg/app/cli/init/templates/skills/markdown/legacy-codebase-assessor/SKILL.md` and installs into `.agents/skills/` and `.claude/skills/` after `rzm init` (any starter). `assumption-tracker` joins the installed workflow when the `action-items` starter is enabled.
  verification:: `rzm init` on a fresh vault produces `legacy-codebase-assessor/SKILL.md` in the agent-skills directories.
- **Orchestrates all four reports with one session**: The skill's procedure calls `rzm agent start` once, captures the `sessionId`, then runs `doc_coverage`, `hotspots`, `complexity`, and (when embeddings are available) `code_similarity` against the same session and path scope.
  verification:: Reading the SKILL.md procedure confirms a single `rzm agent start` followed by sequential `rzm agent report` calls all using the same `sessionId`.
- **Path scope is captured once**: The user provides the target path once at skill invocation; the skill uses it for all four reports. No re-typing.
  verification:: SKILL.md procedure shows a single `<path>` placeholder applied across all four report calls.
- **Synthesis produces a findings note**: After the four reports complete, the skill writes a findings note to `docs/assessment/findings-summary.md` (or `Assessment/findings-summary.md` per repo convention) containing: executive summary, top-5 documentation priority files, hotspot × complexity intersections, and a "next steps" prompt.
  verification:: SKILL.md procedure ends by writing a synthesis note to a documented path.

---

### US2 - Skill warns when scope looks suspicious

- id:: ^SPEC-0069-US2
- summary:: Before running reports, the skill checks the indexed corpus against the configured roots and flags likely scope problems (zero PHP files when PHP roots are set, all symbols in one language when multiple are configured, etc.) so the engineer doesn't interpret a misleading-looking report as the codebase being empty.
- status:: satisfied

The wp-tst dogfood test surfaced exactly this trap: `doc_coverage --path wp-content` returned all-TypeScript results because the user didn't realize wp-includes wasn't being walked. A skill that inspects the index before reporting can catch this in one diagnostic step.

#### Acceptance Criteria

- **Skill inspects the index pre-report**: Before invoking any `rzm agent report` call, the skill queries the symbol/file counts per language (via SQL or a future `rzm code stats` command) and presents the count to the user.
  verification:: SKILL.md procedure has an explicit "diagnose scope" step before report invocations, showing what the skill is looking for.
- **Empty/skewed scope produces a warning**: If a configured language has zero files in the index, or one language dominates >90% when multiple roots are configured, the skill warns the user and suggests a likely remedy (check config, rebuild index, expand roots) before continuing.
  verification:: SKILL.md guardrails document the warning conditions and suggested remedies.
- **Scope mismatch with `--path` is detected**: If the user's specified `--path` excludes most of the indexed corpus (e.g. `--path wp-content` when 70% of symbols are under `wp-includes`), the skill names the mismatch and asks the user to confirm before proceeding.
  verification:: SKILL.md procedure includes a path-vs-corpus check before invoking reports.

---

### US3 - Playbook is rewritten to be skill-forward

- id:: ^SPEC-0069-US3
- summary:: `docs/reference/guides/playbook-legacy-codebase-assessment.md` is rewritten so only `rzm init` and `rzm index` remain as user-visible CLI; Steps 3–6 become "invoke the `<skill>` skill". The CLI sequences move into the relevant SKILL.md files.
- status:: satisfied

Following that skill-forward framing, the playbook should orient the human engineer ("here's the chain of skills to invoke"), not enumerate the CLI commands. CLI is the skill's job.

#### Acceptance Criteria

- **Playbook references only the user-visible CLI exceptions**: After the rewrite, `rzm` appears only in Steps 1–2 (`rzm init`, `rzm index`) and in the "Inheriting a Configured Assessment Repo" section's bootstrap commands. No `rzm agent report` or `rzm agent semantic-query` calls appear in the user-facing playbook body.
  verification:: `grep -c "rzm " docs/reference/guides/playbook-legacy-codebase-assessment.md` returns a count ≤6 (init + index + a small number of inheritance bootstrap commands), down from the current 21.
- **Steps 3–6 route through skills**: Step 3 (orient) is absorbed into the assessor skill. Step 4 (reports) becomes "invoke the `legacy-codebase-assessor` skill". Step 5 (interpret) is the same skill. Step 6 (assessment vault) is the same skill. Subsequent flagging routes through `assumption-tracker`; client packaging routes through `client-harness-builder`.
  verification:: The playbook body explicitly names each skill and its invocation context.
- **Original CLI sequences preserved in skill files**: The four `rzm agent report` invocations and their interpretation prompt from the current Step 4–5 are moved into the assessor's SKILL.md (not lost — relocated).
  verification:: Each command and prompt present in today's playbook Step 4–5 appears in the new assessor SKILL.md procedure or guardrails.
- **Playbook links the full skill chain**: The playbook orients the engineer to the three-skill workflow (`legacy-codebase-assessor` → `assumption-tracker` → `client-harness-builder`) with a short rationale for each.
  verification:: The playbook body lists the three skills as ordered phases of an engagement.

---

### US4 - Skill chain integrates with assumption-tracker and client-harness-builder

- id:: ^SPEC-0069-US4
- summary:: The assessor skill's output actively seeds the next steps in the engagement chain: it identifies candidate uncertainties for `assumption-tracker --mode flag` (during documentation work) and hands off to `client-harness-builder` at engagement close. Handoff is concrete (named files + suggested assumption types), not vague ("look at these files later").
- status:: satisfied

Drew's framing was the **chain**, not the individual skills. The findings note from the assessor names which files have low coverage and high hotspot scores — those are exactly the files the engineer will start documenting next, and where `assumption-tracker` flags will accumulate. The harness packager later turns the validated docs into client deliverables. The skill chain should compose: assessment output should be assumption-tracker's input, not a separate manual translation step.

#### Acceptance Criteria

- **Findings note identifies documentation priorities**: The findings note lists at least: top hotspot files needing docs, complexity outliers without coverage, and one suggested starting subsystem. This is the input set for `assumption-tracker` flag mode.
  verification:: SKILL.md output template includes these three priority sections.
- **Findings note seeds assumption candidates**: For each top-priority file the assessor names, the findings note suggests likely assumption types to flag (drawing on `assumption-tracker`'s four-type vocabulary: `business-logic`, `schema`, `integration`, `process`). Example: "wp-cron.php — high complexity, zero docs; expect `business-logic` and `process` assumptions during walkthrough." This is a starting list, not exhaustive — the engineer adds/removes as they document.
  verification:: SKILL.md findings template has a "Suggested assumption types per priority file" sub-section that uses the assumption-tracker vocabulary.
- **Skill's "Next steps" prompt names the next skill**: The synthesis prompt ends with explicit handoff text: *"Next: use `assumption-tracker --mode flag` while documenting these files (start with <subsystem>); use `client-harness-builder` when the vault is ready for client handoff."* The handoff names the suggested starting subsystem so the engineer can begin without re-deriving it.
  verification:: SKILL.md procedure ends with this handoff text, parameterized on the chosen subsystem.
- **Playbook documents the full chain as the engagement workflow**: The rewritten playbook frames the three skills as the engagement workflow shape (assess → flag → package), with the assessor as Step 3, assumption flagging as Steps 4–5, and packaging as the final step.
  verification:: Playbook chain diagram or list explicitly orders the three skills.
- **assumption-tracker SKILL.md cross-references the assessor**: The existing `assumption-tracker` skill's "Reference notes" section gains a one-line back-reference: *"See `legacy-codebase-assessor` for upstream assessment that seeds candidate flagging files."* Symmetric link to make the chain discoverable from either direction.
  verification:: `pkg/app/cli/init/templates/starters/action-items/agents/skills/assumption-tracker/SKILL.md` includes the back-reference line.

---

### US5 - Refresh mode for re-assessment

- id:: ^SPEC-0069-US5
- summary:: An AO engineer running a follow-up assessment on an already-indexed vault can invoke `legacy-codebase-assessor --mode refresh` to re-run the four reports against the current index and produce an updated `findings-summary.md`, without re-deriving scope or re-typing the path.
- status:: satisfied

Engagements aren't one-shot. After initial assessment + documentation work + assumption resolution, the engineer needs to re-run the reports to see what changed (coverage went up? new hotspots emerged from a code change?). Mirrors the two-mode shape of `assumption-tracker` (flag/synthesize).

#### Acceptance Criteria

- **Skill supports two modes**: The skill's frontmatter description and procedure document two modes: `assess` (initial run; default) and `refresh` (re-run on already-assessed vault). Mirrors `assumption-tracker`'s shape.
  verification:: SKILL.md frontmatter and procedure both describe the two modes.
- **Refresh reuses previously-recorded scope**: When `refresh` runs and a prior `findings-summary.md` exists, the skill reads the scope (path, indexed roots, embeddings status) from the prior note's metadata and reuses it. The engineer doesn't re-type `--path`.
  verification:: SKILL.md procedure documents reading the prior findings note metadata before report invocations.
- **Refresh overwrites findings-summary.md but preserves provenance**: The new note replaces `docs/assessment/findings-summary.md`; the header includes a "Previous assessment: YYYY-MM-DD" line citing the prior date for traceability. v1 does not diff the two; that's deferred per Open Questions.
  verification:: SKILL.md findings template header includes a previous-assessment date line that's populated in refresh mode.
- **Refresh detects scope drift**: If the index now contains languages or roots that weren't present in the prior assessment, the skill flags this in the refresh output ("New since last assessment: PHP code added under wp-includes"). Helps the engineer notice when their engagement scope grew.
  verification:: SKILL.md procedure includes a scope-comparison step when running in refresh mode.

## Requirements

- The new skill MUST be binary-embedded under `pkg/app/cli/init/templates/skills/markdown/legacy-codebase-assessor/`, matching the install path of existing client-engagement skills
- The skill SKILL.md MUST follow the format of existing binary-embedded skills (frontmatter `name`/`description`; sections `Goal`/`When to use`/`Non-goals`/`Operating mode`/`Procedure`/`Guardrails`)
- The skill MUST NOT introduce new `rzm` subcommands or modify the existing `rzm agent report` interface — it consumes the existing surface
- The skill MUST NOT produce client-facing artifacts — that responsibility stays with `client-harness-builder`
- The rewritten playbook MUST preserve all assessment metrics and "What Good Looks Like" tables from the current version; only the **routing** changes (skills vs. CLI)
- The playbook MUST link to the assessor SKILL.md and to SPEC-0067's two skills for handoff context
- The skill MUST detect and warn when the index is suspiciously narrow before running reports (the wp-tst trap)

## Open Questions

- **Refresh-mode change-summary depth**: When `refresh` mode re-runs on an already-assessed vault, should it diff the new findings against the prior `findings-summary.md` (mention what's new, what resolved) or just overwrite with the latest snapshot? Proposed: full overwrite for v1; cite the prior assessment date in the new note's header for traceability. Defer change-diffing to a follow-on if engineers actually want it.

## Documentation Plan

- New file: `pkg/app/cli/init/templates/skills/markdown/legacy-codebase-assessor/SKILL.md` — the new binary-embedded skill, two modes (`assess` / `refresh`)
- New file: `pkg/app/cli/init/templates/skills/markdown/legacy-codebase-assessor/references/findings-template.md` — synthesis note starting template (executive summary, hotspots, doc-coverage gaps, suggested assumption types per priority file, next-skill handoff)
- Rewrite: `docs/reference/guides/playbook-legacy-codebase-assessment.md` — skill-forward shape with assess→flag→package chain explicit
- Update: `pkg/app/cli/init/templates/starters/action-items/agents/skills/assumption-tracker/SKILL.md` — add reverse-direction cross-link to the assessor (US4 acceptance criterion)
- Update: `pkg/app/cli/init/templates/skills/markdown/client-harness-builder/SKILL.md` — note that assessment + assumption-flagging precedes harness packaging in the engagement workflow
- Possible update: `pkg/app/mcp/CONTEXT.md` or `cmd/agent_surface.go` — if those surfaces enumerate available skills (verify during plan phase)
- No change: `AGENTS.md` "Active Technologies" section is unaffected (skill-only feature, no new tech)
