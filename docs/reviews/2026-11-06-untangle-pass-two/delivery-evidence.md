---
summary: "Second untangle pass implementation, observed failures and verification evidence."
reference-kind: analysis
---

# Second-pass delivery evidence

All thirty audited groups and the gate-discovered corrections are implemented and locally verified under the [bounded plan](plan.md). The delivery target is [PR #101](https://github.com/atomicobject/rhizome/pull/101). Existing subsystem contracts and first-pass retentions remain intact.

## Baseline probes

Root ran `/tmp/rhizome-untangle-pass2-baselines.py` with the repository-pinned tools and disposable fixtures. Exact commands/results are in `/tmp/rhizome-untangle-pass2-baselines.json`.

- Init: `TestRunStaleCommandCleanupPreservesOwnership` reproduced an omitted deletion preview and actual removal of an unrecorded user command file. The replacement must use recorded ownership and normal removal decisions.
- Tasks: `TestCheckOutputRequiresOneCompleteJSONValue` reproduced acceptance of trailing JSON closers in both scheduled and draft runs. Valid objects, whitespace and the existing second-value rejection were controls.
- Capabilities: `TestOntologyEndpointsWithSchema` reproduced omission of the already-emitted `view_preferences.changed` event.
- Indexer timeout: `TestIndexFileTimeoutPreservesSelectedIndexerLanguage` reproduced a PHP indexer returning empty or path-inferred Go identity. The batch caller fills empty language and timeout artifacts are not trusted for freshness, so this is helper identity evidence, not proof of corrupted persisted PHP data. No index version bump is claimed.
- Desktop quoting: `TestCLILinkPrivilegedCommandShellBehavior` passed against the original implementation using unprivileged `/bin/sh` and temporary paths with apostrophes, substitutions and an ownership swap. The shared quoting implementation must retain that behavior.
- Language characterization initially expected the wrong result for unknown extensions. The test expectation was corrected to preserve the actual strong match and single result; the corrected baseline passed. Production behavior was not changed to fit the original test assumption.
- The initial query SECTION fixture used an unsupported locator. It was corrected using the existing generated-section locator pattern, and the public fragment/relation characterization passed before the traversal change. A temporarily incomplete language-helper migration caused one compile failure; no query regression was inferred from it.
- A complete caller search found the old language helper was also used by `NotesForFile`. A real-store regression reproduced a PHP file incorrectly matching a Python anchor; the unknown-extension control retained both candidates. That caller joins the canonical language migration. Logs: `/tmp/rhizome-untangle-pass2-query-characterized-3.log` and `/tmp/rhizome-untangle-pass2-baseline-php-notes.log`.

## First-pass publication

[PR #101](https://github.com/atomicobject/rhizome/pull/101) carries the completed first pass. Two Sol rounds approved it without findings. The initial Greptile CLI review at `4c08a5980eaa2088f097f2fe242e31987261b779` returned confidence 5/5 and an empty comments array. This score covers the first-pass commit, not the unfinished second pass. The raw review is `/tmp/rhizome-untangle-pr101-greptile-initial.json`.

## Delivery boundary

Implementation, caller migration, relevant test cleanup, local verification and fresh reviews are complete. Use PR #101 for the current remote head and CI state. External Go consumers remain unverified. No merge, production installation, deployment or real provider request is included.

## Focused verification

All eight groups in `/tmp/rhizome-untangle-pass2-focused.json` passed on the integrated implementation: PHP timeout and note lookup, language outcomes, GraphQL type matching, task-check JSON, init ownership/compression, event/capability delivery, real-shell CLI link commands, and the full harness subtree. Each listed command used the repository-pinned tool environment. The broader verification results and fresh reviews are recorded below.

## Independent review

Three fresh source reviews approved the integrated second-pass diff against `4c08a5980eaa2088f097f2fe242e31987261b779` with no actionable findings: core/dataflow/query/graph paths; runtime/harness/tasks/init ownership; and browser/desktop/event contracts. Their reports are in `/tmp/rhizome-untangle-pass2-review-{core,runtime,web}.md`. Reviewers did not run tests and did not claim independent runtime verification. They explicitly inspected test removals, maintained boundary policies and new shared owners.

The first full-gate attempt found an unused import after moving event constants and two files above their size limits. Root removed the import and redundant implementation comments, without changing behavior or raising any limit. The full gate passed after those fixes.

A post-change whole-source symbol scan checked 43 retired names. Forty-two have no source references; the remaining `SymbolKeys` spelling belongs to the retained footprint field, not the removed residual accessor. Exact evidence is `/tmp/rhizome-untangle-pass2-retired-symbols.json`.

The completed Go race suite, integration suites, 1,844 web unit tests and ten supervisor tests passed in the full-gate rerun. All 112 browser tests also passed in `/tmp/rhizome-untangle-pass2-verify-fixed.log`.

## Candidate accounting

All thirty audited groups are implemented; no candidate was silently dropped. The linked audits preserve the intentional exceptions and supporting caller evidence.

| Groups | Delivered |
| --- | --- |
| C2-01..03 | Removed the twelve unused Store entry points, four unused path/extension helpers, duplicate slash-splitting helpers and nearby narration. Active migrations and transactional cleanup remain. |
| Q2-1..4 | Removed the unused sort adapter; shared interface traversal behind existing GraphQL root policy; shared exact metadata-bullet recognition; removed impossible local errors. |
| I2-01..07 | Shared path/parser language policy including PHP lookup; retired seed/answer/codeintel/residual/progress/row-count APIs; migrated useful assertions to live paths. |
| R2-RT-1..8 | Shared graph-path preparation, MCP environment parsing and Go event values; removed unused mutation/readiness/credential APIs; fixed task JSON EOF validation and cancellation-test synchronization. |
| W2-1..4 | Removed unused declarations; advertised the existing preference event; shared browser event spelling and POSIX quoting; shared the two matching invalidation-stream transport loops. |
| T2-TOOL-1..4 | Put stale command cleanup through recorded ownership and normal decisions; shared compression activation; removed unused defaults and lint helper. |

Useful test coverage remains at the actual callers. Removed tests only exercised retired helpers or broad prefix deletion; replacements assert production seed/answer/progress behavior, file preservation, deletion authority, reporting, shell execution and exact stream frames. No new dependency, schema migration, provider call, deployment or compatibility shim was introduced.

## Gate-discovered subprocess failure

The initial pre-commit run failed one compounding fixture: its second ordinary-mode invocation exited 1 instead of the expected stopped exit 3. Fifteen isolated runs passed. Root preserved the failure log and added stderr to that assertion; no expected result or budget was weakened.

A deterministic early-exit fixture then reproduced an unhandled stdin `EPIPE` in the shared CLI client, for both a rejecting child and a child claiming success without consuming its request. The client now records stdin write errors until process closure, preserves child stderr first, and rejects an otherwise zero-exit result when request delivery failed. Both new cases and the complete compounding script suite pass. The [fresh pipe review](pipe-review.md) approved this delta without findings. The independently reproduced defect is established; the original intermittent sample lacked stderr, so its exact cause cannot be proven retrospectively.

Logs: `/tmp/rhizome-untangle-pass2-pipe-red.log`, `pipe-green.log`, `compounding-green.log`, and `task-repros.json` under the same prefix. The canonical template was rebuilt; init previewed and refreshed exactly its generated client file, and the canonical/generated bytes match. Init then reported convergence.

## Platform CI corrections

The first-pass remote head was mergeable, with most jobs green, but Linux web tests and Windows rest-unit tests failed. Logs were captured in `/tmp/rhizome-untangle-ci-{web,windows-rest}.log`.

- Linux: both supervisor failures used a synchronous synthetic ready manifest and failed the strict freshness check. A copied-test coarse-timestamp simulation reproduced both original failures; after explicitly timestamping that fixture and observing restart rejection, the same simulation passes. Production discovery and timeouts are unchanged. The original runner's exact timestamp delta was not logged.
- Windows shutdown: TerminateProcess may report access denied for an already terminated child. The post-kill reaped branch now recognizes only the actual Windows error and only with the exact child exit signal closed; open/nil signals and other errors remain failures. Windows-only tests cover both sides.
- Windows trust: using a regular file as the store directory produces a missing-path result on Windows rather than a read failure. The test now puts a directory at the actual trust-record path, preserving the integration assertion and fail-closed behavior on both systems.

Fresh [Windows](windows-review.md) and [supervisor](supervisor-review.md) source reviews approved these corrections without findings. Local focused tests and Windows test cross-compilation passed; actual Windows execution is delegated to the updated PR CI.

## Final verification

| Check | Result and scope |
| --- | --- |
| Focused behavior | All eight initial groups passed; additional pipe and coarse-timestamp regressions failed before their fixes and passed afterward. |
| Full local gate | `make verify` passed after the compounding client fix: build, fast checks, race tests, integration suites, 1,844 web tests, ten supervisor tests and 112 browser tests. Late CI corrections were then covered by the final pre-commit and focused checks below. |
| Final pre-commit | `make check` passed with the final CI corrections present. |
| Late CI corrections | `go test -race -tags fts5 ./pkg/harness/internal/jsonstdio ./pkg/repositorytrust -count=5` and all ten Node supervisor tests passed. Windows-specific test binaries cross-compiled with `GOOS=windows GOARCH=amd64 CGO_ENABLED=0`; this is not Windows execution. |
| Desktop | `make desktop-deps`, `make desktop-check` and `make desktop-build` passed. Checks included 113 UI tests, 111 Rust tests, clippy and app-instance lifecycle fixtures. |
| Actual CLI/init | Both the built root CLI and the CLI inside the packaged desktop app initialized disposable Git repositories, preserved five custom command files through check/apply/check, and converged. Root and packaged CLI build metadata share the source revision but use their intentional different desktop gateway flag. |
| Generated source | Init previewed exactly `.rhizome/modules/compounding/scripts/lib/calls.mjs`; apply and subsequent check passed, with canonical/generated byte equality. The generated-file fingerprint changed through init. |
| Documentation | Default validation and frozen-scope validation passed with zero issues; final publication validation runs after this report update. |
| Reviews | Three second-pass integration reviews, the subprocess-delta review and two CI-delta reviews approved without actionable findings. Source reviews did not claim independent test execution. |

All commands used the installed repository-pinned Go 1.27.3, golangci-lint 2.15.0, Node 22.23.1 and gitleaks 8.30.1 via `/tmp/rhizome-untangle-pinned`. Billing tests used the disposable DynamoDB Local endpoint, never a live provider. Detailed local logs and the command/result manifest are under `/tmp/rhizome-untangle-pass2-*`; packaged init evidence is `/tmp/rhizome-untangle-pass2-packaged-init-result.json`. The generated web build output was restored to its tracked source version rather than committing hashed build assets.
