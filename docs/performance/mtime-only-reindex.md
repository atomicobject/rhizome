---
summary: "Content-identity note hashing: an mtime-only re-index now writes nothing but note freshness columns, with before/after row and graph-revision counts."
last-verified: 2026-09-05
---

# Mtime-only re-index cost

`git checkout`, `git stash`, editor save-without-change, sync clients and CI checkouts change note mtimes without changing bytes. Before this change every such touch rewrote all derived note rows, truncated and rebuilt the ontology read model, advanced the published metadata generation, and bumped the graph revision hundreds of times, invalidating every cached graph response and every published-generation reader.

The cause was that `noteStateDigest` folded filesystem mtime into the per-note digest, so `RawNotesHash` — and through it `NotesHash` — changed on a touch. `noteStateDigest` now hashes path plus content hash only. Mtime and size remain persisted on the note row as freshness evidence; they are compared per row, and a content-identical row is refreshed in place through the new `NoteMetadataDelta.SourceTouches`.

This does not weaken B09 (`engine-code-freshness.md`): every file is still read and re-hashed exactly as before. `DiscoverDirtyPaths` still flags an mtime difference dirty, and both delta builders still project every selected note. The only new skip is a hash match *after* reading, which is the gate B09 endorses. No mtime-equality shortcut was added anywhere.

## Before / after

Disposable copy of `testdata/integration/python-app/vault` (24 notes, local `test` embedding provider), counting triggers on every graph-input and metadata table, one `RZM_SKIP_REPO_DELEGATE=1 rzm index` per scenario. Before = the merge of `codex/engine-follow-up` with the Intel v63 trigger narrowing (PR #224); after = this change. Single sample per scenario: these are exact row and bump counts, not timings.

| Scenario | Bumps before | Bumps after | Row writes after |
| --- | ---: | ---: | --- |
| No change | 0 | 0 | none |
| Touch every note (`find … -exec touch {} +`) | **327** | **0** | `notes` update 23, `note_metadata_state` replaced with identical values |
| Edit one note body | 335 | 335 | unchanged (see below) |
| Add a note linking an existing one | 274 | 274 | unchanged |
| Delete a linked target | 322 | 322 | unchanged |
| Add an alias to a linked target | 335 | 335 | unchanged |

Before, the touch-only run wrote `graph_doc_edges` 60 del / 60 ins, `note_property_values` 78/78, `note_tags` 19/19, `note_markdown_targets` 25/25, `note_projection_state` update 23, `ontology_nodes` 46/46, `ontology_edges` 36/36, `ontology_note_types` 10/10, `ontology_note_assessments` 23/23, `ontology_note_state` 23/23, `ontology_node_field_values` 41/41, and replaced both `note_metadata_state` and `ontology_schema_state` with a new generation. After, none of those tables is written and both generations are byte-identical.

For the four content-changing scenarios the per-table row counts are identical between the two binaries, table for table. This change deliberately claims no improvement there.

## Parity

`TestRunUnifiedCore_IncrementalScenariosMatchFreshRebuild` runs all five scenarios through `RunUnifiedCore` and compares the resulting note paths, content hashes, graph edges, aliases, property values, Markdown targets, ontology types, ontology edges and node count against a fresh index of the same tree in a second store; every scenario matches. Alias rename and delete rederive inbound edges from unchanged sources, because both still take the bootstrap route. `TestBuildPathDeltaTouchOnlyDirtyPathsProduceSourceTouches` pins the incremental route, including a mixed delta where one note is rewritten and another is only touched.

## Benchmark

`BenchmarkGraphRevisionTriggerWriteOverhead`, 50 transactions per sample, three samples, same host: with-trigger median 30.53 ms before and 30.64 ms after; without-trigger 3.34 ms and 3.31 ms. No regression; the difference is host noise.

## Dependency

The touch-only result requires Intel v63 (`engine-graph-revision.md`), which narrows the `notes` update trigger to `AFTER UPDATE OF path, title`. Without it the in-place mtime write — and the pre-existing codeintel `TouchNoteMtimes` write — would still bump the revision.

`noteMetadataIndexerVersion` moves from `"6"` to `"7"`, so every existing vault re-indexes fully once on upgrade and then behaves as above. That is the sanctioned migration path; no dual-hash compatibility exists.

## Recorded follow-up

The not-current published rewrite is still all-or-nothing: editing one note body rewrites all 24 notes' metadata rows and forces 15 of 23 ontology neighbour assessments. Narrowing it needs the path-set/alias guard from `projectionLinkTopologyChanged` ported to the published path, plus its own parity evidence. That is the next candidate in this area and was deliberately left out of this change.
