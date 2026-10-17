## pkg/app/codeintel

Code-intel indexing primitives and service/indexer composition shared by command, batch, and live callers. Runtime bootstrap consumes this module without owning its constructor.

- **Entry points**: `NewCodeAnchorService`, `ResolveRoots`, `ResolveRoot`, `IngestMarkdownCandidates`, `IndexCandidates`, `ValidateNotes`, `ReadAuthoredMarkdownAnchors`
- **Key invariant**: read-only authored-anchor fallback validates the source path and resolved target within the vault, selects configured note ownership through the explicit format runtime, and parses only admitted Markdown. Shared runtime providers consume its unresolved targets and diagnostics without selecting a provider or parsing source.
- **Key invariant**: all indexed paths are vault-root-relative when persisted (`pkg/paths` strict rel conversion)
- **Key invariant**: explicit code ingest reads selected bytes and uses content hash, indexer version and trusted parse status to skip unchanged parsing and persistence; file timestamps cannot prove freshness. Note ingest retains its source/ownership and mtime/content-hash guards.
- **Key invariant**: full indexing supplies preclassified explicit candidates. `IngestMarkdownCandidates` and `IndexCandidates` validate the complete batch, do no walk/extension classification/stale cleanup, and route only their supplied lane; ownership orchestration owns all of those decisions.
- **Key invariant**: explicit Markdown and code candidate read/build failures are terminal to the full run. They leave reconciliation pending and never use legacy cleanup to retire prior ownership. Retired code-only command walkers and direct-store fallbacks are removed.
- **Key invariant**: note ingest receives an explicit configured `notemeta.Indexer` and shared writer, then projects `notemeta.NoteSourceSnapshot` with a vault-relative path; anchors accepts it through `BuildNoteIndexWorkFromSource` / `IngestNoteSource` only
- **Key invariant**: collection-style note ingest must honor `VaultDefinition.Includes` before ignore/exclude pruning, or out-of-scope notes leak into intel/semantic state
- **Key invariant**: early code-semantic submission must stay batched/off the file-worker path; synchronous per-file submission can stall code indexing behind semantic prep backpressure
- **Key invariant**: a code persistence batch commits freshness, refs/external evidence, Intel and rationale atomically; late failure or cancellation preserves prior rows so retry cannot skip incomplete files
- **Key invariant**: code ingest should not own the drain for async semantic submitters; long-lived submitters batch in the background and close later in unified orchestration so `index_code` measures file ingest, not semantic tail wait
- **Key invariant**: `index_code phase_wall` encloses candidate queue residence and end-of-phase writer drains; use `file_enqueue_wait_cum`, `file_queue_wait_cum`, `write_drain_cum`, `write_flush_cum`, and `semantic_drain_cum` before blaming parse/read CPU
- **Key invariant**: `worker_submit` starts after `BuildCodeIndexWork`; `worker_submit_local` is now dominated by reducer handoff (`result_submit`, `result_reduce`, `result_finalize`), while semantic/write submission stay in their own buckets
- **Key invariant**: unified full-index mode should let code semantic work consume provider capacity first; note embeddings can queue during note ingest, but flush after code streaming completes
- **Key invariant**: external-reference evidence travels inside `CodeIndexWork` through the existing bounded writer queue; workers emit natural keys only and never look up persistence IDs

### Deep docs

- [[Code Index (Hub)]]
- [[Indexing pipeline (Hub)]]

`ProcessCandidates` owns bounded parallel reads of the already selected code and note candidates. Queue backpressure, cancellation, read errors, and discovery/completion callbacks stay in this ingestion module; ownership discovery stays in noteownership and indexcore.
