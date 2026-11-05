---
type: EffortNote
id: EFF-2026-11-05-16-27
aliases: [EFF-2026-11-05-16-27]
name: Code mode reliability and discovery
created-at: 2026-11-05T21:27:53Z
status: complete
summary: Make permission failures actionable and immediate, shorten routine effort discovery, and verify the installed code-mode guidance in fresh Claude and Codex sessions.
governing-specs:
  - "[[vault-runtime-coordination]]"
  - "[[persistent-agent-code-mode]]"
  - "[[saved-query-recipes]]"
  - "[[startup-code-discovery]]"
  - "[[code-mode-skill-scripts]]"
---

# Code mode reliability and discovery

## Scope

Implement the user's November 5 request to make CLI code mode reliable and easy to use, including guidance changes and clean-history Claude and Codex trials. Preserve CLI code mode, per-worktree runtime ownership, lock safety, and existing mutation authorization. No MCP migration, global permission changes, or desktop broker is included.

## Spec Set (Frozen)

- [[vault-runtime-coordination|SPEC-0104]], as present at `bb591cdb025a4f241657a39c880cf408afa58dde`: discovery, bounded waiting, single ownership, and safe fallback.
- [[persistent-agent-code-mode|SPEC-0092]], at the same revision: code-mode outcomes and host authority.
- [[saved-query-recipes]], at the same revision: bounded reusable typed questions.
- [[startup-code-discovery|SPEC-0095]], as revised under the user's subsequent request to put the connection check directly in session start.
- [[code-mode-skill-scripts|SPEC-0115]], amended during the final reliability pass to keep pending reads with their live runtime owner until ready or canceled, rather than starting a competing local refresh.

## Stories In Scope (Frozen)

The selected slice is runtime discovery and code-mode error propagation, plus recipe and guidance usability. Preserve the existing spec-level contracts. Permission denial must not masquerade as runtime absence or trigger a local writer fallback.

## Spec Coverage Checklist

- [x] Permission denial returns immediately and names the required access.
- [x] Genuine absence, unresponsive owners, and cancellation keep their established behavior.
- [x] A direct recipe returns open efforts across both representations with pagination and canonical creation dates.
- [x] Installed guidance enables short, recipe-first retrieval and correct permission recovery.
- [x] Fresh Claude and Codex sessions exercise the user-visible workflow.

## Plan

The user authorized the previously discussed permission handling and guidance improvements, plus independent clean-session trials, in the November 5 chat. This is the bounded implementation of that request; no configured current-user identity is available, so approver metadata is omitted.

1. Add permission-denial regression coverage at runtime discovery and code-mode forwarding; preserve the original cause and return without retry or in-process fallback. Update the owning runtime guidance.
2. Add an open-efforts recipe to the Agentic Engineering source templates. Improve code-mode guidance around minimal discovery, recipe selection, response envelopes, completeness, and sandbox recovery; regenerate installed copies from source.
3. Build and run focused tests, required full checks, content validation, and init idempotence. Verify denied and allowed local execution. Run the same practical lookup in fresh Claude and Codex sessions, inspect their evidence, and correct observed friction.

Escalate only if the work requires changing the host's permission policy, weakening lock ownership, replacing CLI code mode, or expanding the product architecture.

## Original Intended Delivery

A fast and understandable CLI code-mode workflow that works with authorized local runtime access, fails promptly with useful guidance when access is denied, and discovers enough related schema for a structured query in one call.

## Actual Delivered

Session start now includes a bounded runtime connection report while preserving the session and context on access failure. Code mode reports `runtime_access_denied` without waiting or falling back to an in-process writer. Permission causes survive initial discovery, registry/process startup, and post-spawn probes; failed-start cleanup respects the request deadline. The short session check preserves the normal background startup lease. A live runtime that reports a pending projection retains the read: the host retries under the original call deadline instead of opening a competing writer. Cancellation preserves the connection for a later read. Lint now excludes `.delta/worktrees` alongside the existing worktree exclusions.

Schema discovery now accepts a type or a set of types, and opt-in related-type depth up to two hops. It merges definitions and query roots, includes interface implementations, and preserves whole definitions under a default 32 KiB SDL budget (up to 128 KiB) and a fixed 32-output-type ceiling. Included types, unexpanded references, limits, and truncation reasons remain explicit. Agent CLI, code mode, and MCP share this behavior.

The Agentic Engineering starter now includes `open-efforts`, with both effort representations, creation ordering, and pagination. The source and installed Claude/Codex guidance explain selected launcher use, direct recipe invocation, exact context inputs, nested result coverage, compact projections, source reconciliation, and per-command permission recovery without a redundant verification query.

## Execution Notes

- Baseline: the initial query waited 30 seconds; its retry waited 60 seconds before a boot-time permission failure. The same query succeeded outside the sandbox in 201 ms. An isolated filesystem-restricted Codex profile with explicit local networking returned all 54 effort records in 296 ms while protected writes and external test connections remained denied. Cold startup's boot-time read remained denied.
- Initial guidance was consulted. It documented code execution and discovery, but omitted permission recovery and a compact routine-inventory route. The agent also added unnecessary identity discovery and separate schema calls.
- The runtime permission regression failed with a deadline before the fix and passed afterward. A real sandboxed query now reports `runtime_access_denied` in 0.26 seconds, without the former startup wait and writer fallback. The open-efforts recipe passes a 102-open-record fixture with both representations, creation ordering, and two pages; closed records are excluded.
- Source guidance was regenerated for Claude and Codex. `init --check` reported everything up to date. Claude's clean-history trial answered using a version check, one session start, and one recipe call in about 1.1 seconds. It identified missing recipe-pagination nesting and launcher-selection wording; both are being clarified. Its speculation that old active efforts were probably delivered was unsupported, so inventory guidance now explicitly distinguishes age from completion evidence.
- `make check-full` found a pre-existing formatting failure because the Makefile's recursive lint traverses vendor files inside `.delta/worktrees/vqgrdjwjjfrn/rhizome`. That nested checkout is unrelated and was preserved. Remaining checks continued.
- The revised Claude trial used the project wrapper and the start connection result, with one inventory recipe and batched effort-context reads. Its feedback led to explicit recipe input and workspace-history guidance. A separate code review found three cold-start gaps: registry/process permission errors, post-spawn permission loss, and unbounded failed-start intent cleanup. Regression tests now cover these paths; error propagation and cleanup deadlines were corrected.
- Cold fixture: denied startup returned `access_denied` in 18 ms; authorized startup returned `starting` in 62 ms. The original session then served a code-mode file request in 42 ms. A query on the fixture correctly reported missing ontology, an unrelated capability prerequisite rather than a connection error.
- The final Codex trial saw `access_denied` in session start and used the same session with authorized runtime-backed calls; no second start or verification query was needed. It recovered complete inventory and linked delivery evidence. The final Claude trial saw a ready connection and completed the same tasks. Follow-up guidance now gives exact context inputs and compact result projections, and requires reconciling explicitly linked delivery records.
- The three review regressions failed against the original `ensure.go` through a temporary compiler overlay, and passed with the fixes. Race-enabled focused tests passed for runtime access/startup, agent start, code-mode failure handling, the live start schema, and open-effort pagination.
- Full verification also caught the base skill's 80-line limit after the new instructions were added. Redundant section markup was consolidated without raising the limit. The original full run's unrelated cancellation timing assertion passed in the later full run; the nested `.delta` formatting failure remains outside this change.
- Final verification: `NO_WEB=1 make build` passed; `./bin/darwin/rzm init` followed by `./bin/darwin/rzm init --check` reported everything up to date. `make test-content file-size-check` passed after the skill correction. All changed Go files passed `gofmt -l` without output.
- Final focused race checks passed: `GOCACHE=$PWD/.gocache go test -tags fts5 -race ./pkg/app/runtime ./pkg/app/agentapi ./pkg/app/agentcode ./cmd ./pkg/app/cli/init -run 'TestEnsure|TestPostSpawn|TestFailedSpawn|TestStartContract|TestAgentStart|TestCodeModeAccessDenial|TestOpenEffortsRecipe|TestMarkdownSkillTemplatesUseSetupContracts' -count=1`. HTTP fixtures require local socket permission; the default sandbox denied fixture binding, and the authorized rerun passed.
- `make integration-packages integration-mixed web-test` passed: all runtime and progressive code-mode integration packages, mixed integration checks, and 1,832 web tests across 203 files. The full race-enabled unit run passed except for the since-corrected skill-length check. `make check-full` remains failed because recursive lint traverses the unrelated nested checkout's vendor files; that checkout's owner must resolve its formatting or lint scope before the full gate can be green.
- Final built-CLI smoke test: `./scripts/rzm agent start` returned `access_denied` with a usable session in 33 ms. The next authorized `agent code execute` reused it, ran `open-efforts` in 282 ms, and returned 15 rows with `hasMore: false` and `status: complete`. No repeated start or separate connection probe was used. The changelog records the new behavior.
- Final documentation checks passed: `./scripts/rzm validate` reported zero issues across ontology, identifiers, and broken links; `./scripts/rzm validate frozen-scope-drift` reported no drift. `git diff --check` passed. Runtime checks used explicit per-command permission; no host or global permission policy was changed.
- Follow-up GraphQL usability check: three type-fragment requests took 123, 140, and 126 ms wall time; three typed queries took 356, 260, and 252 ms. These timings include CLI/Node startup but exclude host permission-review overhead. A single execution fetched `Effort`, inspected its `referencedTypes`, and fetched `SpecLike` in 146 ms, returning 2,974 bytes. Full schema discovery returned 64,859 bytes of payload. The measured CLI path is quick; returning unneeded schema and adding agent turns remain avoidable costs. The guidance now explicitly routes structured questions to recipes or GraphQL and demonstrates batched type discovery followed by a nested query. That query ran successfully against the live schema and preserved pagination coverage.

## Deviations

The user subsequently approved implementing bounded schema side-loading after the GraphQL guidance discussion. This adds `types`, `depth`, and `maxBytes` to the existing shared discovery operation, preserving full unselected discovery and focused single-type defaults. The source guidance now uses the implemented one-call route.

The user refined the implementation during verification: session start itself should verify the connection instead of requiring another command. The plan now includes an additive bounded `runtime` connection report in `agent start`, with connected/starting/access-denied/disabled/unavailable states. Minimal startup still avoids index initialization and full runtime waits. Fresh-agent results before this change remain baseline evidence; final trials must use the revised start behavior.

## Schema Expansion Verification

- Regression tests failed before implementation for related-type inclusion, byte limits, and invalid options, and passed afterward. Additional cases cover cyclic references, multiple selections, deduplication, abstract members, the 32-type limit, strict input shapes, and CLI/MCP parity.
- The built CLI fetched `Effort`, `EffortNote`, `EffortWorkspace`, and `SpecLike` with `depth: 1` in 147 ms, returning 4,723 SDL bytes without truncation. A 2,000-byte request retained the complete `Effort` and `SpecLike` definitions and reported `truncated: true`, `limitReasons: [maxBytes]`, and the unexpanded frontier. These timings exclude host permission-review time.
- Independent review found no correctness issues in expansion, bounds, cycles, abstract members, metadata, or surface agreement. Full unselected discovery retains its original response shape.
- Query, pushdown, and recipe package tests passed with the race detector, as did focused agent CLI, code-mode input, and MCP checks. The Python CLI integration schema snapshot passed. The final `__typename` regressions failed before the fix and the full query package passed afterward with the race detector. Build, generated-guidance rerun, content/size checks, and document validation passed. `make check-fast` remains blocked only by the existing nested `.delta` vendor formatting.
- Fresh Codex and Claude trials both used `ontologyQuerySchema({type: "Effort", depth: 1})` and had enough schema to query efforts with governing spec IDs and summaries without further expansion. Codex recovered from initial denied access using the same session. Claude exposed a pre-existing `__typename` execution bug; regression tests reproduced it on an interface root and nested link, and the node resolvers now return their concrete type identity. Guidance clarifies optional truncation fields, interface fragments, and failed operations that still contain partial data.
- Code mode remains a focused reference inside the Rhizome skill. The base skill now names code-mode scripts and GraphQL in its discovery description, explicitly routes code-mode requests to the reference, and requires reading it before the first `agent code execute`. This keeps one owner without introducing a wrapper skill and another lookup.

## Closure Checklist

- [x] Full gate passes. `make check-full` completed successfully after the cold-runtime, terminal-error, startup-budget, and lint-scope corrections. It includes race-enabled unit tests, 1,832 web tests across 203 files, runtime/progressive code-mode integration packages, mixed integration checks, and benchmark contracts.
- [x] Source and installed guidance agree; init rerun is clean.
- [x] Both clean-session trials are reviewed.
- [x] Actual delivery and remaining host limitations are recorded.

## Cold Runtime Follow-through

The user asked whether the work was done and more fresh-model rounds would help. Rather than repeat successful warm-session trials, the final reliability pass addressed the observed cold-runtime delay. The trace showed a 6 ms runtime response followed by approximately 30 seconds in a local `validation-refresh` writer-lock wait. A one-second regression with a held writer lock reproduced the failure deterministically.

The host now retries a pending read with the live owner under the existing operation deadline. A focused review found that the old pending signal also hid permanent readiness errors. Separate regressions cover index, semantic, source-sync, and proof-read failures; these now return directly, while an unfinished watcher or an attempt deadline remains pending. The same regression succeeds when the runtime becomes ready on its next response, and cancellation followed by recovery reuses the connection. The replay immediately after rebuilding completed in 17,347 ms while the runtime initialized; its retained command report records one forwarded call, zero in-process calls, and no errors. Cold indexing still takes time, but the client no longer adds a competing writer wait.

The previous full-gate formatting blocker came from lint traversing a nested `.delta/worktrees` checkout. Its exclusion now matches the existing worktree exclusions; no nested-checkout files were changed. The final replay after the terminal-error correction completed in 22,008 ms with three rows and intact coverage. Full verification also caught and corrected two expectations: permanent/canceled readiness diagnostics now preserve their cause, and an exhausted internal spawn budget keeps `ErrNoRuntime` while caller cancellation keeps its own cause.

## PR Review Follow-through

Greptile's first review scored the branch 4/5 and identified two correctness gaps. G1/P1: watcher preemption could terminate a healthy query. Source synchronization now treats owner cancellation and lane preemption as pending while preserving caller cancellation and permanent failures; the focused regression failed before the fix and passes afterward. G2/P2: untyped records could return an empty or interface `__typename`. Executable `UntypedNote` and `UntypedSection` identities now match their fragments while `resolvedType` retains the ontology classification. The fallback regression and schema/introspection checks pass, and the CLI schema snapshot includes the concrete fallback note type.

Sol-loop round 2 with GPT-6.1 Sol at xhigh on standard service approved these changes and marked G1 and G2 resolved. Across two rounds there were no Sol findings to decline, and usage was not returned. These fixes stay within the original reliability and query contract scope. The rebuilt branch then passed `make check-full`, including the Python CLI schema snapshot, plus `init --check`, default validation, and frozen-scope validation.

## Compounding Follow-ups

Host policy controls local access; repository settings cannot grant it. Preserve the readiness-versus-connectivity distinction, and keep pending runtime reads on their existing owner.

## Status

Complete. Implementation is carried by [d7321464](https://github.com/atomicobject/rhizome/commit/d7321464299924f29d3f1472557489f1f88e7ac6). The final `make check-full`, regenerated-guidance stability, content/frozen-scope validation, clean Claude/Codex trials, and original cold-read replay passed. Sol-loop approved the full scoped behavior diff and subsequent Greptile fixes in two rounds with GPT-6.1 Sol at xhigh on standard service. Review usage was not returned; the two corrected findings remain within the approved scope. Full-send authorization covers this closure, PR, review fixes, CI, and merge. Delivered in [PR #99](https://github.com/atomicobject/rhizome/pull/99); hosted CI and the required Greptile review gate apply before merge. The host still controls permission grants; this change reports denied access and supports recovery without changing host policy.
