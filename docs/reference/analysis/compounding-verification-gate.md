---
type: ReferenceDoc
summary: "Remove Jev's exclusion veto and measure evidence verification of the November 3 held pool with isolated source and state."
reference-kind: analysis
last-verified: 2026-11-03
status: active
---

# Compounding evidence-verification admission

Jev ranks candidates without seeing repository evidence. Its checkability score previously prevented the evidence-reading verifier from examining 24 single-family clusters in the November 3 replay. Every unsettled candidate is now eligible for that verifier. Jev still screens passages and orders peers; the existing Terra low verifier still rejects unsupported, obsolete, generic and already-documented claims. Human publication approval is unchanged.

Count-deferred work resumes without new sessions or `--verify-held`. Spend stops resume their frozen run. Approved/dismissed decisions and open findings continue suppressing settled learnings. Health and the Recommendations banner distinguish incomplete verification from completion and identify the actual store. No new review or task system was added.

## Experiment and source

This is a bounded development replay, not a blind holdout or a model comparison. The earlier [workflow coverage study](compounding-workflow-coverage.md) corrected fallback subject/checkability conflation. This change addresses the normal Jev exclusion gate without changing `jev-latest`, explicit-version support, passage screening or model defaults.

The original October 3 through November 3 packet had 457 episodes, 204 screened episodes, 52 extracted candidates and 46 clusters. Its normal Jev path examined 22 clusters, rejected them all, and held 24 without verification. Those 24 scores were below 0.72: ten had repository-checkable grouper labels and fourteen external labels. These different judgments are not a measured Jev error rate.

The replay freezes current working-tree source, including current documentation changes, at `2026-11-03T16:04:59Z`: 4,682 files, independent Git metadata, no inherited runtime or mining state. Source HEAD was `a645edbbaad9a5159bebc73d30f53d3b43e2d80e`; the disposable snapshot commit is `fd8d7fe6eeb49c6a9f5602a705d2b90dce6b7b88`. The private file manifest records every captured source hash and omission, since HEAD alone omits working-tree edits. Credential files, unsafe external links, executable pins and original indexes/caches were excluded. Embeddings and hooks were disabled in the copy. FTS/current-file evidence remained available.

Only the 24 held clusters and the previously rejected postcard control were seeded into a fresh pool: 27 original candidates and 36 cited episodes. Original scores, source origin/confidence and chronology were preserved. The control's old verification was removed only in the disposable seed so it would be independently checked again; its original record remains in the private packet. No proposals, decisions, cursors, model caches or run history were injected.

Bindings stayed `typesafe:jev-latest`, `codex:gpt-6.1-terra:xhigh` extraction, `codex:gpt-6.1-sol:medium` grouping and `codex:gpt-6.1-terra:low` verification. Empty new episodes make extraction, grouping and ranking no-ops in this replay. Offline tests separately exercise the real normal-Jev and unscreened/fallback mine stages.

## Actual results

All 24 formerly held clusters reached evidence verification. The postcard negative control also reached it. All 25 were rejected; there were zero proposals, invalid responses or pending verifications. This demonstrates admission and disposition, not that every rejection is independently proven correct or that all useful learnings were extracted.

Across all 25 cases, dispositions were 16 already documented, 5 other, 2 not repository specific, 1 generic and 1 false now. Among the original 24 held cases, 15 were already documented; the other counts are the same.

| Case | Synthetic description | Original Jev checkability | Actual disposition | Evidence type |
| --- | --- | ---: | --- | --- |
| 1 | Editor auto-backups can create unexpected commits | 0.33 | reject/already_documented | Current repository/editor guidance |
| 2 | Avoid a redundant owner field on personal goals | 0.70 | reject/already_documented | Current schemas and authoring guidance |
| 3 | Long chart labels need a readable narrow-panel layout | 0.54 | reject/other | Current view source; historical incident unconfirmed |
| 4 | Check availability before using a recently changed chart API | 0.58 | reject/generic | Documented API; current release status unconfirmed |
| 5 | Push a feature branch to its explicit remote ref | 0.57 | reject/already_documented | Current repository Git guidance |
| 6 | Sandbox restrictions can affect an index lock check | 0.38 | reject/other | Entry guide; sandbox cause unconfirmed |
| 7 | Create goal records after the person drafts or sets them | 0.48 | reject/already_documented | Current planning and capture guides |
| 8 | Optional next steps require approval and no reminders | 0.70 | reject/already_documented | Current capture guide and nullable schema fields |
| 9 | A source recording covers only part of a meeting | 0.53 | reject/already_documented | Current meeting and transcript records |
| 10 | Stop a site build when its query results are incomplete | 0.59 | reject/already_documented | Current build guide; query implementation not inspected |
| 11 | A report derived from local notes is not independent evidence | 0.56 | reject/already_documented | Current report provenance guidance |
| 12 | Search conceptually before creating a duplicate note | 0.39 | reject/already_documented | Current authoring and search guides |
| 13 | A setup command can change files before reporting failure | 0.59 | reject/not_repository_specific | Entry guide; external setup behavior unconfirmed |
| 14 | A tracker CLI cannot rename an existing story | 0.64 | reject/other | Entry guide; external rename behavior unconfirmed |
| 15 | Time categories follow story naming and project rules | 0.53 | reject/already_documented | Current classifier, tests and naming guide |
| 16 | Read a quarter of tracked time with one range request | 0.66 | reject/already_documented | Current quarter-read guide; batching unconfirmed |
| 17 | Resolve a human reviewer identity before approval | 0.43 | reject/already_documented | Installed review skill |
| 18 | Change managed skill guidance at its upstream source | 0.69 | reject/false_now | Current managed-file guidance |
| 19 | Optional hooks enable automatic knowledge delivery | 0.69 | reject/already_documented | Installed hooks guide and skill |
| 20 | Consolidate linked notes and review rewritten labels | 0.59 | reject/other | Current note-mutation guidance |
| 21 | Check unresolved targets beyond a validation report | 0.67 | reject/already_documented | Current validation guidance and repository evidence |
| 22 | A separately hosted site deploys from its own repository | 0.65 | reject/other | Current site link; external checkout and command unconfirmed |
| 23 | A new connection may require refreshing the agent session | 0.22 | reject/already_documented | Current source-acquisition guide; fresh external behavior untested |
| 24 | Inspect selected settings without printing credentials | 0.40 | reject/not_repository_specific | Entry guide; external file history unconfirmed |
| 25 | Keep executive updates concise and categories illustrative | control | reject/already_documented | Current writing skill and category guidance |


The optional-next-step case was checkable despite its original 0.70 score. The verifier cited the current capture guide and nullable schema, then rejected it as already documented. It preserved the source distinction between an unknown delegated opener, a likely person correction and later agent reports. The selected agent report was not relabelled as a confirmed human correction.

The time-classification case was rejected as already documented. Current classifier tests and naming guidance cover its local rules; the current generic oversight label also differs from the older statement. The classifier alone cannot establish claims about omitted outside-service fields. The repository guide documents that omission, but no fresh external CLI/API payload was tested. Likewise, the postcard control was rejected against current interactive-writing and illustrative-category guidance rather than generalized into a new standing rule.

## Spend and continuation proof

The dry estimate for 25 cases was about $0.97. The first real run (`20261103T160821-70f81c`) used `--verify-budget 1`: one rejection, 24 deferred by the count limit, $0.032633. The second (`20261103T161156-2d2642`) used `--verify-budget 25` and no `--verify-held`: 24 more rejections, zero remaining, $0.5191098. Aggregate recorded cost was **$0.5517428**, including three binding probes. The verifier made 47 calls across evidence rounds. This is API-equivalent accounting, not a subscription invoice. No full-month repeat, model sweep or new model was run.

A third unchanged invocation returned `nothing-new`, created no run and made no additional model calls. Synthetic stage tests additionally cover spend-stop resume, duplicate prevention, legacy C records, approved/dismissed suppression, completed-finding recurrence and a supported undocumented gap that produces a valid proposal. Their deterministic model replies test pipeline contracts rather than real model accuracy.

## Installed workspace proof

The replay installation was committed in the disposable repository as `f9d1b7623b69ee434ada12a2954f32b3bac4eb85`; its 50 module files have aggregate SHA-256 `681e6ead9dc042423b115b12b284a887ebcaac22e7048e5488521c91d2dd7f6c` (sorted path-to-SHA-256 JSON). The branch-built `bin/darwin/rzm` installed the shipped module into the disposable vault with `init`, followed by `init --check` exiting 0. Its HTTP workspace used that vault's real `.rhizome/compounding` store. No manually staged interface examples were recreated.

The browser showed the first run as `deferred`, one examined/rejected and 24 awaiting evidence verification, with the count-limit reason, continuation command and exact disposable store path. After continuation it showed the second run as `done`, 24 examined/rejected, no pending verification and the rejection breakdown. Recommendations stayed empty and directed readers to Health. Source-text checks alone were not used as UI proof.

All 2,309 live compounding-state files were byte-identical after replay and browser testing. The live desktop continues to read the live vault's own store. This isolated replay was never installed into it and cannot change the history it shows. The tested artifact is the branch-built HTTP workspace and its installed module, not a globally installed Desktop app. After review corrections, the rebuilt/reinstalled module matched all 50 template files and its native Health result read the retained real replay results. Exact final module and executable hashes are retained in the private installation manifest. Browser proof covered real deferral/completion; native installed proof also covers the later scheduler-summary and budget-continuation corrections.

## Reproduction

Raw material, complete source manifests, original scores, source chronology, per-case evidence paths and checked statements, source hashes, ledgers, run manifests, screenshots and dispositions stay in the private `rhizome-compounding-gate-20261103` packet under the system temporary directory. They are excluded from this pull request. A private copy is also retained in the ignored worktree archive `.rhizome/session-memory/eval/verification-gate-2026-11-03/packet.tar.gz`. Public examples above and committed stage fixtures are anonymized.

1. Freeze tracked and nonignored working-tree source into an independent disposable Git repository. Omit credentials, original Git metadata, runtime/index/mining state and external links; disable embedding providers/hooks in the copy. Record the source HEAD, dirty-state inventory and SHA-256 manifest before installing the branch module.
2. Install with an absolute branch-built `RZM`, `RZM_SKIP_REPO_DELEGATE=1`, and `init`; check with `init --check`. Record the installed source hashes and commit the disposable source locally to bind verifier records.
3. Select the IDs in the original audit's `results.json` held array, plus the postcard control, from its saved `state/pool/clusters.json`. Copy only referenced candidates and cited episodes into the new pool. Preserve original records privately; clear only the control's old verification in the seed. Start with no other state or caches and an empty new-episodes JSONL file.
4. From the disposable root, estimate and then run the installed script, replacing the placeholders below. First use count 1 to inspect visible deferral, then count 25 to finish the same pool. No special held opt-in is needed.

```sh
RZM=/absolute/branch/bin/darwin/rzm RZM_SKIP_REPO_DELEGATE=1 \
node .rhizome/modules/compounding/scripts/mine.mjs --harness codex \
  --episodes /private/empty-new-episodes.jsonl --checkout /private/disposable-vault \
  --state /private/disposable-vault/.rhizome/compounding --verify-budget 25 --dry-run
```

Use an authorized `--budget` instead of `--dry-run` for model calls. Trust and serve only the disposable root for browser verification. Preserve any task-test state environment when running an actual task test; it overrides `--state`. Check the final live-state manifest against its pre-test hashes. Never point a branch binary at the live runtime or copy replay state back.

## Limits

This saved development packet does not estimate screening/extraction recall, general verifier quality, or Jev's factual error rate. Some outside-service and historical incident claims remained unconfirmed; rejecting a duplicate local rule does not prove every outside-world clause. All paid calls used the existing Terra verifier. No proposals were forced or published. Invalid verifier contracts remain visibly pending; an unchanged cached invalid response still needs evidence/response repair before retry can progress.
