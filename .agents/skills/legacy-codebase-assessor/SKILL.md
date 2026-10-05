---
name: legacy-codebase-assessor
description: >-
  Use when running a structured assessment of a legacy codebase. Orchestrates the
  four Rhizome assessment reports (doc_coverage, hotspots, complexity, optionally
  code_similarity), diagnoses scope problems before running, and writes a
  synthesis findings note that seeds the next steps in the engagement workflow.
  Two modes: assess (initial run on a freshly-indexed repo) and refresh (re-run
  on an already-assessed vault).
---

# Legacy Codebase Assessor

## Goal

Turn a freshly-indexed legacy codebase into a structured assessment in one invocation that:

1. Diagnoses scope **before** running reports so partial-index situations don't produce misleading-looking results
2. Runs the four reports against one shared session and one user-provided path
3. Synthesizes the JSON outputs into a `docs/assessment/findings-summary.md` note
4. Seeds the next skill in the chain: per-priority-file `assumption-tracker` type candidates and a named starting subsystem for documentation work

## When to use

- After `rzm init` + `rzm index` have completed on a legacy codebase, when the engineer needs the structured assessment report set
- When picking up an engagement that was already assessed and re-running against the current index (use `mode=refresh`)
- Whenever the user says "assess this codebase", "run the assessment reports", "produce the findings note", or "start a legacy codebase engagement"

## Non-goals

- Does not run `rzm init` or `rzm index` — these remain user-driven CLI steps per the engagement workflow contract
- Does not flag uncertainties during the documentation that follows the assessment — that is `assumption-tracker --mode flag`
- Does not package client deliverables — that is `client-harness-builder`, the final step of the engagement workflow
- Does not modify the indexer or the report CLI; consumes the existing `rzm agent start` and `rzm agent report` surfaces
- Does not block on missing embeddings — `code_similarity` is skipped with a warning when an embedding provider is unconfigured; the other three reports run regardless

## Operating mode

`Agent Delegation` — the workflow is bounded and the steps are explicit. Once mode and `<path>` are known, produce the assessment directly. Use `Agent Partnership` only when the scope diagnostic surfaces something the user needs to confirm (an empty configured language, a path-vs-corpus mismatch that the user might want to redirect).

## Engagement workflow context

This skill is the first step of the assessment workflow. Before presenting the chain or writing its handoff, check whether the active harness exposes an installed `assumption-tracker` skill.

```
legacy-codebase-assessor  →  assumption-tracker  →  client-harness-builder
       (assess)                (flag + synthesize)        (package)
```

- **This skill**: produces the structured findings note that identifies what to document next
- **`assumption-tracker --mode flag`**: when installed through the `action-items` starter, captures uncertain findings as typed `#assumption/<type>` ActionItems during documentation work
- **`assumption-tracker --mode synthesize`**: when installed, groups open assumptions before client kickoff meetings
- **`client-harness-builder`**: packages validated documentation into Rhizome-free client deliverables at engagement close

If `assumption-tracker` is unavailable, state that the optional assumption phase requires the `action-items` starter, omit assumption-tracker commands, and hand off directly from assessment/documentation to `client-harness-builder`. Do not imply the skill or ActionItem ontology is installed.

## Procedure

### Mode resolution

The skill operates in one of two modes:

- `mode=assess` (default) — initial assessment of a freshly-indexed repo. Requires a `<path>` argument (the path under the vault to assess; commonly `.`).
- `mode=refresh` — re-run an assessment on a vault that already has `docs/assessment/findings-summary.md`. Reads the prior `<path>` and scope from the existing note's frontmatter; no `<path>` argument required.

If `mode` is not specified by the user, default to `assess`.

### Step 1 — Reuse or open a session

```bash
# Reuse any existing $SESSION_ID the agent has from earlier in the conversation.
# Otherwise:
SESSION_ID=$(rzm agent start --profile code | jq -r .sessionId)
```

Capture `$SESSION_ID` and use it for all subsequent calls.

### Step 2 — Diagnose scope BEFORE running reports

This step prevents the "all results in one language" / "zero results in a configured language" trap. Run sqlite queries against the index DB at `<vault>/.rhizome/db.sqlite` and inspect:

```bash
DB="$(pwd)/.rhizome/db.sqlite"

# Per-language file + symbol counts
sqlite3 "$DB" "SELECT lang, COUNT(*) AS symbols, COUNT(DISTINCT file) AS files FROM symbols GROUP BY lang"

# Per-language module definitions
sqlite3 "$DB" "SELECT lang, COUNT(*) FROM intel_module_defs GROUP BY lang"

# Top-level directory distribution (for path-vs-corpus check)
sqlite3 "$DB" "
  SELECT substr(file, 1, instr(file, '/') - 1) AS dir, lang, COUNT(DISTINCT file) AS files
  FROM symbols GROUP BY dir, lang ORDER BY files DESC LIMIT 20"
```

Then compare against `.rhizome/config.yml`'s `code.*` blocks. **Warn and pause for confirmation** if any of these conditions trip:

- A language is configured (e.g. `code.php.roots: [...]`) but has **0 symbols** in the index → indexing skipped that language; suggest `rzm index --rebuild` and verifying roots match disk reality
- A single language accounts for **>90%** of indexed symbols while multiple languages are configured → likely a partial-walk situation; surface the breakdown to the user before continuing
- The user provided a `<path>` argument and **>70%** of indexed symbols are outside that path → suggest broadening the path or accepting the narrower scope explicitly

If the diagnostic is clean, proceed without prompting.

### Step 3 — Run the four reports (assess mode)

```bash
rzm agent report --session-id "$SESSION_ID" --op doc_coverage --path "<path>"
rzm agent report --session-id "$SESSION_ID" --op hotspots     --path "<path>"
rzm agent report --session-id "$SESSION_ID" --op complexity   --path "<path>"
```

Then attempt `code_similarity`:

```bash
rzm agent report --session-id "$SESSION_ID" --op code_similarity --path "<path>"
```

If `code_similarity` returns a "no embedding provider configured" error, **continue without failing**. Capture the omission in the synthesis note's preamble: *"code_similarity skipped — no embedding provider configured."*

Optionally run vault-health for additional signal:

```bash
rzm agent vault-health --session-id "$SESSION_ID"
```

### Step 4 — Synthesize the findings note

Parse the four JSON outputs and populate the synthesis note using the structure in `references/findings-template.md`. The template prescribes:

- **Executive summary**: coverage %, top hotspot count, complexity outlier count, total indexed files
- **Top-5 documentation priority files**: the intersection of low doc_coverage × high hotspot or complexity score. Each entry includes the file path, why it's high-priority (the specific signals), and **suggested assumption types** the documentation agent should expect to flag (drawn from `assumption-tracker`'s four-type vocabulary: `business-logic`, `schema`, `integration`, `process`)
- **Hotspot × complexity intersections**: files that appear in both top-N lists — highest documentation ROI
- **Doc-coverage gaps**: configured paths returning 0% covered
- **Suggested starting subsystem**: the directory containing the most top-5 files; named explicitly so the next skill in the chain has a starting point
- **Handoff line**: parametrized next-step prompt naming `assumption-tracker --mode flag` and the suggested subsystem

Write the note to `docs/assessment/findings-summary.md` (create the `docs/assessment/` directory if missing). The frontmatter MUST include `previous-assessment` and `previous-scope` blocks (left empty in assess mode; populated by refresh mode) so re-runs can pick up cleanly.

### Step 5 — Refresh-mode branch

If `mode=refresh`:

1. Read `docs/assessment/findings-summary.md`. If absent, error: "no prior assessment found — use `mode=assess` for the first run."
2. Extract the prior `path` and `previous-scope` (indexed roots + embeddings status) from frontmatter.
3. Run Step 2 (scope diagnostic) against the current index.
4. **Compare**: list any languages or top-level directories that are now in the index but weren't in the prior `previous-scope`. Emit a scope-drift warning: *"New since last assessment: PHP code added under wp-includes"* (or similar). Same for shrinkage.
5. Run Step 3 (reports) using the prior `path`.
6. Run Step 4 (synthesis), overwriting the prior `findings-summary.md`. The new note's frontmatter records the previous assessment date in `previous-assessment` and the current scope in `previous-scope`. Refresh compares scope only; it does not compare findings between runs.

### Step 6 — Hand off

When `assumption-tracker` is installed, end with this parameterized prompt for the user/agent:

> *Assessment complete. `docs/assessment/findings-summary.md` is ready. Next: use `assumption-tracker --mode flag` while documenting `<suggested-starting-subsystem>` (top priority); use `client-harness-builder` when the vault is ready for client handoff.*

When it is unavailable, end with:

> *Assessment complete. `docs/assessment/findings-summary.md` is ready. The optional assumption phase is unavailable because the `action-items` starter is not installed; document `<suggested-starting-subsystem>` next, or rerun `rzm init` with action-items enabled. Use `client-harness-builder` when the vault is ready for client handoff.*

## Guardrails

- **Diagnose before reporting.** Run Step 2 before any report. On a partial index the reports return JSON that reads as "the codebase has no signal" when the real cause is missing scope.
- **Single session, single path.** All four `rzm agent report` calls in one invocation use the same `$SESSION_ID` and `<path>`. Do not re-enter `rzm agent start` per report.
- **Embeddings missing is a warning, not a failure.** `code_similarity` is the only embedding-dependent report. If it fails because of provider config, log "skipped" in the findings note's preamble and keep going.
- **Assumption types use the established four-type vocabulary.** The findings note seeds candidate types from `assumption-tracker`'s contract (`business-logic`, `schema`, `integration`, `process`). Do not invent new types here.
- **Optional means checked.** Never emit an `assumption-tracker` command unless that skill is installed in the active harness; use the direct handoff above when it is absent.
- **Never write client-facing artifacts from this skill.** Output is internal AO assessment material. Client deliverables are `client-harness-builder`'s job.
- **Refresh mode requires a prior assessment.** If `docs/assessment/findings-summary.md` doesn't exist, fail with a redirect to assess mode rather than silently degrading.

## Reference notes

- `references/findings-template.md` — the synthesis note structure populated by Step 4
- When the `action-items` starter is installed, use its `assumption-tracker` skill for the four-type assumption vocabulary and flag-mode procedure used after assessment
- See [`client-harness-builder`](../client-harness-builder/SKILL.md) for the final-stage engagement skill that packages validated documentation for client handoff
