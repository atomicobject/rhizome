---
name: validate-subsystem
description: Use when adding or modifying validation checks under pkg/validate or their shared human, agent, and CI surfaces in cmd/validation_product.go and cmd/validation_product_runner.go. Loads validation subsystem constraints and review checklist.
---

# Validate subsystem

## Goal

Add or change vault validation checks without breaking the subsystem's contracts: stable check ids, deterministic output, consistent issue shape, soft-skip semantics, max-issues bounds, and batched store access.

## First step

Read `docs/reference/subsystems/validate.md` before any edit or review in `pkg/validate` or the validate cmd surfaces. Treat its design constraints and review checklist as normative — deviations need explicit justification, not silence.

## Load-bearing rules

1. **Register once; derive every surface.** A new check needs its constant plus one `checkRegistry` entry with canonical/CLI names, aliases, suite membership, applicability, projection prerequisites, remediation support, and runner. Selection, `DefaultChecks`, catalog output, human/agent/CI grammar, and dispatch derive from that registry; do not hand-wire CLI help or a second switch. Update the owning validation docs (`docs/specs/process/audits.md` when applicable).
2. **Check ids are a public contract.** Canonical snake_case constants; CLI kebab-case maps via `CanonicalCheck`. Never rename an id — add aliases.
3. **Honor `--max-issues`.** Truncate `CheckResult.Issues` to `RunContext.MaxIssues` (default 20) but set `IssueCount` to the true total.
4. **Soft skips are `Notes`/`Skipped`, never issues.** Unparseable timestamps, missing schema, missing store ⇒ `Notes []string` or `Skipped`+`Summary`; they must not flip `OK` or the exit code.
5. **Deterministic output.** Sort paths/pairs before emitting; `RunSuiteOnce` runs checks concurrently and re-sorts results — within-check order is on you.
6. **Batch store reads; share the runtime.** Path-slice queries (`CurrentNotePropertyValues`, `OntologyEdgesForPaths`), `*WithRuntime` variants for ontology-backed checks, lazy per-note body caching. No per-note round trips in loops; no raw `os` reads outside `RunContext`/`pkg/paths`.
7. **Respect lifecycle and acknowledgement markers.** Skip closed/archived entities where lifecycle docs require (SPEC-0051); when supporting an in-note suppression marker (e.g. `frozen-scope-drift acknowledged` + spec id under `## Deviations`), make it a named constant and document it in `audits.md`.
8. **Fixes carry honest safety tiers** (`safe`/`needs_confirmation`/`agent_required`) and must be idempotent. Product apply passes the reviewed in-memory result to `ApplyRepairSession`; mutating/recovery sessions require the exact-vault refresher, held-lease prepared postcheck, and explicit replan guidance for skipped conflicts or stale preconditions. Do not re-enter ordinary freshness after apply.
9. **Every stable issue key has an exact next step.** Assign stable issue keys first, then enrich by exact key before `BuildRepairPlan`; never join a suggestion by path/code/count inference. Each unresolved key receives a public executable follow-up command or a structured non-fixable reason. `needs_confirmation` actions must replay through the human `rzm validate fix` surface; otherwise classify them `agent_required`. Preserve the caller's original selector and mutation-relevant options when rebuilding next actions after apply.

## Pre-handoff checklist

- [ ] Constraints and review checklist in `docs/reference/subsystems/validate.md` checked against the diff.
- [ ] New check constant and single `checkRegistry` descriptor added; catalog, suite selection, dispatch, applicability, prerequisites, and command surfaces verified as registry-derived; owning docs updated.
- [ ] Table-driven unit tests beside the check (zero findings, findings, soft-skip Notes, max-issues truncation); model: `pkg/validate/frozen_scope_drift_test.go`.
- [ ] Remediation registry covers every emitted issue code; tests assert exact issue-key joins, executable confirmation commands, non-fixable reasons, and original-scope next actions.
- [ ] Tests: `go test ./pkg/validate/...` (plus `go test ./cmd/...` when cmd surfaces changed).
- [ ] Full gate before commit: `make check`.
