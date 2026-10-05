---
summary: "Browser/runtime engine audit with measured HTTP baselines, graph fingerprint defect, and pane subscription reproduction."
last-verified: 2026-09-05
---

# Engine browser/runtime research

Research baseline and selected implementation evidence. Coordinator owns SPEC-0089 and EFF-2026-09-05-14-10. B03a pane lifecycle and B03b graph revision were subsequently approved as separate PRs. Measurements below describe the original base unless labeled otherwise.

Base: `af100a9059fe1e34a88a59d70af26cf75e5e1368`. Branch: `codex/engine-browser-runtime`. Worktree: `<worktree>`.

## Ranked opportunities

| Rank | Opportunity | Evidence | Cost / proposed boundary |
| --- | --- | --- | --- |
| 1 | Replace graph cache fingerprint scans with reliable change detection | Reproduced stale-cache collision across two database connections; lookup grows to 30.81 ms at 100k edges | Moderate; web + SQLite graph freshness contract, coordinate persisted-state ownership |
| 2 | Stabilize pane SSE identity and cancel obsolete pane reads | Reproduced one same-ref refresh creating two extra SSE connections; requests have no abort options | Low/moderate; browser pane lifecycle, preserve generation guards |
| 3 | Bound type/summary read assembly | 1k generated notes: warm type 43.49 ms, summary 36.74 ms | Read-lane collaboration; do not independently edit noderead/query internals |
| 4 | Coalesce vault invalidation bursts | One note update emits index.changed plus validation.invalidated; browser invalidates all queries for each | Low/moderate; source-confirmed amplification mechanism, actual network count unmeasured |
| 5 | Move graph layout off the main thread or reuse scoped layouts | Synchronous ForceAtlas2 50–120 iterations | Moderate; hypothesis about user impact, needs real graph long-task measurements |

Recommended first batch: graph fingerprint correctness/performance plus the independently testable pane connection/cancellation fix. Keep type/summary internals in the read lane. A fingerprint replacement must observe writes by other handles/processes and must not rely solely on in-process invalidation or elapsed-time TTL. A database revision or suitable SQLite change signal requires explicit connection-pool, same-connection, and cross-process semantics before implementation.

## Measurements

Environment: macOS arm64, Go 1.24.2, `GOMAXPROCS=2`, vendored dependencies, `fts5` tag. Host is shared with other workers, so tails are indicative. Timed regions exclude fixture creation/indexing. No embedding provider or network call is required. HTTP measurements use the real server mux and response serialization through `httptest.NewRecorder`; they exclude sockets, browser rendering, and browser network latency. They are handler baselines, not browser end-to-end claims.

Warm cached graph lookup, fixed one-node response, 30 samples. Synthetic persisted wikilink edges isolate freshness-check cost; these are not whole-graph build timings.

| Persisted edges | Median | p95 |
| --- | --- | --- |
| 0 | 0.085 ms | 0.092 ms |
| 1,000 | 0.376 ms | 0.389 ms |
| 10,000 | 3.083 ms | 3.271 ms |
| 100,000 | 30.808 ms | 32.590 ms |

An earlier shared-host run measured 34.09 ms median / 66.13 ms p95 at 100k edges. Both runs show the same linear trend; do not interpret the tail change as an optimization.

Actual indexed ontology fixture plus generated schema-matched Spec notes, 20 warm requests per endpoint:

| Added notes | Endpoint | First request | Warm median | Warm p95 |
| --- | --- | --- | --- | --- |
| 100 | global graph | 4.30 ms | 0.258 ms | 0.489 ms |
| 100 | type Spec | 5.21 ms | 4.539 ms | 5.414 ms |
| 100 | ontology summary | 3.80 ms | 3.953 ms | 4.184 ms |
| 1,000 | global graph | 25.00 ms | 1.199 ms | 1.791 ms |
| 1,000 | type Spec | 43.90 ms | 43.491 ms | 45.853 ms |
| 1,000 | ontology summary | 36.57 ms | 36.737 ms | 38.383 ms |

Generated notes contain a summary and Requirements heading. This exercises real note admission, ontology indexing, HTTP read assembly, and serialization. It is a simple schema workload, not a representative distribution of complex embedded nodes or edit overlays. Type/summary requests lack a comparable response cache; their scaling merits profiling in the read lane before prescribing a fix.

Small existing ontology fixture, 30 warm samples: status 0.008 ms, tree 0.098 ms, summary 0.325 ms, type Spec 0.224 ms, global graph 0.116 ms, GraphQL introspection 0.013 ms, minimal node workspace selection 0.304 ms. The workspace query selects only `loaded`, so it must not be presented as full workspace latency.

## Concrete defects and source evidence

### Graph freshness is expensive and can miss changed edges

`pkg/app/web/graph.go:40` calls `GraphWebFingerprint` before every cache lookup, outside the cache's concurrent-build deduplication. `pkg/anchors/sqlite/graph_inputs.go:107` computes count/max/sum aggregates across note, file, graph, score, anchor, and ontology tables. An already cached one-node response therefore still pays table-scan cost.

The edge signature sums independently weighted source and destination characteristics. Reproduction: connection A reads the fingerprint for `source-a → target-a` and `source-b → target-b`; connection B swaps the destinations in one SQL update; A reads exactly the same fingerprint. The sum loses edge pairing, so even a stronger hash of each endpoint independently would preserve this collision. Existing same-count rewire tests change edges without testing a destination permutation.

Required regression: warm actual cached graph, rewire via another handle/process without changing edge count, then verify the next response's edges change. Also test deletion/reinsertion, non-edge graph attributes, same-connection writes, unchanged-store reuse, and reopened connections. Preserve external-target isolation. Benchmark cache-hit scaling and concurrent readers.

### Pane refresh reconnects its own SSE subscription

`web/src/components/usePaneStack.ts:514–563` derives a new refs array from pane state and depends on that array in its subscription effect. Pending/completed loads update pane state even when canonical refs remain unchanged.

A deferred-promise Vitest reproduction using unchanged production source measured:

| Stage | Total EventSource connections |
| --- | --- |
| Initial pane resolved | 1 |
| Same-ref SSE refresh pending | 2 |
| Refresh completed | 3 |

Two connections closed. Both workspace API calls received no request options. Source at `usePaneStack.ts:98–105`, `263`, `366` confirms missing signals. Generation checks protect displayed identity but do not cancel obsolete server work. Server `pkg/app/web/node_events.go:355` canonicalizes and projects refs on every subscription, so reconnecting causes read work too.

Required regressions: same canonical ref retains one subscription across refresh; changed canonical ref replaces it once; closing/superseding/unmounting aborts the corresponding read; delayed responses never overwrite the latest pane. Preserve edit-session restore/preview and route identity behavior.

### Other bounded audit findings

- `pkg/app/web/graph_cache.go:61–65`: in-flight followers wait only on the builder channel, without their own cancellation. The first request's build closure owns context, so a canceled leader can also make healthy followers receive its error. Source-confirmed; cancellation timing not measured.
- `pkg/app/web/node_projection_cache.go:78–99`: concurrent cold misses can load/project the same note independently. Warm goroutines are per note and use a background context from `node_workspace.go:655`; no server lifetime cancellation. Impact unmeasured; avoid adding another cache before profiling canonical noderead ownership.
- `pkg/app/web/ontology.go:200,250`: summary asks TypeInstances for full results and then takes five starting refs or only counts. Type list at line 390 also uses this shared seam. Reported to coordinator/read lane.
- `pkg/app/bootstrap/schedulers.go:904–929` publishes each event separately; normal note changes emit index.changed and validation.invalidated. `web/src/query/VaultInvalidationBridge.tsx:37–46` invalidates the entire root per event. Existing tests spy on invalidation calls, not resulting network requests. Measure deferred QueryObserver request counts before changing event policy.
- `web/src/components/useSigmaGraph.ts:351–366` computes synchronous layout; local/module graphs lack the global position-cache reuse. Main-thread stall duration remains a hypothesis.
- Runtime gate in `pkg/app/web/gate.go` is one-shot and explicitly context-aware, with targeted timeout/cancel tests. Do not replace it based on cache findings. Agentapi JSON normalization is intentional CLI/MCP parity; no measured reason to remove it. Search orchestration already has deadline-aware fallback and concurrent retrievers; no independent change proposed in this lane.
- Positive browser coverage: Explorer forwards cancellation signals, shared tree keys deduplicate folder/module reads, delayed route-identity tests exist, and local graph tests cover stable click handlers. The apparently unused AsyncOntologyLocalGraph wrapper is not a priority.

## Reproduction and validation

Local binary built successfully with `NO_WEB=1 make build`. Minimal Rhizome agent session started successfully (`idCXptEoViSxsZ70`). No full `make check` during research.

Disposable Go harness retained at `/tmp/engine-browser-runtime-research_test.go`; raw output at `/tmp/engine-browser-baseline.txt` and `/tmp/engine-browser-scaled.txt`. Copy the harness into `pkg/app/web/engine_research_test.go` in this worktree, run below, then use `trash` on the copied file. The test intentionally asserts the observed fingerprint collision; it is a research probe, not a desired-behavior regression.

```sh
GOCACHE="$PWD/.gocache" GOMODCACHE="$PWD/.gomodcache" GOTMPDIR="$PWD/.gotmp" GOMAXPROCS=2 go test -mod=vendor -tags fts5 ./pkg/app/web -run '^TestEngineResearch' -count=1 -v
```

Go HTTP/scaled/fingerprint experiments passed after correcting two exploratory GraphQL query shapes; the initial query errors are harness errors, not product defects. Initial use of default Go cache failed under sandbox permissions; worktree-local cache succeeded.

Browser harness: `/tmp/rhizome-pane-sse-audit/src/components/churn.test.tsx`; copied unmodified pane implementation, deferred mocked API, fake EventSource, existing checkout dependencies. One test passed with:

```sh
cd /tmp/rhizome-pane-sse-audit
node node_modules/vitest/vitest.mjs run --config vitest.config.ts --configLoader runner --silent=false --reporter=verbose
```

Research limitations: no browser trace, real socket latency, embedding-provider timing, long-running bootstrap contention profile, or edit-overlay workload measured. No claimed production speedup. Temporary harnesses are local evidence and should become maintained behavior-focused tests/benchmarks only in the selected implementation batch.

## Approved graph batch design

Use a migrated singleton graph-domain revision plus database incarnation token. Database triggers increment the revision in the same transaction as graph-relevant INSERT/UPDATE/DELETE operations, including score/type/label inputs. Filter mixed-purpose tables by graph eligibility; UPDATE checks OLD and NEW membership. External evidence and symbol-ref tables remain outside this domain, preserving the explicit unchanged-fingerprint isolation test. Audit every graph read dependency before finalizing the trigger inventory.

A fingerprint becomes one singleton SELECT via QueryRowContext, with no reserved connection or extra store opener. Database triggers observe writes from the same process, raw SQL, other handles, and other processes; rollback also rolls back the revision. Reopening a replaced database changes its incarnation token and store identity. Live file replacement beneath open database handles remains unsupported by store policy; this batch does not silently reopen or repair files.

Use the intel domain migration runner; upgrade/replay/idempotence/read-only tests are required. Benchmark trigger overhead on large write batches as well as lookup cost. Graph cache follower cancellation is deferred from B03b. Existing followers still wait for the builder; cancellation behavior was identified during research and is not changed or claimed as implemented by this batch.

Documentation validation: `./scripts/rzm validate` completed with 0 issues and 0 errors.

## B03a pane lifecycle implementation evidence

Selected change: stable canonical-ref subscription identity and abortable pane requests. A shared synchronous stack transition preserves the latest stack across same-turn operations and React priority renders, cancels removed pane requests, and publishes plain state values; it does not mutate the authority ref during render. Request generations continue rejecting late responses. The live subscription reads the latest edit session through the current loader.

The original same-ref refresh created three total EventSource connections (initial + pending + completion). The corrected hook and real-browser regression retain one connection through both pending and completed refresh. Superseded, closed, cleared, and unmounted reads receive aborted signals.

Verification before the full repository gate:

- Focused `usePaneStack.test.tsx`: 14 tests pass, including deferred refresh, latest edit-session options, late responses, direct clears, StrictMode rapid transitions, and transition/priority rebasing.
- TypeScript and Biome checks pass.
- `npx -y react-doctor@latest . --verbose --diff` from `web/`: final score 100/100, no issues. Earlier reviews caught updater side effects and render-time ref mutation; both corrected, without suppression.
- `GOMAXPROCS=2 make web-e2e`: final run 10/10 pass. The pane-specific test opens a real fixture workspace, delivers a node invalidation to its live EventSource, holds the resulting real GraphQL request, and verifies one subscription during and after loading. Existing navigation, delayed/error/empty identity, and edit-cell flows also pass.
- Independent read-only review: no remaining actionable findings. It initially found direct-clear cancellation and priority-update loss; the final diff includes regressions for both.
- `./scripts/rzm validate`: 0 issues, 0 errors.

The first browser attempt could not launch because Chromium was missing. Installing Playwright Chromium resolved the environment prerequisite; no test was weakened. Full repository gate and PR review are recorded when complete.


## Final candidate disposition

B03a implements pane subscription stability and obsolete-request cancellation, including stale-session/SSE navigation regressions. B03b implements transactional graph revision freshness in a separate child PR; its durable benchmark and indexing evidence lives in `docs/performance/engine-graph-revision.md` after integration. The graph follower-cancellation finding remains deferred.

Type/summary assembly was handed to the read lane; this browser/runtime batch makes no claim about its final outcome. Vault invalidation-burst coalescing, synchronous graph layout, and cold projection deduplication remain unimplemented research candidates. Their user-visible amplification or main-thread impact was not measured. Runtime gate and agentapi normalization changes were rejected for this batch because source review found established contracts and no measured problem. Browser evidence covers connection/request lifecycle and actual refresh behavior; it does not establish production latency or large-graph rendering improvements.

PR review added three deferred navigation cases: cross-file stale session delivery, old-ref SSE during navigation, and nodeId-only embedded-node to same-file root navigation. Requested identity now controls eligibility for session/SSE refresh; obsolete resolved identity cannot cancel the destination load. Independent final source review found no remaining issue.
