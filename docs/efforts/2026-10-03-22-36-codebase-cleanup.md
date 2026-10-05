---
type: EffortNote
id: EFF-2026-10-03-22-36
aliases:
  - EFF-2026-10-03-22-36
name: Foundational codebase cleanup
created-at: 2026-10-04T02:36:10Z
status: complete
summary: Audit Rhizome from its foundations upward, fix demonstrated defects, simplify its implementation, and collect independently reviewed child PRs in one integration PR.
---

# Foundational codebase cleanup

## Scope

Drew requested a holistic review and cleanup before open source publication. Priorities are correctness, performance, clear ownership, simpler internal structure, and useful tests. Work covers runtime and storage, indexing, the typed graph and query engine, retrieval, validation, application boundaries, and the web client.

Each change needs a concrete benefit. Performance changes compare the same workload before and after. Test removal preserves observable behavior and failure coverage. Public behavior and persisted contracts remain constraints unless a demonstrated defect requires correction.

## Spec Set (Frozen)

No new product specification. The governing input is Drew's request on 2026-10-03 and the existing [subsystem contracts](../reference/subsystems/README.md).

## Stories In Scope (Frozen)

No selected product stories. The scope is the audit and cleanup described above, delivered through bounded child PRs.

## Spec Coverage Checklist

- [x] Runtime, indexing, and storage reviewed.
- [x] Typed graph, query execution, and note reads reviewed.
- [x] Retrieval, code intelligence, and validation reviewed.
- [x] CLI, agent, and web boundaries reviewed.
- [x] Redundant tests assessed alongside their production behavior.
- [x] Accepted changes have focused evidence, independent review, CI results, and a recorded child PR.

## Plan

1. Map ownership and trace the complex execution paths. Record concrete findings and independent fix units.
2. Reproduce each accepted defect or measure its cost. Choose the smallest design that resolves the cause and preserves the governing contract.
3. Implement in isolated worktrees with GPT-6.1-Sol at xhigh. Use GPT-6-Astra for analysis, design, and independent review.
4. Run targeted local tests, including focused race checks for concurrency. Let CI run the full suite, as Drew explicitly requested. Update owning documentation when an invariant changes.
5. Open child PRs against `t3code/holistic-codebase-cleanup`. Triage Greptile and independent review feedback. Merge each child after its current head passes review and CI.
6. Keep subsequent independent work moving while checks run. Continue until usage limits or no worthwhile actionable work remains. Leave the aggregate PR open for Drew.

The demonstrated partial native move failure has a separate [bounded transaction plan](2026-10-04-note-namespace-transactions.md), including its durable recovery contract and foundation review before integration.

## Plan Approval

Drew explicitly authorized this cleanup, the model split, child PR creation, and merging green reviewed children into the parent. This plan records that requested workflow. The local current-user identity is not configured, so `plan-approved-by` is omitted.

## Original Intended Delivery

One aggregate PR containing verified improvements from focused child PRs, with a record of actual coverage, behavioral fixes, measured performance changes, and remaining gaps. Publication and merging the parent into `main` are outside this delivery.

## Actual Delivered

The following children have passed independent review and CI and are merged into the integration branch:

| Child | Result |
| --- | --- |
| [#21](https://github.com/atomicobject/rhizome/pull/21) | Database symlinks share the schema initialization lock used by their canonical database. |
| [#15](https://github.com/atomicobject/rhizome/pull/15) | Search applies eligibility before retrieval limits, shares scalar/vector filter construction, and preserves exact symbol identity through rollup. |
| [#16](https://github.com/atomicobject/rhizome/pull/16) | Indexed and residual scalar queries agree on typed comparisons, authored blank presence, and embedded source-order ties. |
| [#20](https://github.com/atomicobject/rhizome/pull/20) | Metadata hashes and projected rows use one content capture, preventing false freshness after concurrent edits. |
| [#22](https://github.com/atomicobject/rhizome/pull/22) | Staged descending sorts preserve the same null placement as committed queries, including paginated results. |
| [#19](https://github.com/atomicobject/rhizome/pull/19) | Moved-in directory trees receive watches with correct nested-root policy, recreation tracking, readiness, and resync. |
| [#23](https://github.com/atomicobject/rhizome/pull/23) | Context-killed commands retain cancellation errors; completed failures and interrupted version probes keep correct classification and cache behavior. |
| [#24](https://github.com/atomicobject/rhizome/pull/24) | Managed scripts release discarded completed operation payloads while preserving failure evidence and draining accepted writes. |
| [#26](https://github.com/atomicobject/rhizome/pull/26) | One shared session transport owns process cleanup and JSON framing; protocol failures reach the session before waiting for child stderr closure. |
| [#25](https://github.com/atomicobject/rhizome/pull/25) | Existing lint rules replace the custom skipped-test scanner; browser checks replace CSS text assertions for scrolling and reduced motion. |
| [#27](https://github.com/atomicobject/rhizome/pull/27) | Deleted panes reject late reads, and failed embedded-node retries retain their canonical target. |
| [#30](https://github.com/atomicobject/rhizome/pull/30) | Global graphs keep their refresh obligation when older requests complete after an index event, including first loads and reconnects. |
| [#17](https://github.com/atomicobject/rhizome/pull/17) | Each priority waiter owns and renews its request; closing or losing one request cannot remove or mask another waiter. |
| [#29](https://github.com/atomicobject/rhizome/pull/29) | Custom-view stamp reads run serially, stop with their view, and recover from stalled reads through a bounded deadline. |
| [#31](https://github.com/atomicobject/rhizome/pull/31) | Semantic result hydration reads ordered section IDs without loading every sibling section body. |
| [#32](https://github.com/atomicobject/rhizome/pull/32) | Vector searches require current chunk embedding identity and dimensions before selection, excluding retained stale rows. |
| [#28](https://github.com/atomicobject/rhizome/pull/28) | Configuration and runtime publication share cooperative Windows file primitives and remove failed temporary writes. |
| [#33](https://github.com/atomicobject/rhizome/pull/33) | Staged scalar filters and sorts use the indexed query semantics; sort keys are prepared once per row. |
| [#34](https://github.com/atomicobject/rhizome/pull/34) | Alias and target changes refresh dependent graph edges and validation findings; an ordinary freshness check repairs older stale projections. |
| [#35](https://github.com/atomicobject/rhizome/pull/35) | Staged selected fields receive the same shape, enum, and required-field validation as committed query results. |
| [#36](https://github.com/atomicobject/rhizome/pull/36) | Concurrent query facets share one successful vector per text, preserve cancellation ownership, and report the causal provider failure. |
| [#37](https://github.com/atomicobject/rhizome/pull/37) | Changed chunk content retires current embedding metadata before replacement, preserving retries after provider failure and final repeated-ID semantics. |
| [#38](https://github.com/atomicobject/rhizome/pull/38) | Recovery distinguishes prepared rollback from committed cleanup, preserving valid later edits and deletions after a successful commit. |
| [#39](https://github.com/atomicobject/rhizome/pull/39) | Claude and Codex share short-lived command execution and its process tests, while each driver keeps vendor policy. |
| [#41](https://github.com/atomicobject/rhizome/pull/41) | Embedding publication resolves chunk identities once per existing batch inside the writer transaction. |
| [#18](https://github.com/atomicobject/rhizome/pull/18) | Code-reference renames preserve literal destination text and structured spans, rescan to canonical paths, and retain health reporting for literal fragment paths. |
| [#40](https://github.com/atomicobject/rhizome/pull/40) | Recipe validation finds supported skills/template roots, reports inaccessible entries, and loads recipe files only when the selected checks require them. |
| [#43](https://github.com/atomicobject/rhizome/pull/43) | Heading changes and inbound rewrites publish through one witnessed edit session; direct retries recover interrupted writes and the CLI reports actual outcomes. |
| [#44](https://github.com/atomicobject/rhizome/pull/44) | Expansion canonicalizes partial node identities and counts shared-frontier rows against total edge limits while preserving requested result groups. |
| [#42](https://github.com/atomicobject/rhizome/pull/42) | Native moves preflight the whole endpoint set, preserve overwrite targets until replacement, and retain case and authored-alias references. |
| [#50](https://github.com/atomicobject/rhizome/pull/50) | Repair publication retires rename sources before installing destinations; reverse recovery handles both path orders and valid older journals. |
| [#45](https://github.com/atomicobject/rhizome/pull/45) | Detailed semantic results publish session history only for complete retained bodies, including distinct results from the same code file. |
| [#51](https://github.com/atomicobject/rhizome/pull/51) | Watcher eligibility tests exercise installation directly, removing an unrelated scheduling deadline while retaining asynchronous lifecycle coverage. |
| [#49](https://github.com/atomicobject/rhizome/pull/49) | Incomplete directory discovery returns an error before stale-row retirement can remove still-existing notes. |
| [#47](https://github.com/atomicobject/rhizome/pull/47) | File and vault context publish session history after final packing, trimming, and warning assembly; omitted, partial, and compressed raw content remains eligible for retry. |
| [#48](https://github.com/atomicobject/rhizome/pull/48) | Revalidated stale lock owners publish complete replacement metadata without a removal gap; native Windows readers stay valid through takeover. |
| [#52](https://github.com/atomicobject/rhizome/pull/52) | Live code indexing shares batch publication, invalidates symbol caches across publication failures and concurrent fills, and avoids repeated tail-index updates. |
| [#53](https://github.com/atomicobject/rhizome/pull/53) | Native move preparation uses an isolated Git index, preserves staged changes and supported index flags, and rejects unsupported repository states before publication. |
| [#54](https://github.com/atomicobject/rhizome/pull/54) | Residual and staged built-in path filters and sorts preserve exact path spelling before pagination, matching indexed queries. |
| [#59](https://github.com/atomicobject/rhizome/pull/59) | Application runtimes close owned embedding caches on setup failure and shutdown, preserving borrowed-provider ownership. |
| [#56](https://github.com/atomicobject/rhizome/pull/56) | Identifier commands allocate and render guides from the runtime-refreshed schema and store while retaining early schema admission errors. |
| [#46](https://github.com/atomicobject/rhizome/pull/46) | Heading rename updates fragment-only self-links across supported path spellings and refuses file symlinks before publication. |
| [#57](https://github.com/atomicobject/rhizome/pull/57) | Shared identifier pools include authored embedded owners and all declared alias fields, without scanning unrelated root properties for embedded-only pools. |
| [#60](https://github.com/atomicobject/rhizome/pull/60) | Batch evaluation tests separate cancellation outcomes from timeout propagation and actual deadline expiry. |
| [#58](https://github.com/atomicobject/rhizome/pull/58) | Queue cancellation tests synchronize on the active operation and pending control, preserving real cancellation behavior. |
| [#62](https://github.com/atomicobject/rhizome/pull/62) | Exact refresh admits selected note sources while retaining attachment, ignored-path and deletion coverage for retirement and cleanup. |
| [#61](https://github.com/atomicobject/rhizome/pull/61) | Identifier repair accounts for every declared alias field and scopes alias identity to the owning type. |
| [#64](https://github.com/atomicobject/rhizome/pull/64) | Provider lifetime tests isolate database counts, retain focused subtest selection and preserve child-process coverage. |
| [#65](https://github.com/atomicobject/rhizome/pull/65) | Node-locator edits use the live writer, invalidate derived reads and preserve cancellation as a retryable error. |
| [#70](https://github.com/atomicobject/rhizome/pull/70) | Derived identifier repairs carry exact owner changes across preview phases and retire every child-authorized alias. |
| [#68](https://github.com/atomicobject/rhizome/pull/68) | Serve test pollers use cooperative readers for atomically replaced runtime files. |
| [#55](https://github.com/atomicobject/rhizome/pull/55) | Index-lock metadata reuses the shared cooperative file reader, removing duplicate platform code. |
| [#69](https://github.com/atomicobject/rhizome/pull/69) | Required watcher failures retain admitted inputs and reach the lane result; a quiet retry converges projections through the same runtime. |
| [#67](https://github.com/atomicobject/rhizome/pull/67) | Oversized code-reference inputs are rejected before allocating file contents, retaining ordinary-file and growth-race guards. |
| [#63](https://github.com/atomicobject/rhizome/pull/63) | Native namespace publication reuses the repair transaction owner for durable decisions, witnessed rollback, Git staging, writer admission and current-footprint recovery. |
| [#66](https://github.com/atomicobject/rhizome/pull/66) | Rename and batch move commands plan required backlinks before publication and preserve committed, restored, unresolved and recovered outcomes through CLI and code-mode results. |
| [#73](https://github.com/atomicobject/rhizome/pull/73) | Structured links are sealed once after final ordering, removing two discarded parser seals while retaining source and snapshot authority. |
| [#72](https://github.com/atomicobject/rhizome/pull/72) | Mention regexes skip absent literal names while preserving invalid UTF-8, Unicode, alias, basename and sequential mapping behavior. |
| [#71](https://github.com/atomicobject/rhizome/pull/71) | Section ancestry is classified from complete SDL, so typed relation admission no longer depends on alphabetical compilation order. |

The metadata change reduces dirty rebuild content reads from 2N to N and keeps no-op checks free of projection and file stats. Its direct-reader capture retains additional source bytes: a synthetic 1,000-note workload with 16 KiB notes retained about 15.7 MiB more live heap during the check. This is a measured correctness tradeoff; elapsed-time improvement is not established.

The managed execution workload of 400 discarded 256 KiB results retained 105,229,280 bytes before the fix and 537,160 bytes afterward at a checkpoint inside the still-running script. This measures retained JavaScript heap, not peak process memory or throughput. A synthetic protocol-error workload now returns the error in about 3 ms instead of waiting for its roughly 352 ms timeout; bounded process cleanup remains a separate step.

The section-hydration workload of 25 hits across 25 notes with 64 one-KiB sections each reduced median time from 1.786 ms to 0.547 ms, allocated bytes from 2.619 MB to 0.245 MB, and allocations from 23,713 to 5,536. This measures result hydration alone. Current-vector eligibility adds a correctness cost: paired 10,000-vector measurements showed roughly 4% to 13% more time across the normal query scopes, with essentially unchanged allocations.

The staged-sort workload of 5,000 candidates and one touched note reduced allocations by 31% with one sort key and 36% with three keys. Paired median times improved from 161 ms to 139 ms and from 190 ms to 154 ms; timing was noisy on the shared machine. Prepared keys preserve source-order ties and remove reads from the comparison function.

Changed-content retirement has a measured correctness cost on the 512-anchor replacement workload: median time rose from 6.662 ms to 8.100 ms, allocated bytes from 1.122 MB to 1.297 MB, and allocations from 22,269 to 23,342. The separate embedding-publication optimization, measured with that correction already present, reduced a 512-vector, 128-dimension workload from 6.402 ms to 5.640 ms, bytes from 1,092,161 to 817,885, and allocations from 14,988 to 7,100. Identity reads fell from 512 to 11. These measurements concern different operations and should not be combined into a net indexing claim.

The final code-reference rewrite preserves structured spans and literal paths. Three alternating paired runs of the unchanged 1 MiB comment-heavy, 100-mapping public workload compared the structured implementation before the last two optimizations with their combined result. Matched median time fell from 5.865 s to 0.375 s (15.6 times faster), allocated bytes from 263.0 MB to 178.0 MB (32.3% lower), and allocations from 1,462,194 to 1,070,499. Unmatched median time fell from 5.048 s to 0.212 s (23.9 times faster), bytes from 104.9 MB to 69.7 MB (33.5% lower), and allocations from 516,306 to 352,307. Every measured call checked its public update counts.

That paired baseline already includes the structured-span correctness repair. The historical original regex implementation measured about 13.43 s and 118.7 MB on the same workload; this is a separate, nonpaired measurement. Final allocated bytes remain roughly 1.5 times that original result. Small actual-mention controls showed a noisy 2.9% time increase for the literal guard alone with unchanged allocation counts. These measurements do not establish peak memory or general rename throughput.

Recipe applicability avoids an eager-discovery regression on unrelated checks. A selected broken-links workload with 1,000 16-KiB recipe files measured 34.328 ms and 41.824 MB with eager loading, versus 2.579 ms and 0.482 MB with zero recipe reads after the correction. This restores the earlier selection cost; it is not a claimed improvement over the original parent.

Live code publication reduced tail-index updates from two to one per changed file. Three alternating runs of 1,000 TypeScript updates reduced median allocated bytes per update from 121,359 to 115,810 and allocations from 2,578 to 2,522. Median update time changed from 1.350 ms to 1.400 ms; no latency improvement is claimed. Unchanged-file admission still performs zero tail updates.

## Execution Notes

- 2026-10-04T02:36:10Z. Started from `1559ac204e5d5baaa81110becaf3ee5fae8d4bcc` with a clean worktree. Launched independent audits of runtime/storage, typed graph/query, and retrieval/validation.
- Built the local CLI with `NO_WEB=1 make build`. A Rhizome agent session started successfully. Indexed context was initially unavailable, so the audits use source and checked-in subsystem guidance directly.
- Initial runtime, graph, and retrieval audits produced eight bounded correctness units. Independent review found additional edge cases before integration, including lock contention, source-order ties, and identity loss during later search transformations.
- A second application audit reproduced protocol-error shutdown stalls, canceled version probes cached as failures, and managed scripts retaining already-discarded results. The 400-call synthetic execution workload retained about 105 MB versus 0.48 MB through the generated client directly. These repairs have passed independent review and CI.
- The web audit reproduced two pane request-lifecycle defects, graph invalidation losing a refresh obligation, and overlapping custom-view stamp reads that survive cleanup. Pane, test-hygiene, and global graph refresh repairs are merged. The stamp lifecycle repair is also merged after independent review of its bounded read deadline.
- Storage follow-up reproduced a stale vector row being reused by a replacement chunk and a changed embedding remaining searchable in its previous dimension table. The measured design keeps current metadata identity and dimensions in eligibility before retrieval limits. Vector identity and section projection are merged. The source-freshness repair now retires evidence transactionally when content changes and preserves the last effective hash for repeated IDs. The independently measured embedding-lookup optimization is also merged.
- Further public-API audits reproduced alias changes leaving durable graph edges and validation findings stale while readiness remained true; recipe checks skipping supported skills/template locations; and concurrent same-text query facets producing different vectors and invalid continuations without an index change. Query coherence and incremental graph convergence are merged, including materialization recovery for previously stale graph projections. Recipe discovery is merged, including selection-gated loading and actual inaccessible-root controls.
- Staged query filtering, sorting, and selected-field validation now share the committed query semantics. Independent public query matrices cover optional invalid values, required-field errors, enums, embedded nodes, aliases, source-order ties, and committed/staged parity.
- Recovery audit reproduced successful commits leaving cleanup journals that reject valid later note edits or deletions. The phase-aware repair is merged after actual warning/crash recovery and subsequent-edit probes. Shared command execution and embedding batches are also merged after independent review, green full CI, and final-head Greptile 5/5.
- Further mutation probes found self/alias overwrite data loss, overlapping batch moves, and namespace or heading changes published before later backlink failures. Heading transactions are merged. Endpoint admission is merged after case, alias, ancestry, and native Windows spelling corrections. Full namespace transactions have an accepted bounded plan and a reviewed Git preparation contract. Session probes also found detailed search and nested context marking budget-omitted content as delivered. Both response-publication repairs are merged after review of compression, concurrent callers, distinct bodies from one code file, and exact retained output.
- The legacy indexing audit reproduced unreadable directory walks retiring existing note rows while returning success. The merged repair preserves incomplete discovery as an error before stale cleanup. Windows CI exposed a separate stale-lock handoff reader failure. Native held-reader controls on the same runner image passed with unchanged production code, disproving the initial deterministic delete-pending explanation. Phase-specific native diagnosis then reproduced transient Access denied errors during removal. The atomic-replacement repair is merged after independent source/public review, native Windows CI, and final-head Greptile 5/5.
- Repair rollback ordering is merged after public failure/crash probes, reverse-rollback interruptions, and captured legacy-journal recovery. Independent review corrected a macOS-specific fixture path before native Linux/Windows CI passed.
- The context-publication child hit a Windows timeout in the watcher eligibility-policy fixture. A controlled slow backend reproduces this test deadline independently of missing events; the test-only scheduling correction is merged, with actual worker and native watcher coverage retained. The context-publication child then passed the final native checks and is merged.
- Live code indexing now shares publication through the existing batch owner. Review reproduced a cache invalidation regression and a pre-existing late-fill race; their corrections pass independent public SQLite interleavings. Real SQLite trigger failures cover every generic persistence family, retries, and unchanged-file admission. The child is merged after removal of repeated tail-index work and final CI/review.
- The isolated Git preparation helper is merged after actual Git parity controls, native Windows CI, and independent review. The native engine and public CLI integration subsequently completed through children #63 and #66. Windows inventory checks now use fresh filesystem metadata, with a positive control demonstrating cached directory timestamps.
- Residual built-in path filtering and sorting is merged after a public indexed/residual/staged parity matrix. Identifier allocation review separately reproduced startup schema changes and embedded sibling owners being omitted from pools. Both repairs are merged after public CLI and real-store regression tests, independent review, full CI, and final-head Greptile 5/5.
- Repeated public search setup failures with the optional embedding cache retained SQLite owners after garbage collection. The application ownership repair is merged after the same failing-search workload retained zero descriptors instead of eight, with a borrowed-provider control confirming caller ownership. Normal MCP queries reuse providers; this is not a claim of one leaked database per ordinary MCP query.
- Heading self-links are merged after case-variant and file-symlink review findings were reproduced and fixed. A follow-up identifier validation audit found that declaring both alias fields can hide a collision and leave it present after an apparently successful repair. The source-aware correction is merged after per-type authority controls; derived child rekeys are merged after independent public plan/apply/repeat controls and final CI/review.
- Native Windows CI exposed a separate batch-evaluation test assuming its first HTTP response finishes within 100 ms. Controlled cancellation and independent deadline checks replace that scheduler assumption; this test-only child and the queue-control scheduling correction are merged after independent review, full CI, and final-head Greptile 5/5.
- Real post-apply refresh probes reproduced attachment renames committing files and then failing projection intake. The merged exact-path selection repair preserves admitted formats, system context documents and complete retirement coverage. Node-reading locator authority and cancellation repairs are merged after same-scope graph refresh and retry controls, full CI and final review.
- Windows CI exposed interference in a process-wide database-owner count. Fresh-process workloads preserve exact leak assertions, focused subtest selection and coverage; that test correction is merged. The serve poller correction and shared reader consolidation are also merged after native CI and final review.
- Further namespace review reproduced moved-in ignored sources rewriting unrelated bare links, large valid requests exceeding the persisted summary limit, and optional code rewrites losing dependent move order after lease release. Corrections preserve full current diagnostics, bounded recovery summaries and the existing lease owner. Actual Unicode case and normalization aliases also require candidate lookup that still proves physical identity before retiring old keys.
- Required watcher destination failures now reach the lane result, with retained inputs and quiet retry verified against real SQLite. The watcher repair and oversized code-reference early admission are merged after independent review and native CI. The 16 MiB skipped-file workload fell from about 16.8 MB to 11 KB allocated per call; the normal-file control retained its behavior.
- A follow-up output-ownership audit reproduced deliberate mutation of returned nested values affecting a reused scope, but found no detached-output contract or harmful supported caller. The investigated concurrent GraphQL preview workload passed the race detector. This remains a bounded API-ownership question, not a claimed application defect.
- The native transaction engine and CLI are merged after source review, public failure/crash/retry probes, actual Git parity controls, Unicode filesystem capability controls, native Windows checks and Greptile feedback. The existing publication owner now supplies recoverable required publication and truthful product outcomes; optional code-reference writes retain request ordering under its borrowed lease.
- The schema follow-up reproduced order-dependent acceptance of unsupported Section-derived link targets. The full-AST correction and legal public fixture repairs preserve the existing note-oriented target contract. Typed links to embedded children remain unsupported; universal Note links remain accepted; identifier reference discovery preserves their fragments and resolves the exact embedded owner.
- Profiling the final code-reference workload identified repeated absent-name regex scans and discarded provisional seals. Two independent changes remove those costs without changing grammar or public scan authority; paired and combined measurements are recorded above.
- An independent delivery audit checked every child inventory, reviewed head, merge ancestry, squash correspondence, check rollup and review thread. All 59 children have passing CI and independent review. Greptile feedback is resolved. PR #21 retains a 4/5 summary after the reviewer withdrew its timezone-based date finding; PR #20 has 5/5 on its implementation followed by an independently reviewed comment-only correction. The other children have final-head 5/5 summaries.
- Final source revision `4dc574281c5d7b346b6a9e01f799bdbd5113843b` builds with `make build`, including the web assets. Focused combined namespace/code-reference checks pass with race detection through `go test -race -tags fts5 ./pkg/app/cli ./cmd ./pkg/vault/coderefs ./pkg/vault/obsidian -run '^(TestNamespaceOptionalCodeWritesFollowCommitOrder|TestNamespaceOptionalCodeWriteFailureKeepsRequiredCommit|TestNoteNamespaceCommandsPublishAndReportRequiredLinks|TestCodeModeNoteMoveRetainsPublishedAndRejectedOutcomes|TestRewriteBatchMentionEncodingAndMappingOrder|TestScanStructured|TestScanComment|TestValidateStructured)' -count=1`; the relevant code and test files are unchanged from the measured integration revision. The built runtime reaches ready state and serves its home page and every referenced local asset successfully. T3 browser automation failed at its client connection during the local smoke check; browser behavior is covered by the passing CI Web E2E runs. Default validation and `validate frozen-scope-drift` both pass with zero issues and zero errors.
- Coverage statements remain bounded by recorded experiments and inspected paths; these audits do not certify entire subsystems.

## Deviations

The user's targeted-test instruction overrides the default local full-suite gates. CI remains the child merge gate.

## Closure Checklist

- [x] Every merged child has an independently reviewed head and green checks.
- [x] Aggregate diff inspected for interactions and unnecessary complexity.
- [x] Coverage and unresolved findings recorded accurately.
- [x] Parent PR linked and current.

## Compounding Follow-ups

The nested-output ownership question remains a possible API-design follow-up: deliberate caller mutation can affect a reused read scope, but no detached-output contract or harmful supported caller was established, and the concurrent GraphQL preview probe passed race detection. No implementation change is justified by that probe alone.

Linked-worktree support for native Git effects, replay of optional effects, and request idempotency keys remain outside the bounded namespace repair. The audit does not claim support for typed Section link targets or general rename throughput beyond the measured workloads.

## Status

Complete for the authorized cleanup delivery. All 59 reviewed children are integrated in [PR #14](https://github.com/atomicobject/rhizome/pull/14), which remains open for Drew. Integration source revision: `4dc574281c5d7b346b6a9e01f799bdbd5113843b`. No release or merge to `main` was performed.
