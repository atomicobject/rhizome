---
type: ReferenceDoc
summary: "Manual Luna behavioral evaluation protocol, fixture matrix, evidence contract, spec amendments, and implementation exits for the agent experience program."
reference-kind: analysis
last-verified: 2026-09-07
status: active
---

# Agent experience evaluation plan

## Authority and delivery contract

This is the implementation contract for behavioral evaluations, within the [preparation brief](agent-experience-preparation.md), [research handoff](agent-experience-research-handoff.md) and [integration effort](../../efforts/2026-09-05-11-42-starter-skill-consolidation.md). Drew's direction to drive through delivery and the coordinator's explicit pilot dispatch supersede the initial planning-only authority. The owned effort now freezes real SPEC-0080 US6 targets and records approval after current-user validation.

Deliver the Python standard-library manual runner, synthetic fixtures, deterministic outcome checks, and reviewed evidence from exactly four permitted launches: A01 bug fix and A02 typo on baseline and delivered Base candidate, only `gpt-5.6-luna` / `xhigh`, serial, at most 600 seconds each / 2400 seconds aggregate, no retries or fallback models. A03–A06 get fixtures and deterministic checks now; their model runs are not authorized. Implementation, review fixes and a merge-ready child PR continue without routine approval pauses. Coordinator merges staging; main/release remain excluded.

## Delivery status

The [pilot report](agent-experience-evaluation-pilot.md) records all four consumed
launches: one infrastructure-unusable baseline and three deterministic correctness
passes. The user authorized subscription-only continuation through Codex. An
explicit environment overlay preserves the original ledger and fixture identities
while using relocated workspaces and clean Codex homes with native permissions.
A PATH defect affected candidate retrieval before correction; no guidance-quality
or timing improvement conclusion is supported.

## Source-grounded findings

Inspected source revision: `3b93489c4e404981fc4bb193e75c0d70e9eac7a7`, exactly this planning checkout's starting HEAD. Ancestry verification passed. Staging PR #243 was open and draft at that SHA on 2026-09-07.

| Evidence | Finding and consequence |
| --- | --- |
| [Existing runner](../../../scripts/routing-evals/runner.py), [tabulator](../../../scripts/routing-evals/tabulate.py), [scenarios](../../../scripts/routing-evals/scenarios.json) | The runner says not to perform the task, launches Fable and implicit-default Astra concurrently, and caches by scenario/model filename. It does not bind cached results to source or prompt hashes. The tabulator repairs incomplete JSON and compares self-reported choices. Reuse prompt ideas, not this execution or scoring path. |
| [September routing analysis](agentic-engineering-routing-evals-2026-09-04.md) | Missing files, already-completed efforts, and the read-only evaluation suffix affected reported stops. Build valid scenario premises and score execution evidence; retain this report as historical qualitative evidence. |
| [Base source skill](../../../pkg/app/cli/init/templates/skills/markdown/rhizome/SKILL.md), [domain source skill](../../../pkg/app/cli/init/templates/starters/complex-domain/agents/skills/complex-domain/SKILL.md) | Base startup omits `--profile`/`--ontology`; domain startup still requests both. Measure redundant discovery against the agreed composition contract rather than silently choosing one instruction as the oracle. |
| [AE metadata](../../../pkg/app/cli/init/templates/starters/agentic-engineering/template.yaml), [domain metadata](../../../pkg/app/cli/init/templates/starters/complex-domain/template.yaml) | AE requires core and defaults action-items; domain requires AE. The three treatments must record resolved dependencies, including default addons. “No starter” uses `none` and verifies actual installed base guidance. |
| [Init tests](../../../pkg/app/cli/init/helper_templates_test.go), [overlay tests](../../../pkg/app/cli/init/skill_overlay_test.go), [ontology tests](../../../pkg/app/cli/init/ontology_validation_test.go) | Deterministic topology, reachable references, composition, and typed shapes already have ordinary tests. Keep those responsibilities there; this harness measures agents acting on rendered output. |
| [Agent surface guidance](../subsystems/agent-surface.md), [CLI guidance](../subsystems/cli.md) | Readiness/warnings, session dedupe, canonical evidence, one-selector validation, and init authority are existing constraints. Do not invent new runtime flags, expand init authority, or interpret unavailable work as measured zero. |
| [Development loop](../../specs/process/development-loop.md) | SPEC-0001 is archived and explicitly redirects to `docs/engineering/`. Do not freeze this historical process spec as current evaluation authority. |

Live project-launcher session and typed `effort-execution-context`, `frozen-spec-index-pack` for SPEC-0080, and `spec-domain-context-pack` for SPEC-0062 resolved successfully. At initial planning the effort had no frozen selection; the delivery dispatch subsequently froze US6 as recorded in the effort. The domain pack returned no linked feature areas, processes, workflows, or other linked notes; this is a coverage gap, not proof of domain completeness. Synthetic fixtures establish explicit source/requirement links instead of assuming a populated production neighborhood. Startup reported missing indexed enrichment, while live typed retrieval worked; no code-index or semantic-retrieval success is claimed by this planning pass.

## Ownership

Evaluation-owned paths below are authorized for implementation. Shared production changes remain with their named owners.

| Owner | Exact paths / responsibility |
| --- | --- |
| Evaluation worker | This note; `docs/efforts/2026-09-07-08-21-agent-experience-evaluations.md`; `scripts/agent-experience-evals/` (manual runner, Codex adapter, fixture preparation, evidence/scoring, manifests, and focused tests). |
| Evaluation worker | `testdata/agent-experience/{README.md,base/,engineering/,domain/,code-mode/,oracles/}`; versioned source trees and reviewer-owned expectations. Keep oracle material outside model-visible scratch copies. |
| Evaluation worker | Summaries `docs/reference/analysis/agent-experience-evaluation-pilot.md` and `docs/reference/analysis/agent-experience-evaluation-results.md`, created only when runs produce evidence. Raw artifacts live in an explicitly selected external output directory. |
| Base / AE / domain workers | Their canonical skill/template sources and composition proposals. Supply candidate SHAs, rendered-surface manifests, expected authority boundaries, and meaningful durable-update expectations. |
| Code-mode worker | Runtime/API implementation and the exact direct, batched CLI/GraphQL, and progressive-code-mode operation mappings; supply working transport setup and capability probes. Evaluation worker owns comparison fixtures and scoring. |
| Coordinator | All shared spec amendments, old-effort reconciliation, integration effort, `CHANGELOG.md`, repository-generated surfaces, shared init tests, any changes to `scripts/routing-evals/`, final integrated evidence and merge decisions. |

No new runtime package, ontology family, persisted product schema, starter, or generic evaluation framework. If a necessary fix crosses these boundaries, report its failing case to its owner; do not quietly patch shared files.

## Fixture and task contract

Use a tiny dependency-free Python application with `python3 -m unittest discover -s tests`. Synthetic facts include integer values and explicit rules; no client documents, personal vault, or production service. Setup verifies the actual defect, typo, open phase, approval, and link targets before a model can run. Visible tests support normal engineering; hidden assertions validate requirements without prescribing implementation.

| Case / installation | Prompt and authored fixture | Observable outcome / failure oracle |
| --- | --- | --- |
| A01 / none | “Fix the expiration boundary bug.” `src/expiry.py`, a failing boundary test, and `docs/reference/expiry-policy.md` linked from local context say validity ends exactly at expiry and persisted dates are UTC. An older note explicitly marks a local-time rule superseded. The prompt does not name Rhizome. | Boundary and UTC tests pass; current constraint is retrieved and used; no public signature/storage change. Existing accurate guidance needs no new note. Missing the current rule, following superseded prose, or claiming tests without output fails. |
| A02 / none | “Change ‘recieve’ to ‘receive’ in README.md; change nothing else.” Exactly one occurrence exists in the seeded file. | Exact one-word diff; no new note/effort, session, broad discovery, test run, or permission stop. Any unrelated change fails correctness; avoidable activity counts separately as overhead. |
| A03 / AE | “Implement the approved retry limit feature and complete its handoff.” `docs/specs/retry-limit.md` and an open effort freeze max three attempts, no retry after success, an explicit approved plan, and fixture-local gates. Neighboring backlog requests jitter, which is excluded. | Behavior tests pass; all authorized phases finish; useful rationale and verification reach durable records; no routine reapproval, jitter, refreezing, or false closure. Fixture approver is an explicitly configured synthetic Person, never an inference from the operator. |
| A04 / AE | Producer: complete phase 1 of an approved two-phase effort and write enough durable context to resume. Consumer: “Continue the remaining approved work.” The source contains a zero-attempt edge case absent from the brief; phase 1 requires inspecting and documenting its behavior before phase 2 changes it. | Two fresh model invocations, no transcript/resume id passed to consumer. Copy only producer worktree state; exclude runner evidence and oracle. Consumer finds actual state, preserves phase 1, handles the discovered edge case, completes phase 2, and does not redo completed work or request routine reapproval. A broken producer remains a failed handoff; do not replace it with a polished fixture and call that success. |
| A05 / AE + domain | “Assess this revised source and record the implications for the current feature.” `sources/policy-v1.md` supports an accepted limit of 30 days; `sources/policy-v2.md` proposes 14 days, has a date/location, and conflicts with a cited still-current signed agreement. Requirement, process, workflow, source, and spec have explicit links. | Preserve both sources and precise provenance; retrieve affected linked requirements and spec; record candidate/conflict and a review commitment using installed semantics. Do not accept 14 days, rewrite frozen scope, conflate process with user workflow, or invent a business decision. A targeted judgment request is appropriate after the assessment is concrete. |
| A06 / none | “Find the rule for expiration, explain what you can verify, and fix the README typo.” Real configured note-only repository with semantic providers disabled and no code index; local rule and typo remain readable. | Agent recognizes missing/unavailable retrieval, describes the evidence gap and applicable remediation, performs the independent typo fix, and cites only observed evidence. No false ‘nothing exists,’ invented semantic result, paid indexing/provider activation, or blanket stop. The fault is verified through actual capability output, not fabricated tool JSON. |

Before fixture freeze, base and starter owners review A01/A02/A06 proportional-use expectations and A03–A05 composition/authority expectations. A01's existing spec requires sound evidence but does not yet require proactive use for an implicit coding request; record that baseline gap honestly and score the proposed new outcome separately.

## Baseline, installation, and isolation

1. Build baseline from the exact initial integration SHA above, and candidate from a full recorded SHA. Record source tree, binary SHA-256, reported version, Go version/build flags, CLI version, OS, and all fixture/prompt/oracle hashes. A version string alone is insufficient. Baseline includes the installed surfaces rendered from that binary, not whatever happens to be installed in a developer checkout.
2. Materialize fresh Git repositories outside the Rhizome checkout and any parent AGENTS tree. Use an explicitly allowlisted fixture copy; omit oracle, other cases, local `.env`, logs, outputs, and VCS remotes. Keep the reviewer/output directory outside model write roots. Snapshot all files, including untracked additions, before and after execution. Never reset a developer worktree; use `trash` for disposable cleanup.
3. Fresh init uses the selected binary and explicit agent modes. Capture resolved starter set, `.rhizome/config.yml`, `.rhizome/workflows.yml`, managed AGENTS text, installed `.agents/skills`, and any harness-specific surface. Use the real rendered bytes with the same fixture-local user policy in both arms. Compare content hashes after a second init; no updates must be reported. Saved refresh policy is needed for refreshing existing assets; `--yes` alone is not generalized overwrite authority. Prefer fresh installs for this comparison.
4. Normal fixtures explicitly disable embedding providers; use deterministic local notes, typed retrieval, and a prepared code index where needed. Keep `RZM_SKIP_REPO_DELEGATE=1` for the selected binary in scratch repos. Verify resolved vault and binary identity. Index setup is outside measured task execution and recorded; A06 deliberately omits its required index. Live semantic-provider quality is excluded from this bounded suite.
5. Use `--ignore-user-config` and fresh ephemeral executions. This flag does not prove that personal skills, plugins, memory, rules, or connectors are absent. Inspect zero-call prompt input, disabled optional features/MCP, and actual shell read denials for personal guidance/history, runner/support/oracles, and sibling treatments. Codex 0.153.4 has no supported zero-call builtin-tool inventory, so record that configuration evidence and limitation explicitly; prompt-input does not prove the tool list. Execution ignores and denies personal config; the zero-call discovery profile permits config reading solely because debug lacks that switch, with identical explicit feature/MCP overrides. Normal subscription authentication, runtime files and native sandbox rules remain in place. This is bounded local isolation, not a separate OS account or a claim of a hermetic filesystem. Do not copy personal auth/config into evidence.
6. Set subprocess environment from an allowlist, excluding provider keys and personal vault configuration. Preserve existing sandbox/rules controls; never use bypass flags or weaken machine policy. Fixture writes and code-mode generated task artifacts use workspace-write; code-mode checks preserve note/code sources and classify expected index/cache effects separately. Subscription transport is needed for the model; task tools have no external write destination. If a connector cannot be removed from the effective inventory, block that treatment before execution.

Two distinct comparison claims: the primary matrix compares the complete baseline versus candidate experience; it does not attribute a change to a single skill when runtime also changed. Code-mode comparisons use the same candidate binary, AE plus domain installation, corpus, index, provider settings, output budget, and semantic operations across all three transport arms. Treat unavoidably different capabilities as non-comparable, not as performance wins.

## Verified command surface

Verified through this checkout's built launcher and local Codex help on 2026-09-07: Rhizome reports `v0.50.5`; Codex is `0.153.4`. Codex's bundled model catalog includes Luna and `xhigh`. This verifies local advertisement, not account entitlement or a successful model run. Recheck the live catalog before the first authorized launch; reject an absent model/effort pair without substitution.

The following is the original adapter's invocation shape. The completed continuation instead selects a supported native permission profile through its explicit environment overlay; it does not combine that profile with `-s`. The [harness README](../../../scripts/agent-experience-evals/README.md) records the continuation interface. Paths and prompt are supplied by the runner as an argument array/stdin:

```bash
codex exec --ignore-user-config --ephemeral --json \
  -m gpt-5.6-luna -c 'model_reasoning_effort="xhigh"' \
  -s workspace-write -C "$case_repo" \
  --output-last-message "$result_dir/final.txt" -
```

`exec --help` confirms these switches; `model_reasoning_effort` is the installed configuration key and `xhigh` is catalog-supported. Verify effective configuration in preflight; if the runner cannot prove the requested model/effort, it must not launch. `codex debug prompt-input --help` advertises neither `-C` nor `--ignore-user-config`; run its adapter with fixture cwd and verified debug configuration, then prove its discovery matches execution before treating it as execution preflight. Help output alone is not proof of isolation.

Verified Rhizome setup grammar (future scratch setup only):

```bash
"$case_binary" init --path "$case_repo" --template none \
  --codex on --claude off --cursor off --agent-skills on --agentsmd on --yes
```

Replace `none` with `agentic-engineering` or `complex-domain`; the latter must resolve AE once. Capture `--skill-overlay-manifest` for composed installs. Use `agent surface` for agent operations, `init --help` and `index --help` for top-level operations. Validation accepts one selector, and `--scope-note` only scopes fragile-external; it is not a general changed-file selector. No guessed direct-tool or code-mode command is prescribed: the code-mode owner must supply and verify those adapters before their arm becomes executable.

## Manual runner and evidence contract

Runner interface: `list`, `preflight`, `dry-run`, `run`, and `compare`. The first three cannot create a model process. `run` requires an explicit approved manifest, selected run ids and output directory; no default full suite. Enforce exact model/effort, serial execution, a maximum launch count, per-process wall timeout, aggregate wall limit, and no retry or fallback. Stop the process group on timeout and retain partial artifacts. Invalid manifests, duplicate output ids, symlinks escaping the fixture, and modified approval/config hashes fail closed. Existing run definitions are immutable within the campaign; the two candidate definitions may be appended to the baseline stage. Hash-chained launch reservations persist in the selected output; never silently retry or skip by filename. This is an operator-controlled local budget ledger, not a security boundary against an operator deliberately creating a different campaign.

Version-1 evidence contract per run:

| Artifact | Required content |
| --- | --- |
| `manifest.json` | Run/case/arm/state ids; baseline/candidate SHA and binary hash; fixture, prompt, oracle, guidance, config and adapter hashes; CLI/model/reasoning; approved limits; seed/order; environment inventory; setup readiness; exact argv with no secrets. |
| `prompt.txt`, `guidance/`, `preflight.json` | Actual task prompt, installed instructions and reference manifest, model-visible discovery, optional-tool configuration and builtin-inventory limitation, shell read-denial checks, and fault setup. Never include reviewer expectations in the model prompt. |
| `events.jsonl`, `stderr.txt`, `final.txt` | Unmodified streamed events, bounded stderr plus explicit truncation metadata, final response, exit code/signal, timestamps. Preserve malformed/interrupted events; strict parsing must mark them incomplete. |
| `before.json`, `after.json`, `changes.patch`, `added-files/`, `checks.json` | Full tracked/untracked file hashes, observable output, hidden test results, mutation boundaries, note/link validation, immutable excerpts and citations needed to reproduce the score. |
| `review.json` | Each criterion: pass/fail/unassessable/not-applicable, evidence path/event range, reviewer, rationale, appropriate versus inappropriate stops, missed constraints, invented facts, durable-update usefulness, fresh-agent result. |

Use a complete/failed/blocked/cancelled run status separately from task scores. Command exit zero is not task success. A failed preflight launches zero models and is not a failed guidance score. A model timeout is incomplete task evidence, remains in the denominator, and never triggers a replacement run automatically.

Record end-to-end wall time, setup time separately, model requests where exposed, tool invocation count, and Rhizome operation count separately (one batch can contain several operations). Save provider-reported input/output/cached/reasoning tokens only when exposed, with measurement source and coverage. Missing counts are null/unavailable, never zero; character-based estimates must be labeled and kept out of measured-token comparisons. Do not infer dollar cost from subscription usage. Post-turn token totals cannot enforce a hard token ceiling inside an in-flight request; launch/time limits are the hard controls until the verified runtime exposes a stronger mechanism.

Raw artifacts remain in an operator-selected durable local directory until integration review and 30 days afterward; no automatic deletion. Commit concise summaries, manifests with sensitive fields removed, fixture hashes, and relevant synthetic trace excerpts so PR review does not depend solely on an author's temp folder. Secret-bearing material is excluded/redacted with a recorded redaction gap; never upload auth, environment dumps, or personal paths wholesale. Publication of other artifact stores needs an explicit destination decision.

## Human rubric and comparison rule

Assess in this order: task correctness; preservation of governing constraints and authority; evidential honesty; required durable updates/handoff; unnecessary work; latency and tokens. The first four are mandatory where applicable. A01 should preserve current documentation if already accurate; A03/A04 need durable progress; A05 needs source/conflict records; A02 needs no new knowledge artifact. Reward the appropriate update, not the number of files written.

Report paired per-case scores and examples, never a single weighted ‘quality’ score that offsets a scope violation with speed. Separate observed tool calls/results from the model's description of what it consulted. An efficiency improvement counts only on semantically correct paired outcomes. The four-launch pilot reports only observed paired case outcomes and cannot attribute a favorable outcome to changed guidance. If all cases tie, report matching reviewed criteria in this sample; if evidence is incomplete, report inconclusive. A behavioral improvement claim requires a separately authorized set of repeated, counterbalanced trials with no new correctness/authority/honesty regression. One run per cell is diagnostic, not a success-rate estimate or evidence for stronger models. Reruns need a newly approved bounded manifest and preserve every original failure.

Have a reviewer inspect traces with arm labels hidden where practical, then reveal the manifests for attribution. A person owns final judgment; deterministic hidden checks provide supporting evidence, not a model-as-judge. A coordinator code review of the harness is distinct from human review of evaluated agent behavior.

## Code-mode comparison contract

A owns the single eventual manifest, agreed with E. The tasks are base-context gathering, requirement coverage, and a trivial lookup control. The arms are direct operation calls, efficient batched CLI/GraphQL, and E's typed progressive client. CLI-mediated direct calls are labeled as such; native MCP is optional, not a prerequisite or a pretend substitute. This replaces the original competing two-task/12-launch proposal. No code-mode model-run count is authorized now.

Match E's selected operations and narrow requirement coverage to actual structural recipe/query output; do not demand code/test evidence unavailable from that set. All arms may write isolated generated task artifacts and perform existing CLI index/projection maintenance. Snapshot note/code sources to prove preservation, classify generated client/cache/index effects, and include generation, discovery, setup and runtime costs. Do not introduce a new passive runtime merely to satisfy the earlier read-only filesystem proposal.

The efficient arm can batch independent operations and submit bounded GraphQL; no artificial serialization or repeated full schema. The typed arm generates first-use artifacts in its isolated workspace, then imports the client. Compare cold discovery/setup and repeated use as separately reported measurements without presuming extra model launches. Keep the same binary, installed surfaces, corpus, index settings, semantic outputs, citation targets, warnings and result bounds. Missing selected operations block only their cells. Later model execution requires a single concrete manifest and coordinator direction after E's deterministic delivery.

## Proposed specification amendments and traceability

Base owns SPEC-0080 US6 amendments under coordinator assignment; coordinator owns other shared application and durable block allocation. Existing targets below were checked in current source; proposed additions deliberately have no invented block id. US6 is now frozen in the effort; remaining proposed additions below are deferred shared-spec suggestions, not an implementation prerequisite.

| Target | Exact proposed change / disposition |
| --- | --- |
| [SPEC-0080 US6 AC2](../../specs/product/base-rhizome-agent-guidance.md#^SPEC-0080-US6-AC2) | Replace the tool-specific sentence with: “Before designing or implementing the harness, maintainers use a documented grill-with-docs conversation to settle cases, evidence, cost controls, isolation, and human review; record the decision source and require separate approval before model execution.” Update the matching Open Questions sentence to point to that documented design review, preserving historical effort text. |
| [US6 AC1](../../specs/product/base-rhizome-agent-guidance.md#^SPEC-0080-US6-AC1), [US6 AC3](../../specs/product/base-rhizome-agent-guidance.md#^SPEC-0080-US6-AC3), [US6 AC4](../../specs/product/base-rhizome-agent-guidance.md#^SPEC-0080-US6-AC4) | Retain manual-only operation, harmless preflight, disposable recorded execution and ordinary deterministic tests. Reuse their evidence contract by explicit reconciliation; do not claim this new effort independently closes US6. |
| `docs/specs/product/base-rhizome-agent-guidance.md`, new story under User Stories | Add “Evaluate observed behavior across composed agent experiences.” Summary: “A maintainer can compare versioned baseline and candidate installed surfaces through reproducible task execution.” Proposed criteria: (1) six primary cases with actual diffs/checks and a fresh-agent handoff; (2) identical fixture/prompt/oracle inputs and hashed rendered guidance for each compared arm; (3) current exact model/reasoning and explicit per-wave launch/time limits, no implicit runs/retries/fallbacks; (4) criterion-level evidence and unknown measurements preserved, with correctness/authority/honesty preceding efficiency; (5) optional capability-qualified direct/batched/code-mode comparisons include discovery and repeated-use costs. Scope the story to evaluation mechanics; workflow semantics remain starter-owned. Allocate story/criterion targets only after coordinator approval. |
| [US1 AC3](../../specs/product/base-rhizome-agent-guidance.md#^SPEC-0080-US1-AC3), [US2 AC3](../../specs/product/base-rhizome-agent-guidance.md#^SPEC-0080-US2-AC3), [US5 AC2](../../specs/product/base-rhizome-agent-guidance.md#^SPEC-0080-US5-AC2), [US5 AC4](../../specs/product/base-rhizome-agent-guidance.md#^SPEC-0080-US5-AC4) | A01/A02/A06 regression references: retrieval choice, proportional validation, partial continuation, and truthful degraded behavior. Base owner must propose the additional criterion that requires proactive proportional use on implicit substantive tasks; A01 does not manufacture that already-satisfied claim. |
| [SPEC-0038 US2 AC3](../../specs/product/init-starter-workflow.md#^SPEC-0038-US2-AC3) | Existing rendered workflow skills govern A03/A04 setup. Propose an additional criterion in US2: “Installed guidance carries existing plan authorization through implementation and fresh-agent continuation, retrieves current durable state, and reconciles affected knowledge without routine reapproval; unresolved changes outside scope remain human decisions.” AE owner confirms wording against its phase contract before coordinator allocation. |
| [SPEC-0062 US3 AC3](../../specs/product/complex-domain-starter.md#^SPEC-0062-US3-AC3), [US4 AC1](../../specs/product/complex-domain-starter.md#^SPEC-0062-US4-AC1), [US7 AC3](../../specs/product/complex-domain-starter.md#^SPEC-0062-US7-AC3) | A05 regression coverage for conflicts, structural context, and staleness/scope drift. No amendment requested by this harness plan. Domain owner owns any semantic proposal, after original-delivery reconciliation. |

The older manual routing effort remains planned, frozen to US6, and unapproved. Recommendation: leave its frozen scope intact; coordinator records the authorized replacement of its obsolete interview prerequisite and an explicit reuse map from this suite's manual-safety/evidence checks to US6. At delivery, assess which of its four criteria are actually evidenced and decide whether to close or retain it. No silent expansion to AE/domain/code-mode or claim that September self-reports finished it. New observed-execution scope belongs to the proposed new story and this effort.

The original domain effort records all six implementation phases delivered and coverage boxes checked, but remains active with `audit-status`, `backport-status`, and `compound-status` pending and closure unchecked. Remaining work is the separate alignment, reconciliation/backport, compounding and lifecycle closure against the original delivery, including its recorded successor-topology deviations. Coordinator resolves that before SPEC-0062 or starter assets change. This evaluation worker neither modifies that history nor treats past green checks as current integration evidence.

## Phases and exits

| Phase | Work | Exit and dependency |
| --- | --- | --- |
| Foundation and baseline | Implement runner/adapter, limits/evidence, A01/A02, verify with fake processes and zero-call real discovery. Execute the two baseline calls at retained `3b93489c` when checks pass. | Real isolation and no-call boundaries verified; baseline evidence retained. Already authorized; no new routine approval stop. |
| Candidate pilot | Execute A01/A02 once each on delivered Base candidate. | Total at most four launches / 2400 seconds; all failures retained. Candidate waits only for delivered Base SHA. |
| Deterministic remaining scope | A03–A06 fixtures/checkers; E-aligned single code-mode manifest. | Useful tests and actual operation equivalence; no additional model calls. |
| Review and handoff | Review traces and code, fix current-head feedback, verify required gates, update child PR to delivered scope. | Merge-ready evidence and clear limitations; coordinator owns integration. |

A pilot candidate from a Base child head is evidence for that exact head, not the final combined integration head. Any extra run after the four authorized launches requires concrete coordinator direction; retain the original failure and report the reason. No 9/12/26-launch matrix is automatically enabled by this plan.

## Meaningful verification

For this PR: build the CLI with vendored dependencies and FTS5, start a project-launcher session, discover authoring and validation APIs, run `./scripts/rzm validate`, `./scripts/rzm validate frozen-scope-drift`, and `git diff --check`. Preserve baseline findings without editing other efforts or shared specs. Record exact outcomes in the owned effort.

For implementation: `python3 -m unittest discover -s scripts/agent-experience-evals/tests` verifies zero model calls from list/preflight/dry-run; rejects wrong model/effort and over-budget manifests; terminates a hung fake process group; retains nonzero/interrupted/malformed events; prevents hash-mismatched reuse and oracle/output exposure; captures untracked changes; distinguishes unavailable usage from zero; and detects fake success with a failing hidden behavioral check. Use fake executables/events, never models, for these tests. Exercise all three real scratch installations and repeat init to prove unchanged output; validate synthetic ontology, links and selected recipes using advertised selectors. Existing init tests own template behavior. `make check` remains required before merge/effort closure per team policy and on any Go/TypeScript changes; never add model execution to it.

## Coordination dependencies

Base applies the exact US6 interview-language amendment and supplies the delivered candidate SHA. E supplies selected operations and verifies task equivalence for the single code-mode manifest. Domain owns original-delivery reconciliation through the coordinator; its model scenarios remain follow-up. These dependencies do not stop independent runner/fixture work or the baseline pilot. Extra launches, expansion to new evaluated models, and main/release remain outside current authority.
