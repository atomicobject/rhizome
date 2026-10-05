---
type: ProductSpec
summary: "Defines batch/live indexing plus the lightweight validation projection, including freshness, progress, rebuild, and lock coordination."
id: SPEC-0036
spec-status: active
last-updated: 2026-09-17
aliases:
  - SPEC-0036
  - indexing-workflow
---

# Indexing workflow

## Summary

Rhizome indexing should feel predictable: users run `rzm index` to build or refresh the searchable vault/code state, servers keep that state warm in the background, and agents can trust progress, timings, and retrieval freshness without learning hidden pipeline details.

The product surface includes configuration-driven full indexing, separate code maintenance commands, rebuilds, progress bars, `--timings`, status/maintenance output, and lock/priority behavior when another indexing process is active. Validation, repair, and CI use a smaller note-only projection that shares the same correctness primitives without claiming full search or code freshness.

## Goals

- make `rzm index` the clear batch-build and repair command
- keep live watcher refresh compatible with the same freshness model
- expose truthful progress and timing labels for code, notes, primary ontology chunks, graph refresh, and maintenance
- make rebuild and lock behavior understandable when users run commands concurrently
- preserve strong retrieval results for mostly typed-note repositories

## Non-Goals

- turning `rzm index` into a general database administration tool
- exposing every internal queue/batch knob as a product option
- requiring users to understand ontology projection before they can refresh search
- preserving legacy raw-note embedding behavior when ontology-ready indexing gives better freshness and cost control

## User Stories

### US1 - Run one command that leaves code intel, note metadata, ontology-derived semantic evidence, graph signals, and index maintenance coherent
- id:: ^SPEC-0036-US1
- summary:: Run one command that leaves code intel, note metadata, ontology-derived semantic evidence, graph signals, and index maintenance coherent.
- status:: ready

#### Acceptance Criteria

- A full run completes the observable sequence: code discovery/indexing, note ingest, metadata sync, ontology graph refresh, primary ontology-node embedding, semantic write drain, graph recompute, validation cache refresh, and cheap SQLite maintenance. ^SPEC-0036-US1-AC1
  verification:: Run `rzm index --timings` on a fixture vault and confirm the stage/timing labels map to the listed sequence without broad unlabeled work.
- Progress labels distinguish file ingestion, primary ontology-node sync, provider embedding work, semantic write flushes, graph refresh, and maintenance.
  verification:: Inspect progress output for a run that changes notes and code; each domain appears under its own label.
- `--timings` reports phases that correspond to real work rather than wrapping broad planning, queue waits, writeback, or pruning under misleading provider labels. ^SPEC-0036-US1-AC3
  verification:: Confirm primary-chunk planning/rendering, provider embedding, writeback, pruning, graph, and maintenance phases are independently visible when applicable.

### US2 - Refresh changed content and explicitly rebuild all indexes when required
- id:: ^SPEC-0036-US2
- summary:: Refresh changed content and explicitly rebuild all indexes when required.
- status:: ready

#### Acceptance Criteria

- An explicit `rzm index --rebuild` clobbers the unified SQLite database plus WAL/SHM sidecars, then recreates every indexing domain from scratch. Mode-scoped rebuilds are retired.
  verification:: Rebuild a healthy, corrupt, and future-schema fixture; verify full replacement and reconstruction under the index lock.
- Ontology-ready vaults use source-owned `node_body` chunks as their note semantic surface and prune stale raw authored-section or generated-card chunks for typed notes.
  verification:: Index a typed-note fixture and confirm `ontology_node` body chunks exist without duplicate typed-note `doc_section` or card semantic chunks.
- Schema, primary-chunk format, and scope changes expand work explicitly enough to refresh affected ontology-node evidence instead of silently leaving stale rows.
  verification:: Change ontology schema, primary-chunk format version, or indexing scope and confirm the run logs or timings show the expanded sync.

### US3 - Understand when another indexer is active and how interactive work waits, requests priority, or yields

- id:: ^SPEC-0036-US3
- summary:: Understand when another indexer is active and how interactive work waits, requests priority, or yields.
- status:: ready

Revision 2026-09-17: when a vault runtime is live, `rzm index` executes inside it and never contends for the lock; [[vault-runtime-coordination|SPEC-0104]] governs that path, the lane's yield latency, lock roles, and runtime identity. This story keeps governing in-process indexing and one-shot writers.

#### Acceptance Criteria

- Concurrent indexers coordinate through a cross-process lock before shared SQLite writes begin.
  verification:: Hold `.rhizome/index.lock` from one process and confirm a second batch indexer waits or exits instead of writing concurrently.
- Interactive indexing can request priority from best-effort background work without forcibly deleting a live lock or assuming the current holder is dead.
  verification:: Run an interactive lock acquisition with `requestPriority=true` and confirm it creates/removes the priority request file while preserving the holder lock.
- Stale lock recovery is automatic only when the holder process is dead, the metadata is old enough to be considered abandoned, or the heartbeat has exceeded the active stale threshold.
  verification:: Exercise indexlock stale-lock tests for live PID, dead PID, recent corrupt file, and aged corrupt/heartbeat cases.

### US4 - Find the governing indexing story, acceptance criterion, and rationale from the orchestration or policy code before changing behavior
- id:: ^SPEC-0036-US4
- summary:: Find the governing indexing story, acceptance criterion, and rationale from the orchestration or policy code before changing behavior.
- status:: ready

#### Acceptance Criteria

- Key orchestration and policy entry points link to the smallest relevant story, acceptance criterion, or technical requirement that explains their ordering, locking, chunk-family, or timing behavior.
  verification:: Run `rzm agent file-context` on indexing entry points and confirm the retrieved docs include the specific `SPEC-0036`/`SPEC-0012` nodes, not only broad hubs.
- Rationale comments preserve the non-obvious reason for bounded discovery backpressure, one queued writer lane, shared semantic lanes, primary-chunk invalidation, and lock wait/priority behavior.
  verification:: Run `rzm agent code-rationale --path pkg/app/indexing` plus adjacent indexing packages and confirm each listed policy has an indexed `WHY:`/`IMPORTANT:` comment.
- Documentation-only changes to indexing docs or code bindings validate broken links, ontology shape, code frontmatter, and code anchors before handoff.
  verification:: Run `rzm agent validate broken-links --max-issues 40`, `rzm agent validate ontology --max-issues 40`, `rzm agent validate code-frontmatter --max-issues 40`, and `rzm agent validate code-anchors --max-issues 40`.

### US5 - Remove generated-card indexing and converge one source-owned primary semantic stream
- id:: ^SPEC-0036-US5
- summary:: Remove generated-card indexing and converge one source-owned primary semantic stream.
- status:: satisfied
effort:: EFF-2026-07-11-16-30

#### Acceptance Criteria

- Indexing contains no generated-card manifest, render, embedding, writeback, pruning, validation, report, or second-pass stage. ^SPEC-0036-US5-AC1
- Upgrade migration removes generated-card tables, chunks, vectors, and sidecar state while preserving ordinary code, document, and ontology-node primary chunks. ^SPEC-0036-US5-AC2
- Fresh and upgraded databases converge to the same primary chunk set; unchanged primary chunks do not enter provider queues. ^SPEC-0036-US5-AC3
- Progress, timing, help, validation, report, and agent capability surfaces advertise only real primary-chunk work. ^SPEC-0036-US5-AC4

### US6 - Refresh only the projection required for validation and repair
- id:: ^SPEC-0036-US6
- summary:: Refresh note metadata, ontology, links, headings, and block targets for validation without rebuilding project, code, graph, or embedding domains.
- status:: satisfied

#### Acceptance Criteria

- Validation, repair planning/apply, and CI share one coordinator that publishes note metadata, outgoing links, and Markdown headings/block IDs as one source delta before deriving ontology projections. ^SPEC-0036-US6-AC1
- Local commands reuse the shared vault database; CI can bootstrap an isolated scratch database from configured notes. ^SPEC-0036-US6-AC2
- An unchanged vault reuses the current validation projection without running project discovery or full indexing. ^SPEC-0036-US6-AC3
- After repair, only exact changed, renamed, or deleted paths refresh before targeted checks rerun. ^SPEC-0036-US6-AC4
- Code, code-anchor, chunk, graph-score, and embedding freshness remain independent and are never advanced by validation projection refresh. ^SPEC-0036-US6-AC5
- Heading and block-target indexing extends one canonical Obsidian fragment parser/normalizer shared with legacy link validation, covers wiki/Markdown fragments, and excludes fenced and inline-code lookalikes. ^SPEC-0036-US6-AC6
- Refresh acquires the index lock or reuses an explicit caller-held lease, routes the complete source delta and derived ontology writes through the queued writer lane, waits on an explicit flush barrier before checks, and preserves truthful per-domain freshness. ^SPEC-0036-US6-AC7
- Refresh performs zero project/code enumeration and zero embedding-provider calls and leaves code, code-anchor, chunk, embedding, and graph table content unchanged. ^SPEC-0036-US6-AC8
- A full scratch/bootstrap projection and equivalent incremental/exact-path refresh converge to identical metadata, outgoing-link, heading, block-target, and ontology rows for the same source state. ^SPEC-0036-US6-AC9

## Requirements

- `rzm index` MUST remain the authoritative batch workflow for refreshing persisted code, note, semantic, graph, and maintenance state.
- Full indexing MUST preserve a visible order: discover/classify, ingest code and notes, sync primary ontology semantics, rebuild correctness-sensitive code graph state, refresh graph scores, then run cheap maintenance.
- Plain `rzm index` MUST select work from configured note/code scope and content freshness, and honor disabled embedding providers without enabling them. The `--semantic` and `--code` flags and their force overrides are retired.
- Required note metadata/doc-section state MUST be published before dependent embedding work runs.
- Ontology-ready indexing MUST treat source-owned ontology body chunks as the primary note semantic surface.
- Raw authored-section note embeddings MUST be compatibility-only for ontology-unavailable indexing paths.
- Provider-backed work MUST use the configured embedding provider/model and shared compatible semantic runtime lanes.
- Progress and timing output MUST label primary-chunk planning, provider work, writeback, pruning, intent sync, graph refresh, and maintenance separately.
- `rzm index --rebuild` MUST clobber the unified SQLite database and WAL/SHM sidecars before recreating every domain; selecting the flag is explicit operator authorization for replacement.
- Indexing without `--rebuild` MUST NOT clobber the database; replacement-class failures MUST tell the operator to rerun with `--rebuild`.
- Automatic maintenance MUST run cheap SQLite upkeep after heavy writes and reserve full `VACUUM` for explicit requests or freelist-gated reclaim.
- Cross-process indexing MUST coordinate through `.rhizome/index.lock` and priority request files rather than relying on SQLite busy timeouts as the primary guard.
- Live watcher refresh SHOULD share the same freshness and evidence model as batch indexing, with different coalescing and latency policies.
- Validation projection refresh MUST remain a scoped indexing mode, not a second independent pipeline or an implicit full `rzm index`.
- Validation projection freshness MUST be recorded per domain so metadata/ontology/Markdown targets can be fresh while code, embeddings, graph scores, and code anchors remain stale or unchanged.
- Renamed and deleted notes MUST tombstone dependent validation rows without claiming unrelated domains are fresh.
- Code documentation and code anchors SHOULD bind indexing orchestration/policy code to the governing story or acceptance criterion when the behavior is non-obvious or correctness-sensitive.

## Documentation Plan

- Update indexing and notemeta subsystem guidance with validation-projection ownership, per-domain freshness, lock/writer ordering, and scratch/live modes.
- Add rationale comments and coderefs for the no-full-index boundary and post-repair exact-path barrier.
- Keep CLI/CI help explicit that validation projection refresh does not make code, embeddings, code anchors, or graph scores current.

## Open Questions

- whether users need a compact `rzm index explain` view that maps stale search symptoms to rebuild domains
- which timing counters should graduate from diagnostic detail into stable product output

These questions do not change or block the validation-projection story.
