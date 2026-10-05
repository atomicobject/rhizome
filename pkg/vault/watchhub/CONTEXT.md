## Vault watch hub

Normalizes recursive filesystem events and follower hints into debounced, vault-relative change batches for cache and indexing consumers.

- **Shutdown**: `Close` cancels and drains admitted hub work and subscriber callbacks, discards pending debounce events, and closes the backend once. Callbacks may unsubscribe themselves, but must not call `Close` synchronously. `Start` is single-use; starting a closed hub has no effect.
- **Backend drain**: the fsnotify event pump remains alive until backend close because Windows watch installation shares the native event reader. Cancellation stops installation between filesystem operations; shutdown still waits for an in-flight OS call to return.
- **Readiness**: directory-installation work is counted before enqueueing and remains counted through queue handoff and installation. A full queue rolls back its admission count and signals overflow.
- **New directory trees**: nonrecursive backends evaluate each directory with the most specific matching root's options. Registered nested roots are revisited independently, including roots below hidden ancestors pruned by an outer root. Hard ignores still apply; ordinary user excludes preserve traversal for exact `CONTEXT.md`. Removal or rename clears watch and walk tracking for the old subtree so registered roots can be installed again when it reappears. A resync notice follows installation so consumers discover files written before their watches became active; each installed tree gets its own notice.
- **Entry points**: `NewHub`, `Subscribe`, `EmitHintPaths`, `EmitHintResync`.
- **Key invariant**: watcher visibility matches batch discovery. Ordinary paths use the configured ignore matcher; exact `CONTEXT.md` uses the matcher without ordinary note-selection excludes. Root containment, hidden and built-in infrastructure directories, and Git/Rhizome ignore rules remain authoritative.

### Deep docs

- [[Indexing pipeline (Hub)]]
- [[Ignore + exclude rules]]
