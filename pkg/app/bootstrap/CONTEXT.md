## Async bootstrap for live-runtime processes (serve, web, agent)

Shared initialization for `rzm serve`, `rzm web`, and the one-shot `rzm agent` bootstrap. Encapsulates vault resolution, runtime-owner election, and **capability-based async initialization** so long-lived processes can respond quickly and agent CLI can wait only for the capabilities it needs.

- **Entry point**: `NewLiveRuntime(ctx, opts)` — returns fast; heavy init runs in background
- **Election is synchronous** (SPEC-0104): unless `DisableLeaderWork` is set, `NewLiveRuntime` acquires `.rhizome/runtime.lock` before it returns, so `rzm serve` knows whether it owns the vault before binding a listener. The owner-only `OnElected` recovery barrier runs before any phase; if it fails, the runtime closes and releases election ownership before returning the error. Check `ElectionWon()`. A loser starts no phases at all and fails every capability with `ErrNotRuntimeOwner`, so it can never open, migrate, or write the winner's state.
- **Metadata composition**: `NewLiveRuntime` constructs one immutable built-in `notemeta.Indexer` before launching async phases; watcher and leader background indexing receive that exact indexer rather than creating provider runtime state themselves. Live ownership publication derives changed root paths from the provider metadata delta, so root-only formats refresh ontology, semantic, and lexical destinations without entering Markdown anchor ingestion.
- **Provider lifetime**: factories register optional provider closers immediately, so runtime shutdown also releases caches after partial initialization. Embedding stores and shared lanes close before their providers; compatible query-only code work reuses the note provider before construction. CLI semantic-tool setup has the same provider/store ownership.
- **Shutdown**: `Close` cancels and drains directly owned initialization, leader startup, boot catch-up, session cleanup, and note-cache warmup before closing the lane, stores, and election lock. Nested workers register from tracked parents; they must not call `Close` themselves.
- **Code-index opening contention**: Writable Phase 3 startup keeps readiness pending while retrying typed SQLite busy/locked opening errors for 30 seconds from the first attempt. Cancellable waits grow from 100 ms to at most one second. Read-only one-shot startup attempts opening once and returns contention promptly for fail-soft handling. Missing, incompatible, and other permanent failures complete immediately. Only opening is retried, before publication; each attempt retains its native SQLite timeout and may outlast the recovery window, but no further attempt starts after the window expires. Close prevents further attempts and drains any in-flight open.
- **Key invariant**: after the owner-only recovery barrier, slow initialization runs as goroutines so long-running server startup stays responsive
- **Watcher visibility**: WatchHub evaluates ordinary paths with the configured matcher and exact `CONTEXT.md` with the hard-boundary matcher, keeping live events aligned with batch/rebuild selection.
- **Watcher priority and retry**: each ownership batch runs with the indexing lane's cancellable job context and yielding index-lock heartbeat. Interactive priority cancels that batch without stopping the watcher. The lane retains the lease until structural publication and dirty-work activation drain. The watcher preserves drained events after incomplete structural reconciliation. Derived provider and graph computation run outside the lease; short publication jobs reject obsolete generations and source witnesses. Provider cancellation follows the shared embedding node's lifetime, and runtime shutdown joins that node before stores close. A waiting interactive request prevents another watcher batch from acquiring the lease.
- **Watcher outcomes**: reconciliation returns required failures to the lane's handle, terminal event, and last-error status, including cache refresh and pending-generation reads before a health epoch starts. A canceled job context takes precedence after the body drains. Structural input ownership stays with the watcher. Structural acknowledgement and node.changed follow durable structural publication; derived failures retain ready obligations for periodic and startup recovery without replaying structural work. Disabled semantic domains acknowledge their obligations, while configured unavailable providers retain debt. Event-triggered coalesced structural wake has no per-path cooldown; the periodic tick repairs quiet structural debt.
- **Watcher diagnostics**: finite trigger counts are retained, swapped, and restored with pending structural inputs. Cache refresh, scoped/full discovery causes, actual structural phases, and durable acknowledgement retain sparse events and bounded lane metrics. Derived tickets have separate `derived-work` reports for the actual domain, scope, retry attempt, computation and publication outcome; each short writer job remains a correlated child. Computation metrics describe logical requests for this ticket. Physical provider batches shared by the long-lived embedding node accumulate in a bounded collector and publish a separate `runtime.embedding-provider` summary after node shutdown drains. This summary covers shared runtime lifetime rather than any one ticket. Active or crashed runtimes may lack a completed physical summary; missing measurements do not mean zero requests. Current and completed health epochs remain available through the live health API.
- **Consumer view**: `LiveRuntime.Snapshot()` implements `pkg/app/runtimeview.View`; MCP and web read a fresh non-blocking snapshot per call instead of copying capability state. The note reader appears when the existing cache is published; snapshot lookup never initializes or refreshes it. Serve startup may publish a validated existing Intel store as a read-only handle during Phase 1 so provider-current exact-note reads do not wait for semantic initialization; Phase 3 replaces it with the full code-index handle.
- **One-shot selection**: `LiveOptions.Requirements` accepts a typed capability set. Its zero value preserves the full serve/web runtime; `pkg/app/oneshotruntime` compiles explicit one-shot plans so an empty selection cannot become full runtime accidentally. One-shot callers may omit only capabilities their response cannot consume. Omitted capabilities finish with typed `ErrCapabilityNotRequested` state rather than appearing ready or becoming false remediation. `DisableSessionStore` also suppresses session-cleanup ownership, including its immediate cleanup and periodic ticker.
- **Indexed one-shot policy**: `IndexedReadOnlyRuntimeOptions` selects only CodeIndex and opens an existing current validated Intel index with SQLite `mode=ro` and `query_only`, plus its bounded existing-only `mode=rw` session-dedupe handle. Static plan composition suppresses that handle only for `SessionNone`. `SessionOnlyExisting` opens the same narrow writer independently for note-only dedupe. When CodeIndex is also requested, session opening waits cancellably for code initialization to finish, preventing the optional writer and required reader from contending during WAL connection setup. It never exposes an Intel reader or owns cleanup. Therefore code-symbol, code-references, code-rationale, and graph-path expose no session store or other write-capable handle, while file-context and session-aware note operations retain only the declared dedupe handle. One-shot semantic-query separately selects Semantic + CodeIndex + CodeEmbeddings while omitting Search/cache, watchers, indexers, syncers, and leader work. It constructs provider-only query embedders, validates persisted provider/model/dimension metadata when present, and never opens embedding stores for schema creation or repair. Its existing-only session-dedupe handle is retained for repeated semantic retrieval; it never creates or migrates the database or runs session cleanup. Missing/incompatible state degrades through structured indexed-context remediation. The runtime does not initialize cache/search/semantic/leader work or mutate indexed evidence for static exact-index commands. Default/serve/index opens retain checked write-capable behavior, and minimal start requests no capabilities and opens no store.

### Capability-based API

Tools/endpoints express *what capability they need*, not what components. Phases close channels when ready:

Search, semantic, code, and leader state are independent. Use the matching
`WaitFor*` only when waiting benefits the caller; never infer later phases from
semantic readiness.

| Capability | Channel | What's Ready | Accessor |
|------------|---------|--------------|----------|
| Search | `searchReady` | Cache + Watchhub | `WaitForSearch()` |
| Semantic | `semanticReady` | Note embeddings index + provider, or provider-only query state for indexed one-shot retrieval | `WaitForSemantic()` |
| CodeIndex | `codeReady` | Code anchor service + intel store | `WaitForCodeIndex()` |
| Leader | `leaderCh` | This process won election (closed before `NewLiveRuntime` returns) | `WaitForLeader()` |

**Initialization phases** (each continues to next even on failure):

1. **Phase 1 (Search)**: ownership selection → validated existing Intel read handle → cache → watchhub → note-cache warmup (election already decided)
2. **Phase 2 (Semantic)**: Note embeddings store + provider
3. **Phase 3 (Code)**: Intel store → code anchor service → code embeddings → syncers
4. **Phase 4 (Leader)**: Watchers, schedulers, background indexing (only if leader; uses shared `pkg/app/indexing` core)

### Semantic Runtime

- Live syncers use `pkg/app/semanticruntime` for watcher packer policy, provider ceilings, shared gates, and long-lived compatible embedding lanes.
- Watcher ontology primary-chunk follow-up reuses the note runtime lane; do not create burst-only or intent-only embedding nodes in bootstrap/scheduler code.
- Ontology rebuild follow-up selects provider-current, ontology-eligible paths through `ontology.ProjectableMetadataPaths`, matching full indexing. Raw metadata inventory includes descriptor-only sources and must never be sent directly to Markdown ontology projection.
- Intent exemplar sync is indexing-owned semantic work and must be routed through the shared runtime when run from CLI or live indexing paths.

### One runtime per vault

Exactly one process owns a vault root. It wins `.rhizome/runtime.lock`, runs fsnotify, the schedulers, and background indexing, and serves the control API. A process that loses exits with `appruntime.ExitCodeAlreadyRunning`; there is no follower role, no hints log, and no re-election. Watcher freshness events are published straight to the local SSE broker.

### Deep docs

- [[LiveRuntime (async server bootstrap)]]
- [[Indexing pipeline - Live updating (watcher runtime)]]
- [[Cache (Hub)]]

### Persistent diagnostics

`NewLiveRuntime` preserves the command recorder and opaque operation identity in its lifetime context. Search, semantic, code, and leader initialization report independent readiness, duration, and safe capability metadata without awaiting another phase. Omitted capabilities are skipped outcomes. A missing index during a read-only code capability request is an INFO `skipped/index_missing` outcome; its existing structured remediation remains unchanged. Runtime logs use named stages and finite error categories instead of persisting paths or provider response text; warnings retain explicit severity. Indexing-lane correlation and bounded metrics remain lane-owned.

Runtime shutdown drains request handlers, background work, stores, and the election heartbeat before `BeforeOwnershipRelease` finalizes runtime and command diagnostics and closes the headless output capture. All structured recorder writes and elected Go-output capture finish before election ownership is released. The inherited native fallback can still receive process-exit or pre-capture command error prose afterward; it follows the raw-output retention limits above. Attached terminal completion output remains after release. Owner-barrier failures and panics finalize their true error outcome while still releasing the election lock.
