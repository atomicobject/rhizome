---
type: ReferenceDoc
reference-kind: analysis
summary: "Full persistent code-mode delivery verification, six-run pilot evidence, and corrected runtime-file integrity assessment."
status: active
last-verified: 2026-09-07
---

# Persistent code-mode delivery and evaluation

## Outcome

The full persistent agent surface is implemented and independently reviewed: 31 operation families cover 40 operational CLI leaves, including note move. One generated client owns one persistent child process, with bounded serial execution, selective contracts, cancellation evidence, per-request application freshness and explicit mutation authority. Base, Agentic Engineering and Complex Domain guidance installs and runs successfully.

The six-run pilot does **not** establish a general task-speed or context-cost advantage. Persistent mode was about equal on the small repair, slower on effort delivery and faster on source review. Deliverable scope and tool-use behavior also differed. All original functional/provenance checks passed; the tree-integrity checker incorrectly counted derived SQLite files as unauthorized changes. That checker is fixed, with a separately recorded post-run correction. Original results remain unchanged and formal comparisons remain inconclusive without human rubric review.

## Candidate and verification

- Evaluated source: `8d84d11ee743a51112e0a11b389f302bb79ae556`, clean when built.
- Evaluated binary SHA-256: `be8fcb81e49684dbbe86ebc4639b161744699225646f84b8bd81b8c1f31a4615`.
- Full `GOCACHE=/private/tmp/rhizome-persistent-go-cache CHECK_JOBS=3 GO_TEST_PACKAGES=4 GO_TEST_PARALLEL=4 make check`: passed, including race-enabled unit/integration and web checks.
- `make build`, default documentation validation and `validate frozen-scope-drift`: passed.
- All 31 generated operation declarations: TypeScript strict compilation passed.
- Fresh base/AE/Domain installations: installed Node examples passed; repeated init preserved their bytes.
- Real-process tests: payload parity, one child per client, external edits/index replacement, mutation authority, queue overflow isolation, EOF/cancellation handling, and validation plan/apply/postcheck passed.
- Independent full implementation review at that revision: **ship**, no findings; [review receipt](persistent-code-mode-evaluation-evidence/implementation-review.json). Earlier review findings were corrected before evaluation.
- Post-pilot checker correction: `python3 -m unittest discover -s scripts/agent-experience-evals/tests` passed all 42 tests. The separate [integrity review](persistent-code-mode-evaluation-evidence/integrity-review.json) returned **ship**, no findings, and reconciled all 18 input hashes. This Python-only change and the evidence documentation were made after the evaluated binary; the binary and frozen campaign were not rebuilt or rerun.

## Six authorized runs

Requested model/effort: `gpt-5.6-luna` / `xhigh`, through the Codex subscription harness. Exactly six launches were consumed in the declared order, without retries or API-key fallback. All six processes completed and all evidence captures reported no artifact errors. Requested model metadata is not proof of the server's runtime model selection.

| Case | Direct seconds | Persistent seconds | Direct input tokens | Persistent input tokens | Direct output tokens | Persistent output tokens |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| A01: bounded expiry repair | 102.8 | 101.4 | 306,483 | 272,412 | 4,641 | 4,996 |
| A03: approved effort delivery | 185.2 | 544.0 | 752,622 | 2,140,003 | 9,246 | 14,779 |
| A05: changed-source review | 539.8 | 312.8 | 3,930,025 | 1,927,592 | 24,583 | 14,577 |

Source: [run summary](persistent-code-mode-evaluation-evidence/summary.json). Input totals include cached tokens; output totals include reasoning tokens. These are cumulative turn totals, **not** peak context sizes or atomic model-call counts. Exposed tool-event counts were respectively 10/17, 16/28 and 36/57; they include shell/file operations and do not count individual Rhizome operations reliably.

## Integrity correction

The original checker reported `no_unexpected_added_paths` for `.rhizome/db.sqlite` in every run and WAL/SHM additions in A01 persistent. Every other original deterministic check passed. Rhizome normally updates this derived database during retrieval and indexing, so treating those exact paths as authored deliverables was a harness defect.

The corrected checker permits only `.rhizome/db.sqlite` and its exact `-wal`, `-shm`, and `-journal` sidecars. It still records their added/removed/modified paths and snapshot hashes. It does not exempt `.rhizome/` generally: configuration, ontology, guidance, source files and lookalike names remain protected. Regression tests cover runtime creation/modification/removal and rejected authored/lookalike changes.

The [correction receipt](persistent-code-mode-evaluation-evidence/integrity-correction.json) recomputes path differences from each saved before/after snapshot, verifies those differences match the original check, and applies the corrected path policy. All six pass that corrected integrity classification. The receipt includes original input hashes and the corrected checker hash. This is a **post-run diagnostic correction**, not a new campaign or retroactive replacement of the original checks. Original [comparison results](persistent-code-mode-evaluation-evidence/comparison.json) remain inconclusive; no human review scores were invented.

## Trace and deliverable observations

- **A01:** Both arms made the same boundary fix. The persistent arm used a short generated script with one client, four operations and explicit close. Task elapsed time was essentially equal.
- **A03:** Both arms fixed the same retry boundary and recorded test evidence. The persistent arm constructed an interactive Node driver to retain one connection across shell steps, recovered from an initial import error and performed more wrapper/validation work. Its effort remained active pending a separate closure decision; the direct arm marked its effort complete. Both passed deterministic handoff checks, which do not establish equal human judgment or complete workflow equivalence.
- **A05:** Both arms preserved the accepted 30-day contract, the candidate 14-day proposal, exact source provenance and unresolved partial delivery. The direct arm also repaired typed requirement/source/spec/story links; the persistent arm updated only the durable effort record. Persistent elapsed time was lower, but its narrower output means the difference cannot be attributed wholly to transport. Its trace also uses direct `agent start` and a final direct `agent validate all` around the generated-client phase: this is not a pure all-operations-through-one-connection treatment.

Per-run directories contain the original checks, sanitized patches, agent messages and command excerpts. Commands over 5,000 characters are explicitly marked truncated. Raw event streams remain outside the repository; event hashes bind the compact evidence to them. The pilot has one observation per arm/case, fixed order and no human rubric review. It supports concrete workflow improvements, not statistical or priced savings claims.

## Model-free timing probe

Two rounds each performed `fileContext`, `files` and `queryRecipe` against separately initialized/indexed equivalent fixtures. Direct CLI rounds took 130.4/128.5 ms. Persistent rounds took 82.3/61.8 ms; the first includes child startup and handshake. Node startup/imports, both rounds and close totaled 174.3 ms; generation added 31.9 ms. Including generation, persistent total was 206.1 ms versus 258.9 ms for direct calls, about 20% lower in this small probe.

All six normalized payload comparisons passed. Normalization covered vault path/name, ephemeral session ID and file enumeration order. Indexed enrichment was available, with three reads and zero results for the measured file. Direct ran first and OS caches were uncontrolled. Two rounds are descriptive, not task-level or statistical evidence. [Probe receipt](persistent-code-mode-evaluation-evidence/microbenchmark.json).

## Context size and next improvements

Code mode helps when agents discover only selected operations and aggregate/filter/project outputs in JavaScript before returning them to the model. The transport alone does not shrink a full schema or search packet. Printing every returned object preserves that context cost; building elaborate drivers can add more.

Selective operation discovery is already delivered: the measured `code describe --operation file_context` was 1,852 characters versus 53,240 for the ordinary full CLI surface. Compact ontology contracts, consistent search narrowing and less repeated source content remain proposed work. See [[agent-tool-signal-to-noise|measured signal-to-noise assessment]] for evidence and a bounded follow-up sequence. Search-tail cutoffs need corpus evaluation before changing defaults.

## Cost accounting

The [API-equivalent cost receipt](persistent-code-mode-evaluation-evidence/native-cost-receipt.json) is **unavailable** for native orchestration: parent/delegate/reviewer atomic usage is not exposed and whole-task coverage is incomplete. Its pricing snapshot is dated 2026-09-04. Routed cost, Astra repricing and their difference are unavailable. The pilot's cumulative turn usage cannot be priced as one atomic short-context call. No subscription charge or dollar-savings claim is made.
