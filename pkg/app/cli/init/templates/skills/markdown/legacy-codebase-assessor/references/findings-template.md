# Template: Assessment Findings Summary

Starting template for `docs/assessment/findings-summary.md`. The `legacy-codebase-assessor` skill populates this template after running the four assessment reports. Replace all `[PLACEHOLDER]` values with the synthesized output. Keep the section structure stable so refresh-mode runs can compare across assessments.

---

```yaml
---
type: ReferenceDoc
reference-kind: assessment
status: draft
assessment-date: [PLACEHOLDER: YYYY-MM-DD of this run]
path: [PLACEHOLDER: assessed path, e.g. "." or "wp-includes"]
embeddings: [PLACEHOLDER: "enabled" or "skipped — no provider configured"]
# Refresh-mode provenance — left blank on initial assess; populated on every refresh
previous-assessment: [PLACEHOLDER: YYYY-MM-DD of prior run, or empty for initial assessment]
previous-scope:
  languages: [PLACEHOLDER: list of language ids present in prior assessment]
  top-dirs: [PLACEHOLDER: list of top-level dirs that had >0 indexed files in prior assessment]
---
```

# Assessment Findings — [PLACEHOLDER: codebase name]

## Executive Summary

- **Indexed files**: [PLACEHOLDER: total file count]
- **Indexed symbols**: [PLACEHOLDER: total symbol count] across [PLACEHOLDER: count] languages: [PLACEHOLDER: language list, e.g. `php, ts`]
- **Documentation coverage**: [PLACEHOLDER: doc_coverage percent]% of significant files documented
- **Top hotspot**: [PLACEHOLDER: file path with highest authority score]
- **Complexity outliers**: [PLACEHOLDER: count of functions >5x median complexity]
- **Embeddings**: [PLACEHOLDER: "enabled" or "skipped — code_similarity report unavailable"]

[PLACEHOLDER: One paragraph executive narrative — what kind of codebase this is, where the documentation investment should go, what jumps out from the four reports.]

## Top 5 Documentation Priority Files

Each entry is a file that scored high on hotspot or complexity but low on coverage — the highest-ROI targets for the documentation phase that follows this assessment.

### 1. [PLACEHOLDER: file path]

- **Why high priority**: [PLACEHOLDER: cite specific signals — e.g., "complexity 8.3x median + 24 inbound code references + zero coderefs"]
- **Suggested assumption types**: [PLACEHOLDER: 1–2 from `business-logic` / `schema` / `integration` / `process`, with one-line rationale]
- **Suggested approach**: [PLACEHOLDER: short note — e.g., "walk through the main control flow, ask domain expert about the conditional branches"]

### 2. [PLACEHOLDER: file path]

- **Why high priority**: [PLACEHOLDER]
- **Suggested assumption types**: [PLACEHOLDER]
- **Suggested approach**: [PLACEHOLDER]

### 3. [PLACEHOLDER: file path]

- **Why high priority**: [PLACEHOLDER]
- **Suggested assumption types**: [PLACEHOLDER]
- **Suggested approach**: [PLACEHOLDER]

### 4. [PLACEHOLDER: file path]

- **Why high priority**: [PLACEHOLDER]
- **Suggested assumption types**: [PLACEHOLDER]
- **Suggested approach**: [PLACEHOLDER]

### 5. [PLACEHOLDER: file path]

- **Why high priority**: [PLACEHOLDER]
- **Suggested assumption types**: [PLACEHOLDER]
- **Suggested approach**: [PLACEHOLDER]

## Hotspot × Complexity Intersections

Files that appear in both the top hotspot list AND the top complexity list — highest documentation ROI. Document these first.

- [PLACEHOLDER: file path] — hotspot rank [PLACEHOLDER: n], complexity rank [PLACEHOLDER: n]
- [PLACEHOLDER: file path] — hotspot rank [PLACEHOLDER: n], complexity rank [PLACEHOLDER: n]
- ...

## Doc-Coverage Gaps

Top-level directories or subsystems with the lowest coverage. Worth identifying as future-engagement candidates if not in scope now.

| Path | Files | Coverage | Notes |
|---|---|---|---|
| [PLACEHOLDER] | [PLACEHOLDER] | [PLACEHOLDER]% | [PLACEHOLDER] |

## Suggested Starting Subsystem

**[PLACEHOLDER: directory path]**

Why: [PLACEHOLDER: one-line rationale — usually "contains N of the top-5 priority files" or "highest hotspot density"]

## Scope Drift Since Previous Assessment

*(Populated by refresh mode only. Left empty on initial assess.)*

[PLACEHOLDER: Listed scope changes since `previous-assessment`. Examples:
- "New since last assessment: PHP code added under wp-includes (574 files)"
- "Removed since last assessment: ts code under wp-content/themes/twentynineteen"
- "Configuration changed: code.php.roots expanded from [wp-content] to [wp-includes, wp-admin, wp-content]"]

## Next Steps

Assessment complete. `docs/assessment/findings-summary.md` is ready.

**Next when `assumption-tracker` is installed**: use `assumption-tracker --mode flag` while documenting **[PLACEHOLDER: suggested-starting-subsystem]** (the top-priority starting point above); use `client-harness-builder` when the vault has validated documentation and is ready for client handoff.

**Next without `assumption-tracker`**: state that the optional assumption phase requires the `action-items` starter, then document **[PLACEHOLDER: suggested-starting-subsystem]** and hand off directly to `client-harness-builder`. Do not emit a missing-skill command.

When the skill is installed and a top-priority file's domain meaning is unclear from the code, flag the uncertainty immediately with `assumption-tracker --mode flag` near the relevant code — don't accumulate uncertain claims in this findings note.

---

## Refresh Cycle

When this codebase has been worked on since this assessment was generated, re-run with `legacy-codebase-assessor` in `mode=refresh`. The skill reads this note's frontmatter to recover the original `path` and scope, runs the same four reports, reports scope drift since this run, and overwrites this file with a fresh assessment. The current `assessment-date` becomes the new `previous-assessment` in the next iteration.
