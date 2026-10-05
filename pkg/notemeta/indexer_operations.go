package notemeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// EnsureIndexed projects every selected authored source through the explicit
// runtime and atomically publishes its raw facts and provenance.
func (i Indexer) EnsureIndexed(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store Store) (*EnsureResult, error) {
	if store == nil || noteMgr == nil {
		return &EnsureResult{}, nil
	}
	contents, capturedMtimes, err := captureNoteContents(ctx, vaultDef, noteMgr)
	if err != nil {
		return nil, err
	}
	rawNotesHash, notePaths := capturedNotesHash(contents)
	stateHash, err := metadataStateHashWithSelectedProviders(vaultDef, rawNotesHash, i.formats, notePaths)
	if err != nil {
		return nil, err
	}
	state, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return nil, err
	}
	if state.Ready && state.NotesHash == stateHash {
		materialized, err := noteMetadataPathsMaterialized(ctx, store, notePaths)
		if err != nil {
			return nil, err
		}
		// A fatal projection is a durable current-source result, even though
		// provider-aware consumers cannot use its derived facts. Reprocessing it
		// on every freshness check would erase the value of retaining that raw
		// identity; source or provider-version changes will invalidate state.
		stale, err := i.projectionRowsStale(ctx, store, notePaths)
		if err != nil {
			return nil, err
		}
		if materialized && !stale {
			return &EnsureResult{NotesHash: stateHash}, nil
		}
	}
	entries, err := i.projectCapturedContents(ctx, vaultDef, noteMgr, contents, capturedMtimes)
	if err != nil {
		return nil, err
	}
	snapshot, err := i.buildSnapshot(vaultDef, entries)
	if err != nil {
		return nil, err
	}
	if err := store.ReplaceNoteMetadataSnapshot(ctx, snapshot); err != nil {
		return nil, err
	}
	return &EnsureResult{NotesHash: snapshot.State.NotesHash, Dirty: true}, nil
}

// SyncPaths derives and applies one provider-aware metadata delta.
func (i Indexer) SyncPaths(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store Store, changedPaths, deletedPaths []string) error {
	if store == nil || noteMgr == nil {
		return nil
	}
	delta, err := i.BuildPathDelta(ctx, vaultDef, noteMgr, store, changedPaths, deletedPaths)
	if err != nil || delta == nil {
		return err
	}
	return store.ApplyNoteMetadataDelta(ctx, *delta)
}

// DiscoverDirtyPaths returns raw-source changes and expands to all paths when
// the selected provider/projection fingerprint changes without a byte change.
func (i Indexer) DiscoverDirtyPaths(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store Store) (*DirtyPaths, error) {
	formats, err := i.runtime()
	if err != nil {
		return nil, err
	}
	sources, err := i.loadAuthoredSources(ctx, vaultDef, noteMgr)
	if err != nil {
		return nil, err
	}
	return discoverDirtyPaths(ctx, vaultDef, store, formats, sources)
}

// MetadataStateCurrent verifies source freshness, selected provider versions,
// materialized paths, and per-row current projection provenance.
func (i Indexer) MetadataStateCurrent(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store Store) (bool, error) {
	formats, err := i.runtime()
	if err != nil {
		return false, err
	}
	ready, notePaths, err := metadataStateCurrentWithSelectedProviders(ctx, vaultDef, noteMgr, store, formats)
	if err != nil || !ready {
		return ready, err
	}
	return i.projectionRowsCurrent(ctx, store, notePaths)
}

// BuildPathDelta derives a complete provider-aware delta without writing it.
func (i Indexer) BuildPathDelta(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store Store, changedPaths, deletedPaths []string) (*MetadataDelta, error) {
	if store == nil {
		return nil, fmt.Errorf("note metadata store is required")
	}
	if noteMgr == nil {
		return nil, fmt.Errorf("note reader is required")
	}
	changedPaths = dedupeStrings(changedPaths)
	deletedPaths = dedupeStrings(deletedPaths)
	if len(changedPaths) == 0 && len(deletedPaths) == 0 {
		return nil, nil
	}
	state, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return nil, err
	}
	persistedPaths, err := store.CurrentNoteMetadataPaths(ctx)
	if err != nil {
		return nil, err
	}
	persistedStateHash, err := metadataStateHashWithSelectedProviders(vaultDef, state.RawNotesHash, i.formats, persistedPaths)
	if err != nil {
		return nil, err
	}
	rowsCurrent, err := i.projectionRowsCurrent(ctx, store, persistedPaths)
	if err != nil {
		return nil, err
	}
	if !state.Ready || strings.TrimSpace(state.RawNotesHash) == "" || state.NotesHash != persistedStateHash || !rowsCurrent {
		return i.buildBootstrapDelta(ctx, vaultDef, noteMgr, store)
	}
	entries, err := i.loadProjectedEntriesForPaths(ctx, vaultDef, noteMgr, changedPaths)
	if err != nil {
		return nil, err
	}
	durable, err := store.CurrentNoteMetadataRowsByPaths(ctx, projectedPaths(entries))
	if err != nil {
		return nil, err
	}
	topologyChanged, storedAliases, err := projectionLinkTopologyChanged(ctx, store, vaultDef, durable, entries, deletedPaths)
	if err != nil {
		return nil, err
	}
	if topologyChanged {
		return i.buildBootstrapDelta(ctx, vaultDef, noteMgr, store)
	}
	// Content-identical entries keep their derived rows and the published
	// generation; only their persisted freshness evidence is refreshed.
	changed, touches := partitionProjectedEntries(durable, entries)
	if len(changed) == 0 && len(deletedPaths) == 0 {
		if len(touches) == 0 {
			return nil, nil
		}
		return &MetadataDelta{State: state, SourceTouches: touches}, nil
	}
	cache, err := buildIncrementalProjectionLinkCache(ctx, store, vaultDef, changed, deletedPaths, storedAliases)
	if err != nil {
		return nil, err
	}
	if cache != nil {
		aliases := overlayProjectionAliasesForDelta(storedAliases, changed, deletedPaths)
		cache = buildProjectionAliasCache(cache, aliases)
	}
	rawNotesHash, err := incrementalProjectedNotesHash(ctx, store, state.RawNotesHash, changed, deletedPaths)
	if err != nil {
		return nil, err
	}
	delta, err := i.buildDelta(vaultDef, cache, changed, deletedPaths, nextLoadedAt(state.LoadedAt), rawNotesHash, selectedPathsAfterDelta(persistedPaths, changed, deletedPaths))
	if err != nil {
		return nil, err
	}
	delta.SourceTouches = touches
	return delta, nil
}

func (i Indexer) buildBootstrapDelta(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store Store) (*MetadataDelta, error) {
	entries, err := i.loadProjectedEntries(ctx, vaultDef, noteMgr)
	if err != nil {
		return nil, err
	}
	currentPaths, err := store.AllNoteMetadataPaths(ctx)
	if err != nil {
		return nil, err
	}
	paths := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		paths[entry.Source.Path().String()] = struct{}{}
	}
	deleted := make([]string, 0, len(currentPaths))
	for _, path := range currentPaths {
		if _, ok := paths[path]; !ok {
			deleted = append(deleted, path)
		}
	}
	allPaths := projectedPaths(entries)
	cache := obsidian.BuildNotePathCacheWithAliases(allPaths, projectionAliases(entries))
	return i.buildDelta(vaultDef, cache, entries, deleted, time.Now().UnixNano(), notesHashFromProjectedEntries(entries), allPaths)
}

func (i Indexer) buildSnapshot(vaultDef obsidian.VaultDefinition, entries []projectedNoteEntry) (semdb.NoteMetadataSnapshot, error) {
	loadedAt := time.Now().UnixNano()
	rawNotesHash := notesHashFromProjectedEntries(entries)
	stateHash, err := metadataStateHashWithSelectedProviders(vaultDef, rawNotesHash, i.formats, projectedPaths(entries))
	if err != nil {
		return semdb.NoteMetadataSnapshot{}, err
	}
	snapshot := semdb.NoteMetadataSnapshot{State: semdb.NoteMetadataState{NotesHash: stateHash, RawNotesHash: rawNotesHash, LoadedAt: loadedAt, Ready: true}}
	cache := obsidian.BuildNotePathCacheWithAliases(projectedPaths(entries), projectionAliases(entries))
	for _, entry := range entries {
		snapshot.Notes = append(snapshot.Notes, projectionNoteRow(entry, loadedAt))
		snapshot.ProjectionDiagnostics = append(snapshot.ProjectionDiagnostics, projectionDiagnosticRows(entry)...)
		if entry.Projection.Status == noteformat.ProjectionStatusCurrent {
			snapshot.PropertyValues = append(snapshot.PropertyValues, projectionPropertyRows(entry)...)
			snapshot.Tags = append(snapshot.Tags, projectionTagRows(entry)...)
			snapshot.FragmentTargets = append(snapshot.FragmentTargets, projectionTargetRows(entry)...)
			snapshot.SearchRegions = append(snapshot.SearchRegions, projectionSearchRegionRows(entry)...)
			snapshot.WikilinkEdges = append(snapshot.WikilinkEdges, projectionLinkRows(vaultDef, cache, entry)...)
		}
	}
	sortGraphEdges(snapshot.WikilinkEdges)
	return snapshot, nil
}

func (i Indexer) buildDelta(vaultDef obsidian.VaultDefinition, cache *obsidian.NotePathCache, entries []projectedNoteEntry, deletedPaths []string, loadedAt int64, rawNotesHash string, selectedPaths []string) (*MetadataDelta, error) {
	allPaths := dedupeStrings(append([]string(nil), selectedPaths...))
	stateHash, err := metadataStateHashWithSelectedProviders(vaultDef, rawNotesHash, i.formats, allPaths)
	if err != nil {
		return nil, err
	}
	delta := &semdb.NoteMetadataDelta{State: semdb.NoteMetadataState{NotesHash: stateHash, RawNotesHash: rawNotesHash, LoadedAt: loadedAt, Ready: true}, DeletedPaths: deletedPaths}
	for _, entry := range entries {
		delta.Notes = append(delta.Notes, projectionNoteRow(entry, loadedAt))
		delta.ProjectionDiagnostics = append(delta.ProjectionDiagnostics, projectionDiagnosticRows(entry)...)
		if entry.Projection.Status == noteformat.ProjectionStatusCurrent {
			delta.PropertyValues = append(delta.PropertyValues, projectionPropertyRows(entry)...)
			delta.Tags = append(delta.Tags, projectionTagRows(entry)...)
			delta.FragmentTargets = append(delta.FragmentTargets, projectionTargetRows(entry)...)
			delta.SearchRegions = append(delta.SearchRegions, projectionSearchRegionRows(entry)...)
			delta.WikilinkEdges = append(delta.WikilinkEdges, projectionLinkRows(vaultDef, cache, entry)...)
		}
	}
	sortGraphEdges(delta.WikilinkEdges)
	return delta, nil
}

func (i Indexer) projectionRowsCurrent(ctx context.Context, store Store, paths []string) (bool, error) {
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, paths)
	if err != nil {
		return false, err
	}
	if len(rows) != len(dedupeStrings(paths)) {
		return false, nil
	}
	for _, path := range paths {
		row, ok := rows[path]
		if !ok || !i.projectionCurrentForRow(row) {
			return false, nil
		}
	}
	return true, nil
}

// projectionRowsStale reports whether a materialized source row needs a
// re-projection. Fatal projections deliberately do not retry here: a fatal
// result is the durable outcome for unchanged source bytes. A stale
// descriptor-only source is likewise durable: its provider deliberately has
// no executable projection. Unknown or path-mismatched stale rows remain
// retryable so they cannot be treated as provider-current.
func (i Indexer) projectionRowsStale(ctx context.Context, store Store, notePaths []string) (bool, error) {
	formats, err := i.runtime()
	if err != nil {
		return false, err
	}
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, notePaths)
	if err != nil {
		return false, err
	}
	for _, path := range dedupeStrings(notePaths) {
		row, ok := rows[path]
		if !ok || row.Projection.Status != semdb.NoteProjectionStatusStale {
			continue
		}
		if !descriptorOnlyStaleProjection(formats, row) {
			return true, nil
		}
	}
	return false, nil
}

func projectedPaths(entries []projectedNoteEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Source.Path().String())
	}
	sort.Strings(paths)
	return dedupeStrings(paths)
}

func selectedPathsAfterDelta(persistedPaths []string, entries []projectedNoteEntry, deletedPaths []string) []string {
	paths := make(map[string]struct{}, len(persistedPaths)+len(entries))
	for _, path := range persistedPaths {
		paths[path] = struct{}{}
	}
	for _, path := range deletedPaths {
		delete(paths, path)
	}
	for _, entry := range entries {
		paths[entry.Source.Path().String()] = struct{}{}
	}
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func entriesFromProjected(entries []projectedNoteEntry) []noteEntry {
	out := make([]noteEntry, len(entries))
	for index, entry := range entries {
		out[index] = entry.Entry
	}
	return out
}

func notesHashFromProjectedEntries(entries []projectedNoteEntry) string {
	var aggregate [sha256.Size]byte
	for _, entry := range entries {
		xorDigest(&aggregate, noteStateDigest(entry.Source.Path().String(), entry.Source.ContentHash()))
	}
	return hex.EncodeToString(aggregate[:])
}

func sortGraphEdges(rows []semdb.GraphDocEdgeRow) {
	sort.Slice(rows, func(left, right int) bool {
		if rows[left].SrcPath != rows[right].SrcPath {
			return rows[left].SrcPath < rows[right].SrcPath
		}
		if rows[left].DstPath != rows[right].DstPath {
			return rows[left].DstPath < rows[right].DstPath
		}
		return rows[left].Kind < rows[right].Kind
	})
}
