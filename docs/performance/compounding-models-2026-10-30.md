---
summary: "Matched Terra 6.1, Sol 6.1, and Luna 6 evaluation for compounding extraction, grouping, matching, and verification."
reference-kind: guide
code-paths:
  - tools/sessionmemorybench
---

# Compounding model comparison, October 30, 2026

Terra xhigh is the preferred Codex extraction binding. Terra low is the strongest candidate for verification with the existing proposal-review step. Keep Sol medium for synthesis and Sol low for calibration matching. Jev remains the screening gate. Drew approved adopting these OpenAI defaults after reviewing the results; see Approved defaults below.

These recommendations compare the Codex route. Haiku extraction was not retested. Sonnet medium was run as the frozen scoring and verification reference.

## Question and authorization

Can Terra replace Sol in compounding's more demanding stages, and which reasoning settings justify their additional cost? The shipped passage-screening gate uses Jev and stays fixed; Luna remains a candidate for inexpensive grouping, matching, and extraction. Model and reasoning effort vary; datasets, stage code, prompts, and scoring model stay fixed.

Drew approved the campaign with "Approve up to $75" and explicitly authorized its redacted excerpts, candidate learnings, and code evidence to OpenAI through Codex and Anthropic through Claude Code. The aggregate estimate was $40–60, with a $75 cap including scoring. Transcript-derived inputs, outputs, and item judgments remain in the ignored local eval directory. This report contains counts, prices, methods, and conclusions only.

## Prices

Standard API prices per million tokens, verified October 30 against [OpenAI's model pricing](https://developers.openai.com/api/docs/pricing):

| Model | Fresh input | Cached input | Output, including reasoning |
| --- | ---: | ---: | ---: |
| GPT-6 Luna | $0.10 | $0.01 | $0.50 |
| GPT-6.1 Terra | $1.00 | $0.05 | $5.00 |
| GPT-6.1 Sol | $2.00 | $0.10 | $10.00 |

Terra is half Sol's token price and ten times Luna's fresh-input/output price. The Codex catalog offers Terra low, medium, high, xhigh, and max; its API also supports none, which this Codex comparison does not test. See [Terra's model documentation](https://developers.openai.com/api/docs/models/gpt-6.1-terra).

Costs reported here are API-equivalent usage estimates. The CLIs may bill subscriptions. Provider cache hits affect measured cost; separate cold local run directories do not disable provider caching. These costs do not imply invoice charges.

## Frozen methods

The source checkout starts at `dbc04f3a4d5a842333c5052bbc0ab925fa769f85`. The benchmark imports the compounding starter's current stage functions. Local hashes record the actual inputs, prompts, stage code, binary, and bindings. Codex CLI is 0.163.0. Fresh baselines replace older `codex exec` measurements whose instruction overhead differs from the current app-server path.

Extraction uses the original D development window, September 13 through October 3. Its 691 screened episodes come from 32 families. All arms use 45,000-character batching and five concurrent calls. Recall uses all 192 strong reference learnings from the full 1,581-episode development window, including losses caused by screening. Sonnet 5.5 medium matches learnings to candidates; judges receive neither model names nor old verdicts. Missing, duplicate, and off-shortlist judgments are re-asked individually with the same frozen judge, a one-item payload, and ID-constrained schema. A malformed repair fails scoring. Every arm uses this same scoring protocol; the superseded initial scoring attempts remain included in total spend.

Extraction arms are Terra low/medium/high/xhigh/max, Sol low/medium, and Luna medium/xhigh. Sol low, Luna xhigh, Terra medium, and Terra xhigh each have two fresh runs. Other settings are exploratory single runs. The extraction prompt hash is `cd39e5a418cf7fa1`.

Grouping and matching share a deterministic 77-candidate fixture with 58 selected pairs. The selection combines historical same-cluster pairs and lexically close candidates from different clusters. Candidates assigned to multiple historical clusters are excluded from seed selection because they make the historical pair stratum ambiguous; such assignments are allowed in the tested outputs. A frozen Sonnet 5.5 medium judgment independently labels pairs; historical groupings select cases but do not determine correctness. Ambiguous pairs are retained and excluded from error rates. Grouping calls the shipped reducer directly. Matching reuses the shipped calibration matcher's exact instruction and schema, with one possible label per pair. Every synthesized cluster receives a blind meaning/scope audit.

An initial evaluator incorrectly rejected cross-cluster reuse, contrary to the shipped bundled-candidate rule. Those interim scores were discarded. V4 rescored cached model outputs after the correction; the reducer's shipped within-cluster deduplication, filtering, and singleton fallback remain part of the measured implementation. Superseded attempt costs remain in campaign spend.

Grouping references follow the shipped grouping rules and receive exactly the grouping input fields, excluding `why`. A bundled candidate may legitimately appear in multiple clusters. Pair predictions use any shared cluster, with membership sets rather than a single owner. Matching references use separate calls that receive only the candidate and label statements, and judge whether following candidate A satisfies label B's main point, as the shipped matcher requires. An earlier combined reference exposed extra fields; it was superseded before candidate matching or group audits. Its cost remains included in campaign spend. Matching implication and valid grouping under the bundled-learning rule are scored separately. Group fidelity audits account for companion clusters when a source candidate is split, so a fact represented elsewhere is not classified as lost.

Extraction finalists also receive a blind source-fidelity audit of 24 candidates sampled across batches without using the extractor's strength labels. The judge sees complete cited screened-episode views and checks material claims, scope, conditions, and attribution. It distinguishes supported, partly supported, unsupported, and unclear candidates. This is a small diagnostic sample, not an exhaustive precision measurement.

Verification uses a 24-case pilot, including 12 historical accepted-learning cases and 12 stratified historical rejections, then the original 94-case set for finalists. File evidence is pinned to clean commit `184889cbeb2020b8887364602e4139ae6f062233`. Search uses a separate copy at that same commit, with paid embeddings disabled and a fixed keyword/code index. Search responses are cached across models. This avoids later documentation and eval reports leaking answers into evidence. Seed searches for files outside the configured indexed types, including Makefiles, module manifests, ontology GraphQL, and CSS assets, can return ownership errors. These are counted as unavailable searches; direct tracked-file reads and grep still work. The fixed `--fast` search path disables vector retrieval and cannot invoke Voyage. All models receive the same retrieval policy; a verifier can request up to two additional evidence rounds, as shipped. Invalid proposals do not count as recovered learnings.

Independent qualitative checks use producer-blind GPT-6.1 Sol high reviews of source meaning and code evidence. These are separate from frozen Sonnet scoring and are not human acceptance labels. Blinding hides producer names; shared model style and reviewer mistakes can still bias those checks.

The newer verifier prompt includes response classification and differs from historical verifier baselines. Historical accepted-learning recovery and binary agreement are secondary evidence, not a substitute for checking revised proposal content.

## Results

### Development extraction

All 39 batches completed in every arm. Costs below include extraction, not scoring or subsequent verification.

| Binding | Found / 192 | Candidates | Cost | Wall time |
| --- | ---: | ---: | ---: | ---: |
| Sol low, run 1 | 90 | 257 | $1.325 | 2.8 min |
| Sol low, run 2 | 94 | 261 | $1.335 | 2.9 min |
| Sol medium | 96 | 313 | $1.482 | 4.1 min |
| Luna medium | 53 | 158 | $0.063 | 1.4 min |
| Luna xhigh, run 1 | 85 | 310 | $0.169 | 8.3 min |
| Luna xhigh, run 2 | 81 | 316 | $0.181 | 8.8 min |
| Terra low | 94 | 298 | $0.681 | 1.9 min |
| Terra medium, run 1 | 102 | 292 | $0.680 | 1.9 min |
| Terra medium, run 2 | 98 | 286 | $0.675 | 1.9 min |
| Terra high | 102 | 306 | $0.692 | 2.0 min |
| Terra xhigh, run 1 | 106 | 342 | $0.742 | 2.5 min |
| Terra xhigh, run 2 | 108 | 363 | $0.763 | 2.5 min |
| Terra max | 130 | 653 | $3.419 | 26.7 min |

Luna xhigh sent 3 and 7 candidates through repair in its D runs, dropping 0 and 1 respectively. Terra medium's first run sent one through repair and dropped it after final checks. Other D arms needed no candidate repairs.

The reasoning setting is applied by the app-server. Terra reported 0, 87, 964, 4,051, and 477,914 reasoning tokens across low through max; Luna xhigh used 197,674 and 219,058. Reasoning tokens are already included in output token costs. This explains why Luna xhigh is inexpensive in dollars but slower than Terra medium, and why Terra max changes the time/cost profile sharply.

The Sol low, Luna xhigh, and Terra medium spreads are four learnings each; Terra xhigh varies by two. Mean recall is 92 for Sol low, 83 for Luna xhigh, 100 for Terra medium, and 107 for Terra xhigh. Xhigh gains seven over medium at about 11% higher extraction cost and 31% more elapsed time. It is the preferred balanced extraction setting frozen before confirmation. Max has higher single-run recall, with 4.5 times xhigh's cost and about eleven times the elapsed time; its quality and repeatability are not established enough for a routine recommendation.

### Frozen historical confirmation

H contains 1,697 episodes across 26 families, with no family overlap with D. The frozen screen retains 666 episodes and 30.37% of text characters. Its 208 strong reference learnings came from the historical unscreened Haiku xhigh map. All three bindings were chosen on D before this campaign opened H. This window was used in earlier evaluations, so it is historical confirmation rather than a new untouched test.

| Binding | Found / 208 | Candidates | Extraction cost | Wall time |
| --- | ---: | ---: | ---: | ---: |
| Terra xhigh | 99 | 278 | $0.599 | 2.0 min |
| Sol low | 87 | 204 | $1.060 | 2.5 min |
| Luna xhigh | 80 | 240 | $0.127 | 6.5 min |

Terra recovered twelve more reference learnings than Sol while costing 43% less and taking 19% less time. Luna costs about a fifth as much as Terra but finds nineteen fewer reference learnings and takes over three times as long. Terra and Sol required no candidate repairs or drops. Luna sent 15 candidates through repair and dropped 11 after final contract checks. This confirms the preselected Terra xhigh extraction recommendation without tuning on H.

### Source fidelity

A blind diagnostic sample uses 24 candidates from each arm:

| Binding | Supported | Partly supported | Unsupported |
| --- | ---: | ---: | ---: |
| Sol low | 19 | 5 | 0 |
| Luna medium | 16 | 6 | 2 |
| Luna xhigh | 18 | 6 | 0 |
| Terra low | 16 | 8 | 0 |
| Terra medium | 19 | 5 | 0 |
| Terra xhigh | 19 | 5 | 0 |
| Terra max | 13 | 11 | 0 |

No samples were unclear. Partial support includes both material distortions and reasonable derived advice that the judge considered insufficiently explicit in the source. An independent review found supported main points in all eleven max partials. These counts must not be treated as eleven fabricated learnings or used to multiply recall by a precision estimate. Real failure types include lost conditions, broadened scope, changed mechanisms, and proposed fixes described as completed.

### Grouping

The frozen reference marks 34 shared-learning pairs, 23 distinct pairs, and one ambiguous pair. No arm merged a selected distinct pair. That does not rule out inappropriate merges elsewhere, which the all-cluster audit checks. Meaning and merge flags below are automated audit flags, not confirmed-error counts. Costs exclude the audit.

| Binding | Shared pairs found / 34 | Meaning flags / audited clusters | Merge flags | Mean cost | Group call seconds |
| --- | ---: | --- | ---: | ---: | --- |
| Sol low (2 runs) | 27 | 3/49, 1/49 | 0 | $0.0728 | 77 to 89 |
| Sol medium (2 runs) | 27 to 28 | 3/50, 2/49 | 0 to 1 | $0.0770 | 93 to 99 |
| Sol high | 30 | 2/48 | 2 | $0.1469 | 302 |
| Luna low | 29 | 14/46 | 1 | $0.0032 | 31 |
| Luna medium | 32 | 11/49 | 1 | $0.0038 | 45 |
| Luna xhigh (2 runs) | 32 to 34 | 8/42, 9/45 | 0 | $0.0066 | 75 to 131 |
| Terra low | 27 | 11/50 | 2 | $0.0354 | 53 |
| Terra medium (2 runs) | 27 to 28 | 6/49, 5/49 | 0 | $0.0342 | 46 to 47 |
| Terra high | 28 | 6/49 | 0 | $0.0338 | 46 |
| Terra xhigh | 26 | 6/52 | 0 | $0.0383 | 53 |
| Terra max | 27 | 5/53 | 2 | $0.1155 | 218 |

A separate producer-blind qualitative review inspected flagged cases from Sol medium, Terra medium/xhigh, and Luna medium/xhigh. It accepted many flags as reasonable compression or information preserved in companion clusters. Among the reviewed first-run cases, it found 0 material issues for Sol medium, 2 each for Terra medium and xhigh, 9 for Luna medium, and 5 for Luna xhigh. A Sol medium repeat and Sol low baseline each had one moderate omission. These are enriched diagnostic counts, not precision estimates. Material losses included build prerequisites, release-authorization conditions, validation steps, delivery scope, and incompatible metric definitions. Sol medium remains the preferred synthesis binding. Terra medium is cheaper and similarly clusters the selected pairs, but the evidence does not establish equal fidelity.

### Calibration matching

This is the shipped calibration matcher, not a general retrieval benchmark. Its separate directional reference contains 25 matches, 29 nonmatches, and four ambiguous pairs. Costs cover all 58 pairs and exclude reference judging.

| Binding | Matches found / 25 | False matches / 29 | Missed matches / 25 | Mean cost |
| --- | ---: | ---: | ---: | ---: |
| Sol low (2 runs) | 23 | 0 | 2 | $0.0333 |
| Sol medium (2 runs) | 23 | 0 | 2 | $0.0351 |
| Sol high | 23 | 2 | 2 | $0.0545 |
| Luna low | 22 | 4 | 3 | $0.0016 |
| Luna medium | 23 | 7 | 2 | $0.0022 |
| Luna xhigh (2 runs) | 23; 1 run invalid | 3; 1 run invalid | 2; 1 run invalid | $0.0050 |
| Terra low | 18 | 0 | 7 | $0.0157 |
| Terra medium (2 runs) | 17 to 19 | 0 | 6 to 8 | $0.0157 |
| Terra high | 15 | 0 | 10 | $0.0172 |
| Terra xhigh | 14 | 0 | 11 | $0.0206 |
| Terra max | 11 | 0 | 14 | $0.1705 |

Sol low and medium produced the same 23 matches and zero false matches in both fresh runs. Higher Terra reasoning produced more missed matches, reaching only 11/25 at max. Luna xhigh's second run returned a foreign label ID and has no usable matching score; its first run still had three false matches. The reported costs include the invalid attempt.

Independent blind checks also found questionable positives in the frozen matching reference. In a selected set of 18 disagreements, reviewers confirmed many Luna false matches and Terra omissions, identified three reference overmatches, and marked two cases ambiguous. The reference was not relabeled after observing model outputs. Two automated Sol misses were justified rejections under that review, so the diagnosis supports retaining Sol low despite imperfect reference labels.

### Verification pilot

The verifier pilot holds 24 cases fixed. Passing output checks means `propose` with no shipped contract violations. It does not establish factual truth or a correct edit.

| Binding | Proposals passing output checks | Invalid proposals | Cost | Wall time |
| --- | ---: | ---: | ---: | ---: |
| Sonnet medium, reference | 5 | 0 | $1.984 | 1.5 min |
| Sol low | 1 | 0 | $2.048 | 3.5 min |
| Sol medium | 2 | 0 | $2.145 | 4.2 min |
| Sol high | 1 | 0 | $2.562 | 7.4 min |
| Luna medium | 3 | 1 | $0.056 | 1.3 min |
| Luna xhigh | 5 | 0 | $0.169 | 7.1 min |
| Terra low | 3 | 0 | $0.686 | 1.7 min |
| Terra medium | 2 | 0 | $0.817 | 2.0 min |
| Terra high | 2 | 0 | $0.877 | 2.2 min |
| Terra xhigh | 2 | 0 | $1.102 | 3.0 min |
| Terra max | 2 | 0 | $2.523 | 15.8 min |

Medium through max propose the same two case IDs. The frozen Sonnet reference proposes five, but independent review found a false blanket platform claim among those five. More proposals or greater agreement therefore cannot be read as greater correctness. A blind comparison of actual edits found that Luna xhigh's suggested test-documentation change removed integration coverage outside one directory. Terra low and medium preserved coverage while adding the required flags. All three edits matched their target text, so this was a semantic error rather than a patch-contract failure. Full-set results below test those findings over the original 94 cases. A separate blind policy review of four verifier disagreements found one justified rejection and three reusable proposal cores after narrowing. The artifact-cleanup disposition was supported after checking the actual `fix` response, which the learning-only audit view omitted; Luna medium instead chose `remember`. One frozen Sonnet reference proposal generalized a platform/toolchain limitation that the frozen release configuration contradicted. This demonstrates that source fidelity, present code truth, and proposal worthiness are different checks. The automated reference remains unchanged for all arms; its relative false-positive/false-negative counts are agreement diagnostics, not objective accuracy.



### Full verification and repeats

The finalist expansion uses all 94 cases, with two fresh Sol-low runs and two Terra-low runs. The same frozen Sonnet medium verifier is a fresh comparative reference. It proposes eleven cases. Every full-set proposal passed output checks; factual quality and edit correctness remain separate questions. Luna was not expanded after its pilot produced a material integration-coverage regression.

| Binding | Valid proposals | Invalid | Relative TP / FP / FN | Cost | No-cache input pricing | Wall time |
| --- | ---: | ---: | --- | ---: | ---: | ---: |
| Sonnet medium, reference | 11 | 0 | reference | $6.117 | $5.429 | 4.8 min |
| Sol low, run 1 | 0 | 0 | 0 / 0 / 11 | $6.441 | $6.441 | 11.5 min |
| Sol low, run 2 | 1 | 0 | 1 / 0 / 10 | $6.264 | $6.336 | 11.2 min |
| Terra low, run 1 | 3 | 0 | 2 / 1 / 9 | $2.308 | $2.376 | 5.1 min |
| Terra low, run 2 | 4 | 0 | 3 / 1 / 8 | $2.183 | $2.420 | 5.2 min |

TP/FP/FN describe agreement with Sonnet's eleven positives and 83 negatives, not established correctness. Sol's all-reject run would have high aggregate agreement because negatives dominate; that is not a useful quality ranking. Terra's three first-run proposal case IDs all recur, with one additional case on its second run. All four distinct Terra proposal cases request fixes rather than retaining workarounds as memory.

Terra averages $2.245 and 5.14 minutes, versus Sol's $6.353 and 11.32 minutes. That is 65% lower observed cost and 55% less elapsed time. At fresh-input rates the comparison is $2.398 versus $6.389, still 62% lower. These costs include each model's chosen evidence rounds under the same frozen retrieval policy. They do not measure a complete mining-and-delivery run.

An independent qualitative review inspected eight hash-selected disagreement cases and all forty anonymous model options. It found real useful cores, legitimate rejections, overstrict rejections, and incorrect reference proposals. Terra's second E2E-isolation proposal preserved the separate-worktree condition and addressed the actual server-reuse cause; its first version needed that condition made explicit. Sol's second-run Windows checklist proposal matched the release compiler setup better than blanket platform claims, though implementation must update the corresponding Claude skill too. Terra's later proposal on that case also needs precise compiler, architecture, and FTS5 details. Several fix outputs are change sketches rather than executable patches.

The same review rejected the reference's universal cross-build prohibition, a dependency-symlink workaround promoted into mandatory policy, and sandbox guidance already covered by current policy. It also found that some rejections discard useful GraphQL/golden/web coupling. All five exact edit anchors in the sample matched once, and none of those edits removed existing test coverage; some still broadened human intent, concurrency permissions, or golden-update obligations. This sample does not yield verifier accuracy. It supports Terra low as a lower-cost proposal verifier with normal review, not as an automatically correct editor.

## Recommended lineup

| Role | Binding | Reason |
| --- | --- | --- |
| Passage screening | Existing Jev gate | Frozen in this experiment; no new Luna gate comparison. |
| Codex extraction | Terra xhigh | Repeated D advantage and historical H confirmation, at about 43% less extraction cost than Sol low. |
| Verification | Terra low, with proposal review | Repeatable useful fix cores, 62% lower cost at fresh-input rates and about 55% less elapsed time. Scope and implementation details still need review. |
| Grouping and synthesis | Sol medium | Better retention of material conditions in this small fidelity study; Terra medium remains a cheaper candidate with uncertain parity. |
| Calibration matching | Sol low | Stable repeat results; Terra misses more matches, while Luna adds false matches and an invalid repeat. |

Terra medium is a lower-latency extraction alternative supported on D, at roughly the same price as low and more recall. High did not separate from medium beyond observed repeat variation. Max is a promising offline coverage option: 130/192 in one run versus xhigh's mean 107, but 4.5 times the extraction cost and about eleven times the time. It generated 653 candidates versus 342–363 for xhigh; downstream effects were not measured. Max has no repeat or historical H confirmation here.

Luna's price fits disposable triage where outputs can be discarded or checked. This study did not test that use or a replacement for Jev. In the measured memory roles, its lower dollar cost came with missed learnings, material fidelity errors, contract problems, or slow xhigh inference.

## Approved defaults

After the evaluation, Drew said, "Okay, go ahead and update the defaults for OpenAI models." Profile version 2 adopts Terra 6.1 xhigh for extraction and Terra 6.1 low for verification, retains Sol 6.1 medium for synthesis and Sol 6.1 low for calibration matching, and leaves Claude defaults and Jev screening unchanged. Sol low remains the extraction and verification fallback; Luna xhigh remains the final extraction fallback. The shipped profile's evidence points here. Existing local role overrides continue to take precedence. The adoption passed 166 Node tests, a CLI build, a fresh install followed by `--add-addon compounding` and `init --check`, and repository validation with zero issues. The installed profile is byte-identical to the version 2 template.

## Spend and checks

Total recorded matrix usage is **$60.6850 API-equivalent across 2,403 calls**, including all scoring, repairs, and superseded attempts. The cap is $75. Aggregate spend and every ledger reconcile within floating-point tolerance. No calls lack usage or pricing, and no paid API attempts failed. Candidate and proposal contract failures described above remain quality failures. Provider subscription invoices may differ.

Verification completed:

- `node --test pkg/app/cli/init/testdata/compounding/*.test.mjs tools/sessionmemorybench/*.test.mjs`: 164 passed, zero failed.
- `make build BINARY_NAME=rzm-evalcheck` built the full web/CLI bundle; `make build NO_WEB=1` built the final CLI after paid runs ended.
- Fresh disposable `rzm init` with the compounding add-on, followed by `rzm init --check`: exit 0. The emitted caller includes Terra pricing and budget-stop propagation.
- `RZM_SKIP_REPO_DELEGATE=1 RZM_RUNTIME_AUTOSTART=0 ./scripts/rzm validate`: exit 0, zero issues or errors. The sandboxed attempt could not prepare its projection; the local-cache-capable run passed.
- Independent tooling review found no remaining blocking scorer, accounting, provenance, or privacy defect.

The exact evaluation binary is archived locally with its hash before the final build. Raw data, individual outputs, judgments, and that binary are ignored by Git. This report and the baseline guide contain aggregate results only.

## Limits

These are stage evaluations. They do not establish improvement in a complete agent task or live memory delivery. The pair fixture tests a fresh grouping batch rather than incremental grouping against existing clusters; matching tests one possible label per pair rather than production multi-label ambiguity. Sonnet is a consistent automated judge, not human ground truth. The verifier pilot deliberately enriches historical positives, so its proposal rate is not an estimate of production prevalence. The full-set historical recovery denominator expands merged source clusters to thirteen IDs, while the twelve pilot positives were selected by source cluster; those counts are secondary historical diagnostics. Latencies include provider service time and should be treated as indicative unless supported by sufficient repeats.

Shipped defaults require a measured advantage larger than repeat noise, or comparable quality with at least 25% lower cost or latency. Terra xhigh, Sol low, and Luna xhigh were frozen before opening the historical H window in this campaign. H has been used by earlier compounding evaluations, so this is confirmation on a previously used, later family-disjoint window, not a newly untouched test. A strict new held-out claim needs a fresh later window. No default change follows from synthetic tests or old baseline scores.

## Reproduction

The original development data is local-only under `.rhizome/session-memory/eval/`; rebuilding it requires the recorded development-window survey. Campaign approval, dataset hashes, model outputs, timings, spend, and scoring artifacts are under `runs/terra61-2026-10-30/` within that directory.

```sh
node tools/sessionmemorybench/campaign.mjs extract
node tools/sessionmemorybench/campaign.mjs recall
node tools/sessionmemorybench/verify-models.mjs pilot gpt-6.1-terra low 1
```

Grouping/matching use `roles.mjs prepare`, `roles.mjs reference`, `roles.mjs run --model MODEL --effort EFFORT --repeat N`, and `roles.mjs report`. Paid role commands require the absolute campaign `spend.json` as `SM_BENCH_SPEND_FILE` and `SM_BENCH_BUDGET_USD=75`. `roles.mjs reference` reproduces this campaign: it reuses the archived v3 Sonnet-medium matching reference and its ledger (`--match-reference`, `--match-ledger`) and regenerates only the grouping judgments, so a fresh comparison needs a new matching reference first. `verify-models.mjs` refuses to run unless both the evidence checkout and the search checkout are clean at the frozen commit.

The frozen confirmation uses `heldout.mjs prepare` followed by `heldout.mjs run`, with the staged manifest and finalist file retained locally. Fresh full verifier repeats use `verify-models.mjs full MODEL EFFORT REPEAT`; `verify-reference.mjs compare --phase full` compares against the frozen reference. `summarize.mjs` reconciles aggregate ledgers.

Run campaign commands sequentially. Each uses the same aggregate spend record. Reservations are persisted before inference; missing usage, lost responses, and interrupted calls are conservatively charged at their reserved maximum until reconciled. The runner stops when the cap cannot cover the next call's reservation.
