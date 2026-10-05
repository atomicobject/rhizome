---
type: ReferenceDoc
summary: "Authority note for LiveRuntime: async capability-based bootstrap for long-running serve and web runtimes."
reference-kind: analysis
derived-from:
  - docs/reference-notes/LiveRuntime (async server bootstrap).md
last-verified: 2026-09-22
status: active
---

# LiveRuntime (async server bootstrap)

## Summary

`LiveRuntime` encapsulates shared async initialization for long-running Rhizome server processes. It returns quickly while heavier capabilities warm in background phases so serve/web runtimes can become available without waiting for every cache, store, and scheduler to finish booting.

## Capability model

- search readiness: cache, watchhub, asynchronous note-cache warmup
- semantic readiness: note-embedding store and provider
- code readiness: intel store, code-anchor service, code embeddings, syncers
- leader-only work: watchers, schedulers, and boot-time background indexing

## API semantics

- `WaitFor*` calls block until the capability is ready or failed
- `*Ready()` calls are non-blocking readiness probes
- component getters may return nil until the relevant capability is ready
- init failures are stored and surfaced through the wait/readiness path rather than by failing construction for every non-fatal issue

## Ownership rules

- exactly one `LiveRuntime` per vault root owns the vault: it wins `.rhizome/runtime.lock` synchronously before the serve command listens, then runs the watcher, the indexing lane, and the control API ([[vault-runtime]], [[vault-runtime-coordination|SPEC-0104]])
- a process that loses election exits with code 3 and never touches the winner's manifest, lock, or log; there is no follower mode and no cache-hints log
- one-shot runtimes (`rzm agent`, MCP tools) disable leader work entirely and read the shared index

## Design constraint

Any new long-running runtime capability should fit into this phased readiness model instead of reintroducing handshake-blocking bootstrap work.
