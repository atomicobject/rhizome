---
name: notemeta-subsystem
description: Use when implementing, modifying, or reviewing code under pkg/notemeta or its store implementations. Loads notemeta design constraints and review checklist.
---

# Notemeta subsystem

## Goal

Change or review the raw-note metadata index (`pkg/notemeta`) and its SQLite store (`pkg/anchors/sqlite/note_metadata.go`) without breaking freshness hashing, snapshot/delta symmetry, or the store contract.

## First move

Read `docs/reference/subsystems/notemeta.md` first; its constraints are normative for any change in this subsystem. Then pull `pkg/notemeta/CONTEXT.md` and the exact files you will touch.

## Rules

1. `pkg/notemeta` owns raw note facts and freshness only — typed meaning belongs to ontology, anchor extraction to anchors, derived evidence to search. Do not blur the boundary.
2. Bump `noteMetadataIndexerVersion` (`pkg/notemeta/index.go`) whenever row derivation changes; it is the only forced-rebuild mechanism for already-indexed vaults.
3. Keep `SyncPaths` (delta) output identical to a fresh `EnsureIndexed` (snapshot) for the same files — rows, edges, and the incremental XOR notes hash. Test both paths.
4. Normalize every path through `pkg/paths` (`NormalizeNote`, `VaultPaths.RelNoteStrict`); never ad hoc `filepath.Abs/Rel`, never legacy `obsidian.NormalizePath`/`AddMdSuffix` in new code.
5. Any `Store` interface change must keep `pkg/notemeta/store_contract_test.go` compiling against `*semdb.Store` and add behavioral coverage in `pkg/anchors/sqlite/note_metadata_test.go`.
6. All store writes go through `withWriteTx` — one snapshot or one delta per transaction, chunked under the 900-param SQLite limit. No per-row writes from the indexer.
7. New graph edge kinds need `semdb.GraphDocEdgeConfidenceDefaults` entries and inclusion in the snapshot-replace `graph_doc_edges` cleanup predicate.
8. `NoteSourceSnapshot` is the only raw-note DTO for downstream ingestion; extend it rather than adding per-domain note structs.

## Pre-handoff checklist

- [ ] `go test ./pkg/notemeta/...`
- [ ] `go test ./pkg/anchors/sqlite/...`
- [ ] `go test -race -tags=integration ./...` when indexing behavior changed
- [ ] `noteMetadataIndexerVersion` bumped if row derivation changed
- [ ] Store contract test compiles; new store behavior has a `note_metadata_test.go` case
- [ ] `docs/reference/subsystems/notemeta.md` and `pkg/notemeta/CONTEXT.md` updated if boundaries or invariants moved
