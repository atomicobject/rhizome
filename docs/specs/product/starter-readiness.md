---
type: ProductSpec
id: SPEC-0138
summary: Installed starters become usable through automatic setup and index refresh, with persistent readiness and actionable recovery in the desktop.
spec-status: active
last-updated: 2026-11-10
aliases: [SPEC-0138]
---

# Starter readiness

## Summary

Installing a starter must leave the person with a working feature or an explicit explanation of what prevents it from working. Writing its files is a separate milestone from completing its setup, publishing its indexed data, and proving its required queries work.

Rhizome uses one readiness contract for desktop installations, installations made through the CLI while the desktop is open, and reopening a repository with unfinished setup. Existing module actions and runtime indexing remain the execution mechanisms. This extends [[init-starter-workflow]] and [[desktop-repository-setup]] without making non-interactive `init` itself run project compilers or indexing.

## Goals

- Make safe, unambiguous setup and index refresh happen automatically after installation.
- Keep unfinished setup visible until it succeeds or the person removes the starter.
- Explain conflicts, missing prerequisites, and required choices with useful recovery actions.
- Recover from relevant repository changes without repeated identical failing work.

## Non-Goals

- Automatically delete or rename existing repository schemas or knowledge content.
- Run ingestion, curation, generation, or publishing of AO-authored knowledge.
- Add a second index scheduler or bypass repository trust.
- Rebuild code and embeddings after every unchanged `init` run.
- Change the selected Rhizome version or executable without the existing installation flow.

## User Stories

### US1 - Install a starter and reach a usable state

- id:: ^SPEC-0138-US1
- summary:: A person installs a starter and Rhizome completes the setup that needs no additional decision.
- status:: ready

#### Acceptance Criteria

- Installing through repository settings, changing files through the CLI with an open runtime, and opening a repository with unfinished setup all discover the same readiness needs.
- Compatible, trusted starters complete deterministic setup and publish the required index data without a terminal command.
- AO KB single-product setup can select its unambiguous single-product mode. Multiple eligible products require an explicit choice.
- Readiness is established by the starter's checks and required query/index evidence, not file presence or a nonzero record count. A legitimately empty KB can be ready.
- Repeated observation of unchanged, ready inputs does not repeat setup or indexing.

### US2 - Understand and recover unfinished setup

- id:: ^SPEC-0138-US2
- summary:: A person sees the current setup state and can resolve a blocker or retry a failed step.
- status:: ready

#### Acceptance Criteria

- Checking, working, ready, required-input, and failure outcomes have distinct representations.
- The desktop keeps unfinished setup visible across page navigation and reopening the repository. It shows the starter and the actual reason.
- Recovery offers the action appropriate to the outcome, including retry, selecting a product, reviewing a schema conflict, or the existing trust/installation flow.
- Invalid authored SDL and executable-query-schema failures both prevent a ready result and identify their cause.
- Fixing relevant files causes a new check and resumes eligible work. An unchanged failure does not create a retry loop.
- Starter subprocesses use the executable selected for the running repository; an accidental PATH lookup cannot substitute an incompatible binary.
- No recovery action silently changes user-authored schemas or knowledge content.

### US3 - Refresh after ontology folders change

- id:: ^SPEC-0138-US3
- summary:: A person adds, removes, or replaces an ontology folder and Rhizome refreshes the affected typed data.
- status:: ready

#### Acceptance Criteria

- Folder-only events for the flat ontology folder, modules root, valid module folder, and module ontology folder trigger ontology refresh and capability invalidation.
- Non-ontology generated state and invalid module names remain filtered.
- A folder removal or replacement follows the existing full ontology publication path and notifies clients.

## Requirements

- Use one source of readiness truth shared by its runtime, API, and desktop consumers.
- Reuse existing action trust, process bounds, cancellation, runtime indexing, and global event ownership.
- Serialize or coalesce concurrent setup requests for the same repository and starter. A restart must recheck current inputs before claiming success.
- Required setup inputs and failures survive until resolved through durable state or deterministic reconstruction from repository facts.
- Report setup failure separately from ordinary validation findings in existing content.
- Preserve access to other usable repository views while a starter is blocked.
- Include focused regression tests for conflict, missing input, successful setup, unchanged reruns, runtime reopen, cancellation/failure, and ontology folder events. Verify the visible recovery flow with a real fixture repository.

## Open Questions

None blocking the approved scope. The implementation design chooses the smallest shared readiness contract that reuses existing action and index owners.
