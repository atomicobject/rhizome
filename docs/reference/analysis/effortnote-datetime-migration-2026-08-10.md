---
summary: "Exact release-branch plan, apply, convergence, and operator-experience evidence from the Rhizome EffortNote SEQUENTIAL-to-DATETIME migration."
reference-kind: analysis
last-verified: 2026-08-10
code-paths:
  - pkg/validate/identifier_inventory.go
  - pkg/validate/identifier_migration_test.go
  - pkg/app/cli/init/templates/starters/spec-driven/skills/effort-new/SKILL.md
tags: [identifier, migration, validation, ontology, dogfood]
---

# EffortNote DATETIME migration evaluation — 2026-08-10

## Result

The supported repair migrated all 70 EffortNote owners present on `origin/release` from `SEQUENTIAL` to `DATETIME` in one confirmed transaction. Apply reported one transaction applied, none skipped, none failed, and zero remaining issues. The second plan found zero issues. Identifier reconciliation found no collision.

The migration was safe but not hands-off. Two legacy filenames lacked a calendar minute and required governed pre-apply moves. The identifier recovery fix from the original dogfood run is retained on release with regression coverage: an undeclared freeform selector match without a preferred identifier is not admitted to an identifier-gated pool.

## Base and contract

- PR target: `origin/release`
- Rebase target verified before replay: `807957ab18be7a9bc3b90164de8be7c566f1e827`
- Original worktree base verified before the first edit: merge commit `972c4fb171bd1c4db9dae37702ccacd141a734c4`
- Source strategy: `["SEQUENTIAL","EFF","-",4]`
- Target strategy: `["DATETIME","EFF","-",0]`
- Authoritative schema: `.rhizome/ontology/spec-driven.graphql`
- Generated `.agents` and `.claude` copies: unchanged

The shipped contract copies the leading calendar minute from the canonical filename: `YYYY-MM-DD-HH-MM`. It does not consult the current clock, convert time zones, or fall back to Git or filesystem time. `created-at` remains a separate UTC DateTime. Later filename changes do not rederive an existing stable identifier.

## Preserved planning evidence

Exact raw output is preserved outside the worktree:

- incompatible-index failure: `/tmp/rzm-effortnote-release-plan.QExhx3`
- first malformed-path plan: `/tmp/rzm-effortnote-release-plan-green.DB5hSl`
- second malformed-path plan: `/tmp/rzm-effortnote-release-ready-plan.lJRo4d`
- final materialized plan and mapping: `/tmp/rzm-effortnote-release-final-plan.r9X6y5`
- noninteractive apply skip: `/tmp/rzm-effortnote-release-apply.wGKLhF`
- empty second plan: `/tmp/rzm-effortnote-release-idempotence.THp2m7`

The release binary first rejected the worktree's newer main-created SQLite index. The old index was moved to `/tmp/rzm-release-index-backup.PUzD5j`, and the release binary rebuilt its own index. This is local worktree state, not a migration defect.

| Plan pass | Finding | Classification | Fix fingerprint |
|---|---|---|---|
| First release plan | `2026-06-09-client-engagement-tooling.md` lacked a calendar minute | Repository-content issue | `plan:v2:52fdc94049ff6100c0afd4e914864d62f46acc9b7fc9d14f5a019196283a6fd4` |
| Replan after first governed move | `2026-07-11-remove-answer-cards-and-enrich-primary-chunks.md` lacked a calendar minute | Repository-content issue | `plan:v2:a090d44cc1c735c9c9f2c595dd4b9496cf209723e71135d5b2cf3a77583a09cb` |
| Final plan | 70 owner migrations, zero collisions | Ready with confirmation | `plan:v2:979ade5c0caf723c69b50578b623b25bef7801243df18cfa8d07621090451dd8` |

Final plan facts:

- Mapping fingerprint: `a71a58b19c362ffbbe7c9f25513ec741d9202fc6916e4f7a1eecca6599811faf`
- Authority fingerprint: `c2a9891dbaf9d981ca8b8aa387352b719f6bcafd19e54933d3282f4c7f91f1a1`
- Reconciliation fingerprint: `05ce27f47bc5f9884ef4e3bf98e8e145b5f4cceebe0553b8a8e81db0cb44ad1a`
- Actions: 1 `needs_confirmation`; 0 safe; 0 agent-required
- Operations: 81 full-file writes in one transaction
- Owner files: 70
- Additional structured-reference files: 11
- Planned migration file moves: 0
- Identifier collisions: 0
- Review-only warnings: 788
- Git history complete: true
- Git provenance commands: 0, expected because the plan had no collision

### Exact owner mapping

| Owner | Old ID | Proposed ID |
|---|---|---|
| `docs/efforts/2026-04-12-12-00-kb-migration-phase-1.md` | `EFF-0001` | `EFF-2026-04-12-12-00` |
| `docs/efforts/2026-04-24-10-52-answer-engine-search.md` | `EFF-0002` | `EFF-2026-04-24-10-52` |
| `docs/efforts/2026-04-25-20-51-noderef-read-scope.md` | `EFF-0003` | `EFF-2026-04-25-20-51` |
| `docs/efforts/2026-04-25-18-24-noderef-graph-traversal-phase-2.md` | `EFF-0004` | `EFF-2026-04-25-18-24` |
| `docs/efforts/2026-04-26-13-29-noderead-indexed-graph-read-model.md` | `EFF-0005` | `EFF-2026-04-26-13-29` |
| `docs/efforts/2026-04-27-12-04-unified-note-ontology-indexing.md` | `EFF-0006` | `EFF-2026-04-27-12-04` |
| `docs/efforts/2026-04-25-22-28-linkable-embedded-node-identifiers.md` | `EFF-0007` | `EFF-2026-04-25-22-28` |
| `docs/efforts/2026-04-28-22-53-subsystem-docs-code-binding.md` | `EFF-0008` | `EFF-2026-04-28-22-53` |
| `docs/efforts/2026-04-30-13-41-agent-id-allocation.md` | `EFF-0009` | `EFF-2026-04-30-13-41` |
| `docs/efforts/2026-04-30-14-32-lifecycle-rules-tightening.md` | `EFF-0010` | `EFF-2026-04-30-14-32` |
| `docs/efforts/2026-04-30-15-04-frozen-scope-drift-cleanup.md` | `EFF-0011` | `EFF-2026-04-30-15-04` |
| `docs/efforts/2026-04-30-13-31-saved-query-recipes.md` | `EFF-0012` | `EFF-2026-04-30-13-31` |
| `docs/efforts/2026-04-30-18-18-runtime-enriched-query-recipes.md` | `EFF-0013` | `EFF-2026-04-30-18-18` |
| `docs/efforts/2026-04-30-20-31-kb-saved-query-recipes.md` | `EFF-0014` | `EFF-2026-04-30-20-31` |
| `docs/efforts/2026-05-01-11-04-query-recipe-promotion-and-humanization.md` | `EFF-0015` | `EFF-2026-05-01-11-04` |
| `docs/efforts/2026-05-01-16-41-lazy-ac-id-lifecycle.md` | `EFF-0016` | `EFF-2026-05-01-16-41` |
| `docs/efforts/2026-05-01-12-00-usage-driven-block-id-policy.md` | `EFF-0017` | `EFF-2026-05-01-12-00` |
| `docs/efforts/2026-05-01-16-58-heading-rename-safety-and-fragile-external-links.md` | `EFF-0018` | `EFF-2026-05-01-16-58` |
| `docs/efforts/2026-05-02-19-42-typed-semantic-survey.md` | `EFF-0019` | `EFF-2026-05-02-19-42` |
| `docs/efforts/2026-05-02-20-08-nested-spec-dirs-and-id-gated-classification.md` | `EFF-0020` | `EFF-2026-05-02-20-08` |
| `docs/efforts/2026-05-02-22-33-public-api-foundation.md` | `EFF-0021` | `EFF-2026-05-02-22-33` |
| `docs/efforts/2026-05-03-19-18-public-api-dogfooding-and-compatibility-retirement.md` | `EFF-0022` | `EFF-2026-05-03-19-18` |
| `docs/efforts/2026-05-04-14-52-core-configured-table-views.md` | `EFF-0023` | `EFF-2026-05-04-14-52` |
| `docs/efforts/2026-05-05-20-44-non-section-embedded-node-source-shapes.md` | `EFF-0024` | `EFF-2026-05-05-20-44` |
| `docs/efforts/2026-05-06-12-05-configured-view-cell-editing.md` | `EFF-0025` | `EFF-2026-05-06-12-05` |
| `docs/efforts/2026-05-06-17-35-indexed-ontology-node-field-values.md` | `EFF-0026` | `EFF-2026-05-06-17-35` |
| `docs/efforts/2026-05-07-12-16-unified-type-workspace-and-pane-dedupe.md` | `EFF-0027` | `EFF-2026-05-07-12-16` |
| `docs/efforts/2026-05-14-12-33-complex-domain-starter.md` | `EFF-0028` | `EFF-2026-05-14-12-33` |
| `docs/efforts/2026-06-10-14-36-install-init-revamp.md` | `EFF-0029` | `EFF-2026-06-10-14-36` |
| `docs/efforts/2026-06-05-12-31-ontology-aware-search.md` | `EFF-0030` | `EFF-2026-06-05-12-31` |
| `docs/efforts/2026-06-05-13-00-effort-note-frozen-section-inline-display.md` | `EFF-0031` | `EFF-2026-06-05-13-00` |
| `docs/efforts/2026-06-09-10-00-client-engagement-tooling.md` | `EFF-0032` | `EFF-2026-06-09-10-00` |
| `docs/efforts/2026-06-09-18-18-php-language-support.md` | `EFF-0033` | `EFF-2026-06-09-18-18` |
| `docs/efforts/2026-06-11-18-32-skill-forward-legacy-assessment.md` | `EFF-0034` | `EFF-2026-06-11-18-32` |
| `docs/efforts/2026-06-11-18-45-php-indexer-language-coverage-audit.md` | `EFF-0035` | `EFF-2026-06-11-18-45` |
| `docs/efforts/2026-06-12-19-37-notes-workspace-context-signals.md` | `EFF-0036` | `EFF-2026-06-12-19-37` |
| `docs/efforts/2026-06-15-13-51-php-file-scope-call-extraction.md` | `EFF-0037` | `EFF-2026-06-15-13-51` |
| `docs/efforts/2026-06-15-15-46-sparse-graph-fallback-for-assessment-reports.md` | `EFF-0038` | `EFF-2026-06-15-15-46` |
| `docs/efforts/2026-06-19-14-48-starter-management-ejection.md` | `EFF-0039` | `EFF-2026-06-19-14-48` |
| `docs/efforts/2026-07-11-16-30-remove-answer-cards-and-enrich-primary-chunks.md` | `EFF-0040` | `EFF-2026-07-11-16-30` |
| `docs/efforts/2026-07-11-21-37-intel-persistence-and-observability-simplification.md` | `EFF-0041` | `EFF-2026-07-11-21-37` |
| `docs/efforts/2026-07-11-21-37-config-and-init-compatibility-retirement.md` | `EFF-0042` | `EFF-2026-07-11-21-37-2` |
| `docs/efforts/2026-07-11-21-37-agent-tool-and-runtime-consolidation.md` | `EFF-0043` | `EFF-2026-07-11-21-37-3` |
| `docs/efforts/2026-07-11-21-37-graph-and-ontology-read-path-consolidation.md` | `EFF-0044` | `EFF-2026-07-11-21-37-4` |
| `docs/efforts/2026-07-14-12-02-robust-ui-data-lifecycle.md` | `EFF-0045` | `EFF-2026-07-14-12-02` |
| `docs/efforts/2026-07-15-19-43-validation-projection-markdown-targets.md` | `EFF-0046` | `EFF-2026-07-15-19-43` |
| `docs/efforts/2026-07-15-19-43-transactional-validation-repair-engine.md` | `EFF-0047` | `EFF-2026-07-15-19-43-2` |
| `docs/efforts/2026-07-15-19-43-identifier-strategy-reconciliation.md` | `EFF-0048` | `EFF-2026-07-15-19-43-3` |
| `docs/efforts/2026-07-15-19-43-validation-ci-product-surface.md` | `EFF-0049` | `EFF-2026-07-15-19-43-4` |
| `docs/efforts/2026-07-15-19-43-validation-repair-operations.md` | `EFF-0050` | `EFF-2026-07-15-19-43-5` |
| `docs/efforts/2026-07-15-19-43-validation-initiative-integration-hardening.md` | `EFF-0051` | `EFF-2026-07-15-19-43-6` |
| `docs/efforts/2026-05-06-12-15-core-action-items-template-dependencies.md` | `EFF-0052` | `EFF-2026-05-06-12-15` |
| `docs/efforts/2026-05-07-19-01-first-launch-index-gate.md` | `EFF-0053` | `EFF-2026-05-07-19-01` |
| `docs/efforts/2026-05-13-12-51-note-node-indexing-architecture.md` | `EFF-0054` | `EFF-2026-05-13-12-51` |
| `docs/efforts/2026-06-05-12-30-documentation-overhaul-and-ontology-aware-search.md` | `EFF-0055` | `EFF-2026-06-05-12-30` |
| `docs/efforts/2026-06-17-15-54-ontology-note-base-and-migration-validation.md` | `EFF-0056` | `EFF-2026-06-17-15-54` |
| `docs/efforts/2026-07-17-12-53-evidence-grounded-release-orchestration.md` | `EFF-0057` | `EFF-2026-07-17-12-53` |
| `docs/efforts/2026-07-18-12-59-typescript-javascript-language-support.md` | `EFF-0058` | `EFF-2026-07-18-12-59` |
| `docs/efforts/2026-07-20-14-43-canonical-release-ref-publication.md` | `EFF-0059` | `EFF-2026-07-20-14-43` |
| `docs/efforts/2026-07-20-14-43-universal-pinned-command-delegation.md` | `EFF-0064` | `EFF-2026-07-20-14-43-2` |
| `docs/efforts/2026-07-22-14-14-agent-start-contained-optimization.md` | `EFF-0065` | `EFF-2026-07-22-14-14` |
| `docs/efforts/2026-07-22-14-15-agent-start-capability-fast-path.md` | `EFF-0066` | `EFF-2026-07-22-14-15` |
| `docs/efforts/2026-07-22-21-30-agent-start-indexed-enrichment.md` | `EFF-0067` | `EFF-2026-07-22-21-30` |
| `docs/efforts/2026-07-29-18-12-agent-file-context-performance.md` | `EFF-0069` | `EFF-2026-07-29-18-12` |
| `docs/efforts/2026-08-05-11-33-agent-semantic-query-performance.md` | `EFF-0070` | `EFF-2026-08-05-11-33` |
| `docs/efforts/2026-08-05-18-50-agent-semantic-query-deep-performance.md` | `EFF-0071` | `EFF-2026-08-05-18-50` |
| `docs/efforts/2026-08-05-20-45-semantic-query-pr-review-corrections.md` | `EFF-0072` | `EFF-2026-08-05-20-45` |
| `docs/efforts/2026-08-05-21-14-agent-cli-runtime-capability-planning.md` | `EFF-0073` | `EFF-2026-08-05-21-14` |
| `docs/efforts/2026-08-06-12-20-identifier-strategy-migration.md` | `EFF-0074` | `EFF-2026-08-06-12-20` |
| `docs/efforts/2026-08-05-11-31-datetime-identifier-strategy.md` | `EFF-0075` | `EFF-2026-08-05-11-31` |

### Structured reference writes

The plan exposed 81 full-file write operations: the 70 owners above and these 11 additional structured-reference files:

- `docs/reference/analysis/agent-start-performance-2026-07-22.md`
- `docs/reference/analysis/php-coverage-2026-06-12.md`
- `docs/reference/guides/php-indexer-coverage.md`
- `docs/specs/product/indexing-workflow.md`
- `docs/specs/product/search-answer-workflow.md`
- `docs/specs/technical/linkable-embedded-node-identifiers.md`
- `docs/specs/technical/php-language-support.md`
- `docs/specs/technical/primary-semantic-chunks-and-noderef-search.md`
- `pkg/app/cli/init/templates/skills/markdown/legacy-codebase-assessor/SKILL.md`
- `pkg/app/validationproduct/CONTEXT.md`
- `pkg/app/validationrun/CONTEXT.md`

The 788 plain-text and source-code mentions were reported as review-only and were not edited.

## Manual intervention before apply

No ID, alias, descendant identifier, or reference was manually pre-converted.

1. `rzm note move` changed `2026-06-09-client-engagement-tooling.md` to `2026-06-09-10-00-client-engagement-tooling.md`. The source was its canonical `created-at: 2026-06-09T10:00:00Z`. The command updated zero links.
2. `rzm note move` changed `2026-07-11-remove-answer-cards-and-enrich-primary-chunks.md` to `2026-07-11-16-30-remove-answer-cards-and-enrich-primary-chunks.md`. The source was `created-at: 2026-07-11T16:30:00Z`. The command updated three links.

The move surface reported `git history preserved: false` for both. Git rename detection is checked at commit review; the content is otherwise unchanged apart from the migration.

## Apply evidence

The noninteractive agent surface planned all 81 writes but skipped the connected transaction because its safety tier was `needs_confirmation`. It reported `plannedTransactions: 1`, `plannedWrites: 81`, and `remainingFindings: 70`.

The supported human surface then asked:

`Apply the reviewed whole-pool identifier strategy migration? [y/N]`

After explicit confirmation it reported:

- `applied=1`
- `skipped=0`
- `failed=0`
- `remaining=0`
- post-validation: 0 issues, 0 errors
- apply duration: 18.309 seconds
- post-validation duration: 630 milliseconds

The second plan reported 369 identifier nodes checked, zero issues, zero actions, and the same collision-free reconciliation fingerprint.

## Evaluation

| Dimension | Evidence | Assessment |
|---|---|---|
| Uniqueness | 70 proposed IDs, 70 unique values, zero collisions | Pass |
| Determinism | Mapping derives only from canonical path and prior ordinal; the second plan is empty | Pass |
| Timestamp source and format | Every target copies the path's leading valid calendar minute | Pass |
| Same-minute behavior | Three families use canonical unsuffixed base then `-2` onward | Pass |
| Links and references | 11 non-owner structured-reference files were rewritten in the transaction | Pass for structured identity |
| Plain text and code mentions | 788 review-only findings remained unchanged | Safe but noisy |
| Filename behavior | Migration planned and made zero moves | Pass |
| Git provenance | No collision required Git provenance; history completeness remained true | Pass for migration |
| Idempotence | Second plan: 0 issues and 0 actions | Pass |
| Mixed legacy handling | Invalid paths blocked all apply and surfaced one at a time | Safe; serial discovery is rough |
| Operator clarity | Exact mapping, fingerprints, safety tier, transaction, postcheck, and warnings are visible | Strong core; output is too large |

Same-minute assignments:

- `EFF-2026-07-11-21-37` through `-4`
- `EFF-2026-07-15-19-43` through `-6`
- `EFF-2026-07-20-14-43` through `-2`

## Product defects and rough edges

### Product defect corrected

Identifier strategy recovery could cross an identifier-gated classification boundary and admit an undeclared freeform selector match with no preferred identifier. Recovery now batches preferred properties and excludes that candidate. An explicitly typed member with a missing identifier still blocks. Focused regression coverage passes.

### Repository-content issues corrected

Two date-only legacy effort filenames had no materializable DATETIME source. Their canonical `created-at` minute supplied the governed filename repair. These are repository-content issues, not identifier-engine defects.

### Rough edges

1. Blocked planning reports only the first malformed pool member.
2. The human plan emits 788 review records inline, which overwhelms the useful summary.
3. The agent apply surface accepts `--apply` but silently skips a confirmation-tier transaction; the human surface is required.
4. The note-move message says Git history was not preserved even when the final commit may detect a rename.
5. The advertised managed `rhizome` skill is absent from this worktree even though agent skills are enabled; the checked-in Rhizome guidance and launcher remain usable.
6. Repository-local generated `effort-new` skill copies still describe the pre-migration sequential workflow. Their authoritative managed source is current, but the repository contract forbids editing generated copies; regeneration must occur through the supported init workflow after the new binary is available.

## Recommended follow-up

1. Aggregate malformed pool members in one blocked preflight.
2. Add compact or exportable review-only output.
3. Make agent apply state clearly say that confirmation-tier work requires the human surface.
4. Clarify note-move Git-provenance reporting.
5. Restore or explain the missing managed `rhizome` skill in this repository's active harness.
6. Regenerate repository-local managed agent skills after this release so active harness copies pick up the DATETIME effort workflow.

## Validation

Final release-rebase evidence:

- focused `pkg/ontology`, `pkg/validate`, identifier reconciliation, and starter-template tests: pass
- `make build`: pass
- `make check`: pass, including race-enabled Go tests, integration packages, web lint/tests/typecheck, and Greptile policy check
- `./scripts/rzm validate`: pass; ontology, identifiers, and broken links clean
- `./scripts/rzm agent validate all --max-issues 200 --no-suppress`: pass; 10 checks, zero issues and errors
- explicit agent identifiers validation: pass; 370 identifier nodes, zero issues, zero collisions
- second identifiers fix plan: pass; zero issues and actions
- `git diff --check origin/release...HEAD`: pass
