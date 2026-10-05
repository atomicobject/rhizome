---
type: ReferenceDoc
summary: "Fable 5.1 high found no blocking correctness defect; the coordinator corrected an evaluation interpretation and verified fresh installs at the reviewed revision."
reference-kind: analysis
last-verified: 2026-09-07
status: active
---

# Agent experience final review

Fable 5.1 high reviewed `076f6764e491db6a1829267e63cfdd6fd7533a4b` after all five child PRs and integration regeneration were merged. The Ask Fable wrapper ran read-only with the explicitly selected `claude-fable-5-1[1m]` model and `--effort high`; it exited successfully with the report below. The coordinator independently assessed the findings. Fable's review is not human rubric scoring of the model trials.

## Coordinator disposition

- **F1 corrected.** The recorded summary confirms the adapter PATH defect affected A02 baseline and A01 candidate. The pilot now attributes the observed proportional local route only to A02 candidate, whose launcher availability was verified. Results and effort wording name both affected cells. Raw evidence is unchanged.
- **F2 verified; follow-up.** A no-model probe of `run_one` with a blocked preflight writes a blocked ledger entry and rejects the same id on a second attempt, without reserving or launching a model. A recovery design must preserve failed-preflight evidence and reservation integrity; it is not a blocker for the completed pilot, whose unavailable cells used the separate preflight command. Do not reset the campaign or alter its consumed budget to work around this behavior.
- **F3 remains a code-inspection hypothesis.** The rename failure path does not recheck an identical winning artifact. Concurrent same-directory generation was not reproduced in this review. Keep normal generation isolated by task; a bounded concurrency regression and reuse correction are a follow-up.
- **F4 verified tradeoff; optional optimization.** Every invocation runs contract discovery before the requested operation. This preserves current-executable verification and the deterministic measurements include its cost. Verify-once caching would need an explicit executable-change policy; no speculative cache was added.
- **F5 fresh-install gap addressed.** The [validation receipt](agent-experience-evaluation-evidence/final-head-install-validation.json) records the source, binary, checks, and results. `make build` passed at the clean reviewed source. A disposable four-way install check (no starter, AE, Domain, explicit AE+Domain) passed: skill file counts 28/52/69/69; each repeated init preserved skills and both harness mirrors; Domain and explicit combined skills matched; alignment/reconciliation hooks appeared only in Domain composition. Ontology, recipes, views, overlays, and broken-link validation passed on the combined Domain install. Binary SHA-256: `04c9f82020ba18111d94c32947ee028f90abaea90624d4af724f168ea5131e8b`. Existing code-mode parity evidence remains bound to its recorded earlier E source; no final-head model or parity run is implied.
- **F6 documented.** Starter-free fixtures select no workflow starter but include the shared client-harness-builder and legacy-codebase-assessor skills in both arms.

The all-merged source passed `make check CHECK_JOBS=3 GO_TEST_PACKAGES=4 GO_TEST_PARALLEL=4`, all 34 focused Python evaluation tests, and documentation validation. The post-review changes are evidence wording and this disposition; no runtime or template behavior changed. No further model evaluations ran.

## Captured Fable report

# Independent final review: `codex/agent-experience-integration`

Inspected HEAD `076f6764e491db6a1829267e63cfdd6fd7533a4b` against merge-base `b978cc538c3d2bd50b404c813b368c194219cdf1` (313 files). Read-only; no tests were executed by me, no files changed.

## 1. Findings (prioritized)

No blocking correctness defect found. One evidence-interpretation defect should be corrected before or immediately after merge. The rest are low-severity follow-ups.

**F1 (medium, verified) — A02 baseline is presented as a proportionality observation, but its cell ran with the Rhizome launcher hidden.**
`docs/reference/analysis/agent-experience-evaluation-pilot.md:39-42` says "Both A02 traces stayed local ... no Rhizome invocation ... an observed proportionality result for these two executions." The continuation summary (`agent-experience-evaluation-evidence/continuation/summary.json`, run `a02-baseline`) records `shell_runtime.checked=false, "Adapter v1 PATH defect recorded"` and the pilot itself states the first two continuation calls omitted `repo/scripts` from PATH. For `a02-baseline`, "no Rhizome invocation" cannot be separated from "Rhizome unavailable". Only `a02-candidate` (PATH corrected, `rzm version v0.50.5` verified) supports the observation. Related wording: `agent-experience-evaluation-results.md:17` calls it "a candidate PATH confound", but it affected the A02 baseline and A01 candidate.
Minimal correction: restrict the proportionality sentence to the A02 candidate and name the PATH limitation for the A02 baseline; change "candidate PATH confound" to "adapter PATH confound (A02 baseline, A01 candidate)".

**F2 (low, hypothesis from code, not executed) — a preflight-blocked `run` burns the run id without consuming a launch.**
`scripts/agent-experience-evals/runner.py:279-290` writes a `blocked` ledger entry under `ledger["runs"]`; the guard at line 279 then rejects any later `run` of that id, and the README forbids a fresh campaign. Had the three blocked cells been invoked via `run` (they were checked via `preflight`, so it did not bite), the authorized continuation would have been impossible without editing the ledger. Minimal correction: permit relaunch when the existing entry's status is `blocked` (no reservation exists for it), or document the semantics.

**F3 (low, hypothesis) — concurrent identical `generate` into one output directory fails instead of reusing.**
`pkg/app/agentcode/generate.go:119-135`: two writers both stage; the loser's `os.Rename(staging, target)` fails on the existing non-empty hash directory and surfaces "publish code artifact" even though content is identical. Minimal correction: on rename error, re-check `completeArtifact(target, files)` and return `reused=true`.

**F4 (low, proportionality) — the generated client spawns an extra `describe` process on every operation call.**
`pkg/app/agentcode/template.go:168-169`. The spec requires verification "before task calls"; per-call satisfies it but doubles subprocesses for the "simple lookups must stay cheap" goal. The deterministic evidence includes this cost (outer timings), so nothing is misreported. Optional: verify once per client (first call), which is the simplest correct form.

**F5 (low, evidence gap newly noted) — no fresh-install validation at HEAD.**
Combined A03/A04/A05 install validation ran at `1ae34b2b` (Domain merge), before #247 code-mode and #246 evals merged and before the mirror-regeneration commit `82c1e4f7`. Deterministic code-mode parity ran at E `6b050f23`. Templates and installed mirrors match at HEAD (verified by diff), and the later merges are code/docs rather than template content, so risk is low. The coordination record's "`make check` passed on composed source" was not re-run here.

**F6 (informational) — the "starter-free" fixtures are not base-skill-only.**
Both arms of A01/A02 carry `client-harness-builder` and `legacy-codebase-assessor` managed skills (installed by `--agent-skills on`; see `pilot-manifest-summary.json` guidance). Identical across arms, so not a confound; worth stating when the fixture is described as starter-free.

Checks that passed inspection (not exhaustive): canonical templates equal `.agents` and `.claude` mirrors for rhizome, agentic-engineering, all eight domain skills, foundation-review, ingest-transcript, and both starter recipe files; every recipe id and view id referenced by installed skills resolves (27 recipes, `complex-domain.uncovered-requirements` view present); every new recipe field (`planApprovedBy`, `closureChecklist`, `executionNotes`, `sourceLocations`, `conflicts`, typed `requirements` relations, `pageInfo`/`warnings`) exists in the ontology or query layer, and live recipe execution is covered by `complex_domain_recipe_behavior_test.go` and `agentic_engineering_experience_test.go`; generated-client argv matches real flag definitions (`--include-content` is a string flag so `true`/`false` values are valid; `--max-depth`, `--ensure-link-targets never`, `--limit`, `--budget-chars`, `--intent`, `--input`, `--file` exist); path validation blocks finder syntax and boolean operators consistent with `--input` being a pattern surface; manifest preflight refuses unrelated or symlinked manifests; persisted manifest embeds no executable, vault, or session; the three fronts are registered runtime-free with `generate` write-capable; catalog contracts are cloned by value; `agent surface` renders `code` with subcommands. Eval isolation: clean-home adapter denies other runs' homes, support root, auth/config, original fixtures; ledger hash chain and overlay checkpoint bind the continuation to the consumed reservation; `compare` withholds success without human review.

## 2. Composition and proportionality assessment

The base/AE/domain composition achieves the intended shape. The base skill now leads with classify-then-route, a single evidence-authority rule, and explicit "reuse current context, stop when the next decision is supported" language; the old file-context sentence that made retrieved docs "operational constraints" is gone. AE keeps spec → effort → approved plan, states approval continuity once in `SKILL.md` and consistently in workflow-state, implementation, planning, quality-gates, installation and validation references, and gives resume a concrete recipe with blank-approval semantics. Complex Domain composes through two additive AE hooks plus its own retrieval-and-evidence reference that separates unresolved anchor, missing authored coverage, degraded capability, truncation/cap, and stale evidence; the recipes surface `pageInfo`/`warnings`, turn hard caps into bounded inputs, and no longer word empty results as absence. No routing contradictions between the managed blocks, AE router, and base skill were found. A trivial task stays local under every document.

Code mode is proportionate as a pilot: two read operations, opt-in, no service, no second registry, no automatic generation, runtime-free discovery. Its only surfacing is `rzm agent surface`; no skill or `RHIZOME.md` text routes to it, which matches the opt-in decision but should be stated so nobody expects agents to reach it through guidance. The per-call `describe` (F4) is the one place the design pays more than needed. `generate` requires vault resolution (`cmd/agent_code.go:49`), so it is unusable outside a configured project; by design, worth knowing.

## 3. Evidence gaps

Known and intentional, still explicit: four Luna xhigh launches consumed; A01 baseline infrastructure-unusable; A02 cells passed exact-edit checks; A01 candidate passed boundary tests but PATH/Python confound blocks a Rhizome-assisted comparison; no improvement claim; no retries; deterministic parity and fresh-install validations are not model-effectiveness evidence; human rubric scoring absent.

Newly noted: the A02 baseline proportionality claim is also PATH-confounded (F1); fixtures include two unrelated managed skills (F6); no install or parity evidence at final HEAD (F5); runner blocked-id semantics are untested (F2); the checked-in code-mode manifest binds a machine-local binary path, so the comparison is reproducible only by rebuilding per the README.

## 4. Recommendation

Merge-ready from a correctness standpoint. Apply F1's two wording changes first, since they are the only place the record overstates what the evidence supports. Track F2–F4 as small follow-ups. Keep all behavioral-benefit claims withheld until human rubric review exists.
