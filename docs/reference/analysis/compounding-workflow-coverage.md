---
type: ReferenceDoc
summary: "A focused regression study of Markdown and workflow eligibility in compounding, with anonymized examples and isolated replay precautions."
reference-kind: analysis
last-verified: 2026-11-03
status: active
---

# Compounding workflow coverage

A mining run examined Markdown but held every workflow candidate before verification. The run processed 37 sessions and 457 episodes, extracted 76 candidates into 67 clusters, and proposed none. All 36 verified clusters cited Markdown. The gap was the unscreened fallback: it treated a workflow subject as insufficient evidence, even when a guide or schema could confirm the claim.

The fix separates **subject** from **checkability**. The grouper now identifies repository evidence versus external evidence. Code, Markdown guides, schemas, templates, and skills can all support verification. Normal Jev ranking keeps its existing question and threshold. The fallback accepts repository-backed claims and the legacy `code` label; old `workflow` and `environment` labels alone remain insufficient. Unclassified grouper omissions also remain held.

This is an eligibility correction, not a target for more proposals. Verification still checks provenance, present-day truth, duplication, specificity, and whether a useful improvement remains. Existing documentation can resolve a knowledge claim while leaving an independently evidenced delivery or enforcement problem to investigate.

## Current-source disposition review

A private, read-only review examined all 17 held workflow candidates against current source. Fourteen were already covered, two suggested a documentation improvement, and one warranted a tool investigation. Two additional negative controls remained rejected. These are manual dispositions, not model replay results or permission to publish changes to the source vault.

The table uses synthetic descriptions. The private evidence ledger retains the original IDs, provenance, source paths, line references, and hashes; raw sessions and private titles are excluded from this repository.

| Case | Synthetic description | Disposition | Evidence used |
| --- | --- | --- | --- |
| 1 | A personal knowledge repository stays on its current branch while product code uses worktrees | Candidate document change | Human-attributed correction with likely confidence; no explicit convention found in the current entrypoint |
| 2 | Moving a linked note changes historical display labels and leaves repeated targets | Tool investigation | Current rename call path and existing behavior tests; no new runtime reproduction |
| 3 | A title search misses an existing concept expressed with different wording | Already documented | Authoring entrypoint plus conceptual-search guide |
| 4 | A generated report repeats local claims and is mistaken for independent support | Already documented | Provenance warning at the report's point of use |
| 5 | An agent edits an installed skill instead of its canonical source | Already documented | Generated-file manifest and product-change route |
| 6 | Two notes need content consolidation before a destination is overwritten | Candidate document change | Mutation guide lacks a consolidation recipe; overwrite is not semantic merging |
| 7 | Ordinary substantial work still belongs in the daily log on weekends | Already documented | Daily-work guidance with a scoped automation exception |
| 8 | An exploratory idea is prematurely treated as a quarterly commitment | Already documented | Commitment schema and capture guide |
| 9 | An activity count requires human classification and multiple evidence sources | Already documented | Reporting workflow and reconciliation guide |
| 10 | A personal commitment schema should not gain an unnecessary owner field | Already documented | Complete current schema and its ownership context |
| 11 | A shared document should follow its designated source note | Already documented | The planning package's source-of-truth boundary |
| 12 | An optional next-step field causes invented work or reminders | Already documented | Optional schema field, approval requirement, and capture guide; whether to retain the field remains open |
| 13 | Executive reporting needs topic selection before drafting | Already documented | Interactive preparation workflow and its explicit exceptions |
| 14 | Prior report categories are mistaken for permanent mandatory headings | Already documented | Current writing guide treats them as examples |
| 15 | A filename-only rename leaves inbound links behind | Already documented | Graph-aware mutation route |
| 16 | A content map becomes a large draft rather than contextual links | Already documented | Current content-map guidance |
| 17 | Detailed source notes become an overly detailed executive update | Already documented | Current brevity guidance and accepted-example instructions |
| Control A | A candidate repeats the reviewer identity requirement | Reject as redundant | Review skill already gives the rule and recovery command |
| Control B | An old publication filter is described as current | Reject as obsolete | Current build behavior and publication guide contradict it |

The tool investigation does not authorize globally preserving every old label or deduplicating every repeated link. Both can be intentional. The smallest follow-up is an isolated reproduction followed by a product decision about which context needs preservation.

## Six regression shapes

1. **Concept discovery:** a literal title search misses a synonymous concept. Admit the candidate to verification, then reject duplicate memory if the authoring guide already supplies the search route. A missing entrypoint link may justify a document change only when current evidence confirms it.
2. **Historical labels:** a dated log links an old title. A move changes the visible wording. Trace the actual mutation route before deciding whether a fix is still needed.
3. **Executive brevity:** a person asks for a shorter update and requests that accepted examples guide future writing. Preserve `person/likely` provenance; check the current writing guide before proposing anything.
4. **Optional next step:** a delegated brief of unknown origin quotes a preference, while a separate parent correction has likely human attribution. Keep these distinct and preserve an unresolved question about retaining the field.
5. **Identity control:** a candidate repeats an existing review rule. Keep rejecting it.
6. **Obsolete-behavior control:** an old report describes a removed publication filter. Keep rejecting it despite recurrence.

Synthetic mining tests exercise both `screen → extract → group → rank → verify` and `extract → group → verify` in unscreened mode. They check admission, Markdown/schema evidence, source confidence, negative controls, and a missing versus intact documentation entrypoint. Fake clients prove pipeline behavior; they do not measure prompt interpretation by a model.

## Isolated replay procedure

The development packet contains 14 episodes. A further 21 episodes cover the other reviewed claims, but they are not a blind holdout because their claims informed development. A future untouched window is needed before claiming general quality improvement.

1. Capture the source vault with its disposable-copy helper or equivalent explicit allowlist. Omit original Git metadata, runtime/index/state, editor settings, environment files, and credentials. Preserve symlinks without dereferencing them.
2. Remove executable pins and embedding-provider configuration from the copy, disable embeddings, and create a private unsigned Git commit. The helper's empty `git init` alone is insufficient: verification reads tracked files.
3. Freeze the module, input episode file, and captured-source hashes. Use identical inputs and source for both variants and fresh private state for every repeat.
4. Set an absolute `RZM`, `RZM_SKIP_REPO_DELEGATE=1`, explicit `--episodes`, and explicit `--checkout`. Run only against the copy. Resolve symlinks in script paths because the current direct-entry guard requires their physical path.
5. Preview without a budget, record bindings and estimated stages, and obtain the required paid-run approval. An estimate is not a quality result.
6. Score whether each known case was extracted, grouped, admitted, and correctly resolved. Record evidence requests, provenance, negative controls, contract failures, cost, and time. Compare repeated baselines before interpreting small differences.
7. Rehash the original private state and confirm it is byte-identical. Keep raw evidence, replay state, and model output private; publish only anonymized conclusions.

```sh
RZM="$CANDIDATE_RZM" RZM_SKIP_REPO_DELEGATE=1 \
  RZM_TASK_TEST=1 RZM_TASK_DATA_DIR="$FRESH_ABSOLUTE_DATA" \
  node "$PHYSICAL_MINE_SCRIPT" --harness codex \
  --episodes "$PRIVATE_EPISODES" --checkout "$CAPTURED_VAULT" \
  --state "$UNUSED_COMPATIBILITY_PATH" --unscreened --dry-run
```

In the explicit test context, state resolves to `$FRESH_ABSOLUTE_DATA/compounding`, overriding `--state` and bypassing migration. An older baseline without test-context support must use its own fresh absolute `--state` path. Neither variant may share the live store, prior caches, or cursors.

## Evidence status

Offline regression tests and the manual disposition review are complete. Two real CLI previews each estimated $1.57 and made zero model calls. Drew then approved four isolated replay runs with an $8 aggregate cap. All four completed for $0.8084008. The captured source stayed clean, all 134 live state files remained byte-identical, and both variants kept identical model bindings: Terra xhigh for extraction, Sol medium for grouping, and Terra low for verification.

### Paired replay results

Four runs replayed the same 14 development episodes against one captured source snapshot. Both variants used identical model bindings and independent fresh state. All runs used unscreened fallback.

| Run | Extracted | Verified | Held | Proposals | Actual USD |
| --- | ---: | ---: | ---: | ---: | ---: |
| baseline-1 | 10 | 2 | 8 | 0 | 0.0819 |
| baseline-2 | 8 | 5 | 3 | 0 | 0.2213 |
| candidate-1 | 8 | 7 | 1 | 0 | 0.2393 |
| candidate-2 | 9 | 9 | 0 | 0 | 0.2659 |

Total reported cost: **$0.81**. Verified-cluster means rose from **3.5 to 8**. Both candidate repeats exceeded both baseline repeats; the paired gains were +5 and +4. The paired gains exceed the observed baseline repeat range of 3, but two repeats support only a bounded regression conclusion, not statistical significance or population quality.

| Case | Baseline 1 | Baseline 2 | Candidate 1 | Candidate 2 |
| --- | --- | --- | --- | --- |
| Semantic search before duplicate concept creation | Extracted; held C | Not extracted | Not extracted | Not extracted |
| Historical labels and repeated links | Extracted; held C | Verified; other | Verified; other | Verified; other |
| Short executive updates and interactive preparation | Extracted; held C | Extracted; held C | Verified; already_documented | Verified; already_documented |
| Optional next steps and approval | Extracted; held C | Extracted; held C | Verified; already_documented | Verified; already_documented |
| Reviewer identity duplicate control | Extracted; held C | Extracted; held C | Not extracted | Verified; already_documented |
| Obsolete publishing control | Verified; false_now | Verified; false_now | Verified; false_now | Verified; false_now |

Historical-label coverage in the first three runs was bundled into a consolidation candidate; candidate 2 extracted it separately. The verifier lacked current product-source evidence and rejected it conservatively. An adjacent next-step presentation-mechanism fact was verified in baseline 2, but the optional-field policy itself remained held.

The repeated improvement is clear for writing guidance and optional-field policy: both moved from held to verified and were correctly rejected as already documented. Semantic-search recovery was not demonstrated. The identity control was held in both baselines and absent from candidate 1; candidate 2 correctly rejected it. The obsolete-publishing control was correctly rejected in all four runs.

**Adoption:** the results support the narrow fallback eligibility change. They do not establish complete Markdown coverage, general extraction quality, or performance on an untouched holdout. No new proposal was emitted, so no false proposal was observed.

**Remaining limits:** original provenance metadata stayed unchanged, but the existing newest-per-family excerpt selection substitutes later agent reports for direct correction records; the correction text remains in `previous` without its original attribution metadata. Existing excerpt truncation also hides some source rationale. These are follow-ups, not corrected behavior. Normal Jev ranking was not run here.
