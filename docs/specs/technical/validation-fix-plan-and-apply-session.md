---
type: TechnicalSpec
summary: "Defines validation suites, deterministic repair planning, transactional apply, remediation metadata, CI rendering, and post-apply convergence."
id: SPEC-0018
spec-status: active
last-updated: 2026-09-23
aliases:
  - SPEC-0018
  - Validation fix plan and apply session
---

# Validation fix plan and apply session

## Summary

Validation needs a stable technical contract that separates issue detection from repair execution.

The backend contract should support three consumers with different trust boundaries:

- CLI and automation need deterministic safe-fix execution with no prompts
- the web app needs grouped repair metadata plus a replayable apply-session path
- agent-guided workflows need explicit non-safe repair classification and enough ambiguity context to continue from the validation result

This spec defines the shared check registry, suite selection, repair-plan payload, repair transaction model, deterministic-fix constraints, CI contract, and apply-session model used across CLI, agent, web, and automation surfaces.

## Goals

- make validation output the authoritative source for repair planning
- keep safe non-interactive repair deterministic and idempotent
- support grouped repair actions that represent one logical change across many issue instances
- let the browser turn selected repair actions into a fresh previewable apply session
- preserve a clean escalation path for ambiguous repairs
- keep validation and CI fast through a metadata/ontology/Markdown-target projection rather than full project indexing
- let independent repair transactions proceed when another transaction is stale or ambiguous without permitting partial semantic repairs

## Non-Goals

- forcing every repair into an ontology edit primitive before the browser workflow can ship
- storing long-lived approval memory for suggested fixes
- implementing LLM-authored repair prose in the deterministic path
- collapsing all validation issue types into one generic fuzzy repair engine
- treating every registered maintenance or migration diagnostic as part of the comprehensive health suite
- rebuilding code indexes, graph scores, chunks, or embeddings as a validation side effect

## User Stories

### US1 - Apply independent repair transactions atomically with explicit preconditions
- id:: ^SPEC-0018-US1
- summary:: Apply independent repair transactions atomically with explicit preconditions, conflict isolation, and complete execution reporting.
- status:: satisfied

#### Acceptance Criteria

- A repair plan has stable issue/action identities, deterministic ordering, a plan fingerprint, source hashes, expected spans or text, affected identities, complete affected-path metadata, explicit write/rename/delete operation kinds, and lifecycle-policy decisions. ^SPEC-0018-US1-AC1
- Repair operations are grouped into connected repair transactions by shared files and identities; content writes, renames, and deletes in each transaction are staged and committed atomically. ^SPEC-0018-US1-AC2
- A stale precondition, edit overlap, destination collision, ambiguity, or lifecycle-protected edit skips only the connected transaction and independent transactions may still apply. ^SPEC-0018-US1-AC3
- Staged writes preserve file modes. In-process failure leaves the complete transaction committed or the originals restored; process interruption leaves a durable journal that the next validation/repair invocation must recover before doing new work. Recovery failures block the transaction and retain explicit evidence. ^SPEC-0018-US1-AC4
- Apply refreshes the exact changed, renamed, or deleted paths and reruns the targeted checks before returning. ^SPEC-0018-US1-AC5
- The execution report distinguishes applied, skipped ambiguous, failed stale/conflict, and remaining findings and includes the exact replan command. ^SPEC-0018-US1-AC6

### US2 - Select applicable validation suites and render a read-only CI result
- id:: ^SPEC-0018-US2
- summary:: Select applicable validation suites from one registry and render the same read-only result for local validation and CI.
- status:: satisfied

#### Acceptance Criteria

- Check descriptors declare stable public name/aliases, suite membership, applicability, required projection domains, missing-prerequisite outcome, and remediation support. ^SPEC-0018-US2-AC1
- `validation.default.add/skip` and `validation.all.add/skip` compose deterministic sets; unknown checks, add/skip overlap, a `skip` name absent from the built-in set plus that overlay's `add` list, or an empty effective set fail configuration. `audit` has fixed membership this increment. ^SPEC-0018-US2-AC2
- Each invocation accepts exactly one selector: omitted/`default` expands configured default, `all` expands configured all, `audit` expands the fixed audit suite, and a named check bypasses configured composition; mixing selectors is invalid. ^SPEC-0018-US2-AC3
- `rzm ci` uses a scratch validation projection on process-local storage, never mutates notes, supports mutually exclusive `github` annotation and `json` renderers, and exits 0 clean, 1 findings, or 2 execution/configuration/prerequisite failure. ^SPEC-0018-US2-AC4
- Applicable checks with missing or unusable persisted prerequisites return blocked plus the exact preparation command and exit 2 in CI; absent features return not applicable. Prerequisite probing never walks code/project roots: it uses configured capability, row presence, indexer version, scope-config hash, indexed-file count, and stored index timestamp, and discloses that it does not prove live-worktree freshness. ^SPEC-0018-US2-AC5
- Missing-note, missing-heading, and missing-block failures are distinct from code-anchor checks and never receive a self-retargeting or no-op fix. ^SPEC-0018-US2-AC6

### US3 - Classify every validation issue through a remediation registry
- id:: ^SPEC-0018-US3
- summary:: Classify every validation issue through a remediation registry that replaces generic unclassified guidance.
- status:: satisfied

#### Acceptance Criteria

- Each issue code maps to a fix builder and safety class, an exact command or agent action, prerequisites and postchecks, or an explicit non-fixable reason. ^SPEC-0018-US3-AC1
- Existing ontology fix suggestions for declared-type and inverse mismatches flow through the same registry and transactional engine. ^SPEC-0018-US3-AC2
- Code-frontmatter, code-anchor, query-recipe, orphan-block, and missing-section diagnostics retain exact paths/codes and deterministic candidate ordering. ^SPEC-0018-US3-AC3
- Missing-section structural repairs use schema-aware placement and never append a heading at an invalid nesting or order position. ^SPEC-0018-US3-AC4
- Agent next actions preserve the selected suite/check scope and use stable issue keys rather than instance-count arithmetic. ^SPEC-0018-US3-AC5

### US4 - Integrate the validation initiative and finish with a clean core suite
- id:: ^SPEC-0018-US4
- summary:: Integrate projection, repair, identifier, CI, and audit-repair tracks and finish with a clean unskipped core suite.
- status:: satisfied

#### Acceptance Criteria

- Shared contracts land before dependent child changes, and integration tests exercise validation refresh, plan/apply, identifier reconciliation, and CI selection together. ^SPEC-0018-US4-AC1
- The repository's pre-existing ontology and duplicate-identifier findings are resolved after the repair machinery exists; temporary development skips are removed before delivery. ^SPEC-0018-US4-AC2
- Final validation reports zero ontology, identifier, and broken-link findings with no core-check skips or per-finding baselines. ^SPEC-0018-US4-AC3
- Full repository gates and the configured Rhizome CI suite pass on the integrated parent head. ^SPEC-0018-US4-AC4

## Requirements

### Must

- Validation MUST return a stable repair-plan payload when repairable or reviewable actions exist.
- Each repair action MUST include a stable identifier, source check, safety class, summary metadata, affected paths, and the exact edit or replay payload it represents.
- Each issue MUST carry a stable issue key independent of output truncation or instance-count arithmetic.
- The same vault state MUST produce stable repair action identities and payload ordering.
- Safe deterministic repair MUST be limited to edits whose target values and insertion points are uniquely derivable from the current vault state.
- Agent/non-interactive `rzm validate fix ... --apply` MUST skip suggested and agent-required actions without prompting, unless the caller passes an exact reviewed selection.
- An exact reviewed selection (`--apply --action <action-id-or-issue-key>`, repeatable, or `--apply --from-plan <file>`) MUST apply exactly the actions whose ID or stable issue key it lists, regardless of safety tier, and no others. It MUST reject entries that match no action in the current plan and any selected `agent_required` action before lock acquisition or mutation, and MUST keep every source, fingerprint, lifecycle, and journal precondition.
- Grouped repair actions MUST deduplicate repeated issue instances when one logical repair can address them together.
- Broken-link grouping MUST operate on normalized missing target plus normalized candidate target so repeated dead-link cases collapse cleanly.
- The backend MUST preserve enough metadata for the web app to show grouped counts, candidate targets, questions or rationale, and affected-path expansion.
- The server MUST provide an apply-session path that can take selected repair actions and stage them for preview before commit.
- The apply-session path MUST reject, skip, or force revalidation for stale actions whose source conditions no longer match the vault state.
- Repair plans MUST distinguish a validation check, a semantic repair operation, and an atomic repair transaction.
- Repair transactions MUST be connected components over affected files and identities; no operation in an in-process failed transaction may remain applied.
- Apply MUST use source hashes and expected span/text preconditions, detect edit overlap and destination collisions, stage writes, preserve file modes, and return a complete execution report even when work is skipped or fails.
- Multi-file apply MUST write a recoverable transaction journal before the first destination replacement and MUST recover or explicitly block an interrupted transaction before any later validation or repair run.
- The ontology EditSession adapter MUST use `Preview`/`PreviewCurrent` replay only to compute updated content and typed conflicts; it MUST NOT call `Commit`, and the repair transaction engine MUST be the only disk writer.
- CLI and web repair MUST converge on the ontology edit-session replay/conflict semantics for source-preserving structured edits instead of maintaining contradictory mutation rules.
- Plan fingerprints MUST hash canonical serialization of stable issue/action keys, operation kinds, sorted normalized vault-relative paths, and raw on-disk source bytes; checkout-specific line-ending conversion therefore produces a different source precondition rather than a cross-host false match.
- Validation selection MUST derive from one registry and MUST support `default`, `all`, `audit`, and explicit check selection.
- The built-in core default MUST contain ontology, identifiers, and broken internal note/heading/block targets.
- The audit suite MUST remain an explicit maintenance/migration superset and MUST NOT be implied by `all`.
- Repository config MUST use strict known fields and independently compose `validation.default` and `validation.all` from `add` and `skip` lists.
- Checks MUST declare execution-surface support and persisted prerequisites. The same configured default remains valid locally; a selected check unsupported by scratch CI MUST return blocked there instead of becoming a global configuration error or causing CI to build code, code-anchor, project, embedding, chunk, or graph domains.
- Validation and repair MUST refresh only their required validation projection before checks and exact affected paths after apply; they MUST NOT imply full project-index freshness.
- Check results MUST represent completed, not applicable, and blocked states without silently skipping explicitly requested checks.
- CI MUST be read-only and use the same configured default selection as bare local validation.
- CI renderers MUST keep JSON stdout machine-valid; GitHub workflow annotations MUST be emitted only by the separate `github` renderer.
- Human `--apply` MAY prompt for confirmation-needed actions; agent apply MUST never prompt and MUST return structured questions instead.
- `--allow-historical` MUST bypass only lifecycle protection and MUST remain subject to all other safety rules.
- Repairs allowed by the lifecycle contract, such as a destination-preserving broken-link rewrite inside a closed effort, MUST NOT require `--allow-historical`; edits to protected historical fields or sections MUST be skipped unless the override is explicit.

### Should

- Safe deterministic fixers should emit the exact edits they plan to apply before execution.
- The repair-plan model should support mixed execution backends, including ontology edit operations and deterministic file-oriented edits, behind one review lifecycle.
- Missing-section scaffolds should be limited to explicit required headings with deterministic placement and no invented substantive content.
- Agent-required actions should include enough explanation to seed a follow-on ontology-aware workflow.
- Plan generation should scan the vault once per requested suite and batch structured store/Git reads rather than repeating whole-vault work per issue.

### May

- Suggested repairs may include user-facing question text or candidate ranking data for the browser even when CLI ignores them.

## Documentation Plan

- Document repair operation/transaction semantics, preconditions, safety classes, result statuses, and exit codes in validation subsystem and public API references.
- Document validation projection prerequisites/freshness in indexing guidance and config set composition in CLI/vault-core guidance.
- Update README, MCP/agent context, and GitHub Action examples when the public command surface changes.

## Open Questions

None for this increment. Mixed file-oriented repairs adapt to the ontology edit-session conflict semantics, and safety class remains the authoritative automation boundary; confidence scoring is deferred.
