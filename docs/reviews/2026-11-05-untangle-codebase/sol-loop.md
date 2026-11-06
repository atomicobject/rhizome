---
summary: "Sol-loop approval and scope ledger for the first codebase untangle pull request."
reference-kind: analysis
---

# Sol-loop: first untangle pass

The first round approved the complete behavior diff with no findings. PR preflight then found overlap with current main. That integration is recorded below and receives a narrow second review before publication.

- Provider: Codex Work.
- Model: GPT-6.1 Sol.
- Reasoning: xhigh. Service tier: ultrafast.
- Base: `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`.
- Reviewed head: `0945364e5594329d2271d8c4c247887fbd9fa61b`.
- Diff command in every round: `git diff 900eee0b0036ee2699d84d5d02d5ffe6a39a4441`.
- Before review: 294 files changed, 8,578 insertions, 7,081 deletions.
- Round-one result: the same behavior diff, plus this review ledger. Current-main integration is a separate delta below.
- Token usage was not exposed by the review result or thread readback.

| Round | Findings | Outcome | Fixed | Declined |
| --- | --- | --- | --- | --- |
| 1 | None | APPROVE | None required | None |
| 2 | None | APPROVE: main integration | None required | None |

The first round approved its reviewed behavior diff with no blocker or should finding. The existing [delivery evidence](delivery-evidence.md) supplies execution results; the reviewer performed a static review and did not claim to rerun them.

## Reviewer report

VERDICT: APPROVE

No findings.

Scope: Reviewed the diff from `900eee0b0036ee2699d84d5d02d5ffe6a39a4441` to `0945364e5594329d2271d8c4c247887fbd9fa61b`, including new files, affected callers, cross-lane seams, and test cleanup. HEAD matches the requested reviewed commit. `git status --short` is empty; there are no untracked files to review.

Read AGENTS.md, the approved plan, delivery-evidence.md, and relevant owning subsystem guidance. Applied the narrowed plan and its preserved distinctions rather than the historical audit proposals. Examined the bootstrap/cache/view moves, graph presentation and caller policies, operation catalog and validation wiring, writer scheduling and embedding cancellation, fallback heading identity, repository trust, provider settings, supervisor recovery, process shutdown, and generated script ownership. No concrete in-scope regression or broken caller was identified.

Limits: Static source review only. Builds, tests, and interface checks were not run; execution results are the documented delivery evidence, not independently reproduced results. No source edits, commits, pushes, PR comments, or subagents. Only this report was written.


## Current-main integration

Merged `ccdeffe26ee7878c243394872c708b871006f19b` from current main before opening the pull request. Four workflow conflicts preserve main's release topology, dependencies and scanner settings while using the root or team-service `go.mod` for all 15 setup-go versions. Two catalog conflicts retain per-operation descriptors and carry main's expanded ontology schema-discovery inputs/output and additive session-start runtime status into their owning factories. Only those two of the 37 catalog fingerprints changed. Automatic merges retain main's runtime/query fixes, temporary-fixture isolation and the native supervisor test gate.

Verification before the integration commit:

- `GOCACHE=$PWD/.gocache go test -tags fts5 ./pkg/app/agentapi -count=1` passed after the two expected snapshot updates.
- `TEAMAPI_DYNAMODB_ENDPOINT=http://127.0.0.1:65023 /tmp/rhizome-untangle-pinned make verify` passed with Go 1.27.3, golangci-lint 2.15.0, Node 22.23.1 and gitleaks 8.30.1 selected explicitly. Build, race suites, integration suites, 1,844 web tests, ten supervisor tests and 112 browser tests passed. The team-service tests used the disposable DynamoDB Local endpoint.
- `./scripts/rzm init --check` reported everything up to date.
- The same pinned-tool environment passed `make check`; `./scripts/rzm validate` and `./scripts/rzm validate frozen-scope-drift` both reported zero issues.

The initial shell selected Go 1.24.2 and an older linter despite the pinned tools already being installed. One attempt also retained the old GOROOT. The direct pinned-tool invocation resolved these host setup failures without changing source, weakening a check, or disabling artifact attestation. Logs are `/tmp/rhizome-untangle-sol-merge-verify-tools.log` and `/tmp/rhizome-untangle-sol-merge-init-check.log`. The helper only sets the selected tool paths and execs its arguments.

Round two approved the integration at `b0d0cf824a9b7b7fc9e1832fd31aa5e7555e2f0e`, with no findings. Both rounds are complete, with no fixed or declined findings and no outstanding blocker or should. No source changed after this approval. Token usage was not exposed.

Final PR diff against the integrated main: 295 files, 8,629 insertions and 7,080 deletions before this approval note. The same original-base diff is 401 files, 13,990 insertions and 8,023 deletions because it also includes intervening main changes. Those upstream additions are not attributed to this refactor or to Sol fixes.

## Round-two report

VERDICT: APPROVE

No findings.

Reviewed head: b0d0cf824a9b7b7fc9e1832fd31aa5e7555e2f0e.

Scope: Narrow integration recheck after round one's approval at 0945364e5594329d2271d8c4c247887fbd9fa61b. Compared the merge against both parents, c0cc032caf92b2b8ea1633cdc200f39418f3294e and ccdeffe26ee7878c243394872c708b871006f19b, and inspected git show --remerge-diff. Retained the loop's stable base, git diff 900eee0b0036ee2699d84d5d02d5ffe6a39a4441. Earlier approved behavior and changes introduced only by main were not reopened as review scope.

Integration evidence:

- All four workflow resolutions match main byte-for-byte after normalizing the 15 intended Go-version substitutions. Root jobs use go.mod; the team-service job uses services/team-api/go.mod. Both modules specify Go 1.27.3. Main's release topology, dependencies, GOTOOLCHAIN=local, and scanner behavior remain intact.
- The ontology_query_schema and start contract expressions exactly match main in their new per-operation factories. Inspected the relocated descriptions/defaults, expanded discovery output, input validation and CLI/MCP/local decoding, plus start's runtime response and callers. Only the two expected descriptor snapshot hashes changed.
- Inspected the runtime-read retry and cancellation path through the shared outcome adapter, live-owner readiness and refresh requests, and the refactored bootstrap entrypoints it calls. No lost main behavior or competing writer was identified.
- Inspected typed/untyped GraphQL names and fragment matching alongside the retained shared property lookup. Checked the Makefile fixture-path changes, retained native supervisor gate, and GraphResponse migration in server tests. No broken intersecting caller was identified.
- Every file changed only on main is unchanged from the main parent in the merged tree. The worktree is clean.

Limits: Static source and Git review only. No tests, builds, interface checks, CI runs, or release workflows were executed. The reported successful make verify, make check, init --check, documentation validation, and frozen-scope validation are root's recorded evidence, not independently reproduced results. Rhizome context lookup failed because local authentication was unavailable; repository source, policy, subsystem guidance, the prior report, and the review ledger supplied the review evidence. No repository files were edited; no commits, pushes, PR comments, or subagents. Only this report was written.

