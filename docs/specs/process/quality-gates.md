---
aliases:
    - SPEC-0009
id: SPEC-0009
last-updated: 2026-07-15T00:00:00Z
spec-status: archived
summary: Defines the local and CI quality gates expected before implementation work is considered ready.
type: ProcessSpec
---
<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->
This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.


# Quality gates

## Summary

This spec defines the quality gates this repository expects before merge or handoff. It is intentionally repository-owned: each repo should tailor this file to its actual build system, test stack, CI policy, and risk tolerance.

## Goals

- make quality checks repeatable for humans and agents
- require formatting, validation, and tests instead of relying on chat summaries
- keep CI and local workflows aligned
- give workflow skills one source of truth instead of hard-coding commands like `make check`

## Non-Goals

- prescribing a single linter, formatter, or test runner
- replacing repo-specific quality rules with generic prose

## Requirements

### Must

- Repositories define a canonical local quality-check command surface in this file or a linked repo-specific checklist, whether via scripts, task runners, make targets, or another documented entrypoint.
- Agents run the relevant local quality gates before handoff.
- Agents do not claim work is fixed, passing, or complete without fresh verification evidence from the relevant local command surface.
- Formatting is part of the expected quality gate.
- Test execution is part of the expected quality gate for behavior-changing work.
- CI, when present, runs required quality gates before merge.
- Repositories using Rhizome content validation run `rzm ci` on pull requests in addition to repository build/test gates; `rzm ci` is not a replacement for `make check` or equivalent.
- `rzm ci` runs the same repository-configured default suite as bare `rzm validate`, never mutates notes, and fails on findings or blocked applicable prerequisites.
- `rzm ci` exits `0` when clean, `1` for validation findings, and `2` for configuration, execution, or applicable-prerequisite failure.
- CI findings include stable issue codes, affected paths, and exact `rzm validate fix <suite-or-check>` guidance when remediation exists.
- Pull-request workflows must run for child PRs targeting an integration branch, not only PRs whose base is `main`.
- The canonical local command surface is documented in repo docs rather than left implicit in chat or tribal knowledge. Skills must read this policy instead of assuming a universal command.
- When behavior, docs, ontology, or retrieval surfaces changed, agents run the relevant `rzm agent validate` checks before handoff.
- Agents treat validation findings like failing tests: apply safe deterministic fixes, fix current in-scope issues proactively, and rerun validation before handoff.
- Agents classify any validation findings left after safe fixes as `needs decision`, `historical/frozen`, `unrelated/pre-existing`, or `out of scope`.
- Agents do not rewrite `complete` or `archived` effort history to resolve validation noise unless explicitly asked; historical findings are reported as such and routed to follow-up work when needed.
- Historical cleanup authority uses the explicit `--allow-historical` repair override and is recorded in the owning effort; CI never uses that override.
- Quality-gate reports explicitly mark checks as `deferred` when the repo documents them as not run, blocked, or not yet implemented.

This repository's key quality gates:

- `make check-fast` - formatting, vet, lint, and types for the edit loop
- `make check` - `check-fast` plus unit tests; the pre-commit gate
- `make check-full` - adds the race detector, integration tests, and benchmark contracts; CI runs this coverage on every pull request
- `make web-e2e` - end-to-end web tests for when changes impact the web experience

### Should

- CI and local quality gates use the same named commands where practical.
- Agents inspecting failed Rhizome CI should be able to run the emitted repair command directly, replan stale transactions, and report any remaining semantic decision instead of rediscovering check scope.
- Quality gates include linting, type checks, or static validation when the repo's stack supports them.
- Repositories expose one command that runs the common pre-merge checks in canonical order.
- Verification evidence includes the exact command or named entrypoint that was run, not only a chat summary of success.
- Validation evidence includes the applied safe-fix command when one ran and the classification of any remaining findings.

### May

- Add advisory checks for dependency health, performance, accessibility, or deployment readiness.

## Open Questions

- which additional checks should be treated as blocking in this repository
