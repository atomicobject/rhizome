---
type: ProductSpec
summary: "Defines how validation runs focused or configured checks, explains remediation, and safely plans and applies deterministic note repairs."
id: SPEC-0017
spec-status: active
last-updated: 2026-07-16
aliases:
  - SPEC-0017
  - Validation fixes workspace
---

# Validation fixes workspace

## Summary

Validation should evolve from a read-only issue report into a repair workflow that users can trust.

The product needs five distinct behaviors:

- report validation issues and the repair actions Rhizome can infer
- let humans and agents choose a focused check, the configured default suite, the comprehensive applicable suite, or the explicit audit suite
- expose the configured default suite through a read-only `rzm ci` command
- plan repairs by default and apply only deterministic structural fixes automatically from CLI and automation
- reconcile duplicate preferred identifiers without leaving filenames or references behind
- provide a web workspace that groups repeated issues, previews recommended repairs, and applies selected fixes through a fresh reviewable edit session

The core trust boundary is that validation never mutates and repair is explicit, reviewable, and deterministic. Ambiguous or contentful repairs remain visible with exact next steps; they do not turn automation into a guessing or repeated-confirmation loop.

## Goals

- make validation actionable instead of only diagnostic
- let CLI and automation clear safe structural issues without prompting
- let the web app show grouped recommended repairs rather than one row per repeated issue
- make it obvious which repairs are safe, which are suggested, and which need an agent-guided workflow
- tell agents and humans what structured action to take next after a failed validation run
- make the common local and CI path fast without requiring code indexing or embeddings
- make duplicate-identifier recovery deterministic across branches that share commit history
- provide a clean review-and-apply path for selected web fixes through a fresh edit session

## Non-Goals

- building the full web experience in the same change as the initial spec
- silently auto-fixing ambiguous or low-confidence issues
- generating substantive prose for missing sections or incomplete notes
- asking users the same confirmation questions every time repair apply runs
- shipping a second identifier allocation strategy in this effort
- treating source-code text or arbitrary prose mentions as safe identifier references to rewrite

## User Stories

### US1 - Plan repairable validation findings and explicitly apply deterministic repairs
- id:: ^SPEC-0017-US1
- summary:: Plan repairable validation findings and explicitly apply deterministic repairs.
- status:: satisfied

#### Acceptance Criteria

- `rzm validate fix [<suite-or-check>]` prints a deterministic plan and summary without mutating notes. ^SPEC-0017-US1-AC1
- Adding `--apply` applies safe deterministic repairs and leaves confirmation-needed or agent-required actions unresolved unless an interactive human explicitly confirms them. ^SPEC-0017-US1-AC2
- The agent surface never prompts; it returns structured questions and candidates for confirmation-needed actions. ^SPEC-0017-US1-AC3
- Re-running plan or apply after safe fixes are applied is idempotent for the same vault state. ^SPEC-0017-US1-AC4
- Suggested or agent-required repairs remain in the result with an exact follow-up command or explicit non-fixable reason. ^SPEC-0017-US1-AC5

### US2 - See grouped repair recommendations with counts and affected-file detail instead of one issue row per repeated instance
- id:: ^SPEC-0017-US2
- summary:: See grouped repair recommendations with counts and affected-file detail instead of one issue row per repeated instance.
- status:: ready

#### Acceptance Criteria

- The validation workspace shows grouped repair actions as first-class rows distinct from flat issue counts. ^SPEC-0017-US2-AC1
- Each grouped repair row shows its safety class, summary, and the number of issue instances it covers. ^SPEC-0017-US2-AC2
- Users can expand a grouped repair to inspect the affected notes or files before applying it. ^SPEC-0017-US2-AC3
- Repeated dead-link suggestions are presented as one grouped action when they share the same missing target and proposed replacement. ^SPEC-0017-US2-AC4

### US3 - Select grouped repairs, preview the resulting edits, and apply them through a fresh validation edit session
- id:: ^SPEC-0017-US3
- summary:: Select grouped repairs, preview the resulting edits, and apply them through a fresh validation edit session.
- status:: ready

#### Acceptance Criteria

- Selecting grouped repair actions and clicking apply creates a fresh reviewable edit session rather than mutating notes ad hoc.
- The apply flow previews the resulting changes before commit.
- The session can apply one grouped repair across multiple affected notes in one action.
- If a repair plan is stale because the vault changed since validation, the apply flow rejects or revalidates it instead of applying a mismatched edit set.

### US4 - Keep unresolved issues visible without pretending Rhizome can safely fix them automatically
- id:: ^SPEC-0017-US4
- summary:: Keep unresolved issues visible without pretending Rhizome can safely fix them automatically.
- status:: ready

#### Acceptance Criteria

- Validation distinguishes safe deterministic fixes from suggested repairs and agent-required repairs. ^SPEC-0017-US4-AC1
- Broken links with multiple plausible targets are not silently rewritten. ^SPEC-0017-US4-AC2
- Contentful repairs such as writing missing section prose are not auto-generated. ^SPEC-0017-US4-AC3
- Agent-required issues include enough explanation that a follow-on ontology-aware workflow can pick them up without rediscovering the ambiguity. ^SPEC-0017-US4-AC4

### US5 - Run focused or configured validation locally and in CI
- id:: ^SPEC-0017-US5
- summary:: Run focused or configured validation locally and in CI with consistent results and actionable remediation guidance.
- status:: satisfied

#### Acceptance Criteria

- `rzm validate` and `rzm ci` run the repository-configured default suite; the built-in core starts with ontology, identifiers, and broken internal note/heading/block targets. ^SPEC-0017-US5-AC1
- A user can run exactly one selector: omitted/`default`, configured `all`, fixed `audit`, or one named check such as `identifiers`; suite/check mixing is invalid and `all` never implies maintenance-only audit checks. ^SPEC-0017-US5-AC2
- Repository config can independently add or skip checks in the `default` and `all` sets; explicit named-check selection ignores those composed sets. A configured check that cannot run from scratch CI remains valid for local validation but returns blocked with guidance in CI. ^SPEC-0017-US5-AC3
- CI never mutates, offers separate machine-valid JSON and GitHub-annotation renderers, and gives the exact `rzm validate fix ...` command when remediation exists. ^SPEC-0017-US5-AC4
- Checks distinguish completed, not applicable, and blocked-by-prerequisite outcomes; validation never builds code indexes or embeddings to satisfy a blocked check. ^SPEC-0017-US5-AC5
- `rzm validate list` explains each check's suite, applicability, prerequisites, and remediation support. ^SPEC-0017-US5-AC6

### US6 - Reconcile duplicate preferred identifiers after branch merges
- id:: ^SPEC-0017-US6
- summary:: Reconcile duplicate preferred identifiers deterministically while preserving governed filenames and unambiguous references.
- status:: satisfied

#### Acceptance Criteria

- The identifiers check detects duplicate preferred identifiers for every ontology field that declares identifier semantics. ^SPEC-0017-US6-AC1
- The repair plan selects one keeper and allocates replacements deterministically, producing identical results for repositories with the same relevant content and shared Git history. ^SPEC-0017-US6-AC2
- Apply updates the preferred identifier, alias mirror, governed filename token, recursively derived descendant identifiers/locators, and every structurally resolved inbound reference through the repair engine's connected transactions. ^SPEC-0017-US6-AC3
- Ambiguous references or stale preconditions skip the affected identifier transaction without blocking independent repairs and report how to replan or resolve the ambiguity. ^SPEC-0017-US6-AC4
- Plain-text mentions and source-code tokens are reported as review candidates, not silently rewritten. ^SPEC-0017-US6-AC5

## Requirements

### Must

- Validation MUST emit a structured repair plan alongside issue results when repairable or reviewable issues exist.
- The repair plan MUST distinguish at least three safety classes: safe deterministic, suggested review, and agent-required.
- `rzm validate` and `rzm ci` MUST remain read-only.
- `rzm validate fix` MUST plan by default; mutation MUST require `--apply`.
- Non-interactive repair MUST apply only safe deterministic fixes.
- Human CLI repair MAY ask once for each confirmation-needed action during an explicit `--apply` session; agent and other non-interactive repair MUST remain safe-only and MUST NOT prompt. No surface may repeatedly prompt for the same unresolved repair.
- The web validation workspace MUST present grouped repair actions separately from raw issue rows.
- Grouped repair actions MUST include the number of affected instances and enough detail to expand the affected-note list.
- The product MUST support grouped repair actions for repeated simple cases such as the same dead link appearing in many notes.
- Validation output MUST include structured next-action guidance that distinguishes safe auto-fix, confirmation-needed review, agent-required remediation, check errors, and unresolved issues that need classification.
- The web apply flow MUST create a fresh edit session before changing notes.
- The review/apply flow MUST allow users to preview selected repairs before commit.
- Validation MUST leave ambiguous or contentful repairs unresolved in the deterministic path.
- The default suite MUST include ontology, identifiers, and narrowly scoped broken internal note/heading/block targets unless repository config skips one.
- Every default-suite issue code MUST have an executable repair planner that returns either deterministic edits or a structured confirmation/agent-blocked operation with evidence and an exact next command; a generic unclassified/non-fixable registration is insufficient.
- `all` MUST mean all applicable comprehensive health checks for the vault, not every registered maintenance, migration, network, or expensive diagnostic.
- Lifecycle-protected complete or archived history MUST require an explicit `--allow-historical` repair override; that override MUST NOT bypass conflicts, ambiguity, ontology rules, or atomicity.

### Should

- Safe deterministic fixes should include structural repairs such as alias mirroring, required list initialization, deterministic identifier backfill, and explicit missing-section scaffolds.
- Validation output should expose next actions in JSON so agents can run safe fixes, review ambiguous repairs, and classify leftover findings without parsing prose.
- The web workspace should preserve enough repair metadata from the validation result that it does not need to derive edits client-side.
- The workspace should make the difference between issue count and repair count obvious.
- Agent-required issues should explain why they are not deterministic so a user or agent can triage them quickly.

### May

- The web workspace may later support suggested-but-not-safe review flows as long as they remain separate from non-interactive agent repair.

## Documentation Plan

- Update README and CLI help with the check/suite menu, plan/apply examples, config overlays, historical override, and CI workflow.
- Update agent-facing validation guidance and structured result descriptions so agents use exact remediation commands and ask only for semantic decisions.
- Keep validation, indexing, ontology, CLI, and vault-core subsystem references aligned with their implementation boundaries.

## Open Questions

None for the selected CLI, CI, identifier, and repair-foundation stories. Browser-only presentation and suggested-action apply behavior remain future product slices and do not block these efforts.
