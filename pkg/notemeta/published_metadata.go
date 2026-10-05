package notemeta

import (
	"context"
	"fmt"
	"sort"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// PublishedMetadataPreparation holds one sealed-source projection pass for a
// complete ownership-routed note set. Its contents remain private so callers
// can only use provider status to route downstream work and then reuse this
// exact preparation for metadata publication.
// descriptor-only source identity.
func (i Indexer) BuildPublishedMetadataDelta(
	ctx context.Context,
	vaultDef obsidian.VaultDefinition,
	noteMgr obsidian.NoteReader,
	store Store,
	notePaths []paths.NotePath,
	sealed map[paths.NotePath]noteformat.AuthoredSource,
) (*MetadataDelta, error) {
	if store == nil {
		return nil, fmt.Errorf("note metadata store is required")
	}
	preparation, err := i.PreparePublishedMetadata(ctx, notePaths, sealed)
	if err != nil {
		return nil, err
	}
	return i.BuildPublishedMetadataDeltaFromPreparation(ctx, vaultDef, noteMgr, store, preparation)
}

// BuildPublishedMetadataDeltaFromPreparation builds the metadata delta from a
// prior PreparePublishedMetadata result. The preparation stays opaque so this
// is the only path that can consume provider facts.
func (i Indexer) BuildPublishedMetadataDeltaFromPreparation(
	ctx context.Context,
	vaultDef obsidian.VaultDefinition,
	noteMgr obsidian.NoteReader,
	store Store,
	preparation PublishedMetadataPreparation,
) (*MetadataDelta, error) {
	if store == nil {
		return nil, fmt.Errorf("note metadata store is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	formats, err := i.runtime()
	if err != nil {
		return nil, err
	}
	if err := validatePublishedMetadataPreparation(formats, preparation); err != nil {
		return nil, err
	}

	fatalRows, fatalPaths, err := publishedDurableFatalRows(ctx, store, formats, subtractPublishedPaths(preparation.projectable, preparation.sealedPaths))
	if err != nil {
		return nil, err
	}
	readPaths := subtractPublishedPaths(subtractPublishedPaths(preparation.projectable, preparation.sealedPaths), fatalPaths)
	if len(readPaths) > 0 && noteMgr == nil {
		return nil, fmt.Errorf("note reader is required for unsealed projectable sources")
	}
	readEntries, err := i.loadProjectedEntriesForPaths(ctx, vaultDef, noteMgr, readPaths)
	if err != nil {
		return nil, err
	}
	entries, err := validatePublishedProjectableEntries(subtractPublishedPaths(preparation.projectable, fatalPaths), preparation.sealedEntries, readEntries, formats)
	if err != nil {
		return nil, err
	}

	descriptorRows, err := publishedDescriptorRows(ctx, store, formats, preparation.descriptors)
	if err != nil {
		return nil, err
	}
	if err := publishedRowsHaveNoUnexpectedPaths(ctx, store, preparation.canonicalPaths); err != nil {
		return nil, err
	}

	rows := make([]semdb.NoteMetadataRow, 0, len(entries)+len(descriptorRows)+len(fatalRows))
	for _, entry := range entries {
		rows = append(rows, projectionNoteRow(entry, 0))
	}
	rows = append(rows, descriptorRows...)
	rows = append(rows, fatalRows...)
	rawNotesHash := notesHashFromRows(rows)
	current, err := i.publishedMetadataCurrent(ctx, vaultDef, store, rawNotesHash, preparation.canonicalPaths)
	if err != nil {
		return nil, err
	}
	if current {
		delta, settled, err := publishedCurrentStateDelta(ctx, store, entries)
		if err != nil {
			return nil, err
		}
		if settled {
			// The full-index ownership set is already represented by the current
			// provider-aware metadata state. In particular, retain LoadedAt so the
			// published ontology state remains the same generation; either there is
			// no metadata delta at all, or only refreshed freshness evidence.
			return delta, nil
		}
	}

	currentEntries := currentProjectedEntries(entries)
	cache := obsidian.BuildNotePathCacheWithAliases(projectedPaths(currentEntries), projectionAliases(currentEntries))
	loadedAt, err := publishedMetadataLoadedAt(ctx, store)
	if err != nil {
		return nil, err
	}
	return i.buildDelta(vaultDef, cache, entries, nil, loadedAt, rawNotesHash, preparation.canonicalPaths)
}

// publishedCurrentStateDelta supplements the raw source hash, which tracks
// content identity only. A provider can change a projection from current to
// fatal without changing it, so the durable row must match the provider result
// before a metadata rewrite is suppressed. It reports settled=true when the
// published generation is retained: either nothing at all changed, or only
// filesystem freshness evidence did.
func publishedCurrentStateDelta(ctx context.Context, store Store, entries []projectedNoteEntry) (*MetadataDelta, bool, error) {
	if len(entries) == 0 {
		return nil, true, nil
	}
	durable, err := store.CurrentNoteMetadataRowsByPaths(ctx, projectedPaths(entries))
	if err != nil {
		return nil, false, err
	}
	changed, touches := partitionProjectedEntries(durable, entries)
	if len(changed) > 0 {
		return nil, false, nil
	}
	if len(touches) == 0 {
		return nil, true, nil
	}
	state, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return nil, false, err
	}
	return &MetadataDelta{State: state, SourceTouches: touches}, true, nil
}

func publishedPathDescriptors(formats noteformat.Runtime, canonicalPaths []string) ([]string, map[string]noteformat.Descriptor, error) {
	projectable := make([]string, 0, len(canonicalPaths))
	descriptors := make(map[string]noteformat.Descriptor, len(canonicalPaths))
	for _, rawPath := range canonicalPaths {
		notePath, err := paths.CleanNotePath(rawPath)
		if err != nil || notePath.String() != rawPath {
			return nil, nil, fmt.Errorf("published note path %q must be canonical and vault-relative", rawPath)
		}
		provider, ok := formats.ProviderForPath(paths.RelPath(notePath))
		if !ok {
			return nil, nil, fmt.Errorf("no note format provider claims %q", notePath)
		}
		descriptor := provider.Descriptor()
		descriptors[notePath.String()] = descriptor
		if formats.CanProject(descriptor.ID) {
			projectable = append(projectable, notePath.String())
		}
	}
	return projectable, descriptors, nil
}

func (i Indexer) projectSealedPublishedSources(
	ctx context.Context,
	formats noteformat.Runtime,
	canonicalPaths, projectable []string,
	sealed map[paths.NotePath]noteformat.AuthoredSource,
) ([]projectedNoteEntry, map[string]struct{}, error) {
	if len(sealed) == 0 {
		return nil, map[string]struct{}{}, nil
	}
	selected := make(map[string]struct{}, len(canonicalPaths))
	for _, notePath := range canonicalPaths {
		selected[notePath] = struct{}{}
	}
	projectableSet := make(map[string]struct{}, len(projectable))
	for _, notePath := range projectable {
		projectableSet[notePath] = struct{}{}
	}
	sources := make([]noteformat.AuthoredSource, 0, len(sealed))
	sealedPaths := make(map[string]struct{}, len(sealed))
	for mapPath, source := range sealed {
		canonical, err := paths.CleanNotePath(mapPath.String())
		if err != nil || canonical != mapPath {
			return nil, nil, fmt.Errorf("sealed note path %q must be canonical and vault-relative", mapPath)
		}
		path := canonical.String()
		if _, ok := selected[path]; !ok {
			return nil, nil, fmt.Errorf("sealed note path %q is not selected", path)
		}
		if _, ok := projectableSet[path]; !ok {
			return nil, nil, fmt.Errorf("sealed note path %q is not projectable", path)
		}
		if err := validatePublishedSource(formats, canonical, source); err != nil {
			return nil, nil, err
		}
		sources = append(sources, source)
		sealedPaths[path] = struct{}{}
	}
	sort.Slice(sources, func(left, right int) bool { return sources[left].Path() < sources[right].Path() })
	entries, err := i.loadProjectedEntriesFromSources(ctx, sources)
	if err != nil {
		return nil, nil, err
	}
	return entries, sealedPaths, nil
}

func validatePublishedSource(formats noteformat.Runtime, expected paths.NotePath, source noteformat.AuthoredSource) error {
	if source.Path() != expected {
		return fmt.Errorf("sealed source path %q does not match selected path %q", source.Path(), expected)
	}
	provider, ok := formats.ProviderForPath(paths.RelPath(expected))
	if !ok {
		return fmt.Errorf("no note format provider claims %q", expected)
	}
	descriptor := provider.Descriptor()
	if !formats.CanProject(descriptor.ID) {
		return fmt.Errorf("sealed source %q uses non-projectable format %q", expected, descriptor.ID)
	}
	if source.Format() != descriptor.ID {
		return fmt.Errorf("sealed source %q format %q does not match selected format %q", expected, source.Format(), descriptor.ID)
	}
	return nil
}

func (i Indexer) loadProjectedEntriesFromSources(ctx context.Context, sources []noteformat.AuthoredSource) ([]projectedNoteEntry, error) {
	formats, err := i.runtime()
	if err != nil {
		return nil, err
	}
	entries, err := loadBounded(ctx, len(sources), func(index int) (projectedNoteEntry, error) {
		source := sources[index]
		projection, err := formats.Project(source)
		if err != nil {
			return projectedNoteEntry{}, fmt.Errorf("project sealed note %q: %w", source.Path(), err)
		}
		return projectionEntryFrom(source, projection)
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(entries, func(left, right int) bool { return entries[left].Source.Path() < entries[right].Source.Path() })
	return entries, nil
}

func subtractPublishedPaths(projectable []string, sealed map[string]struct{}) []string {
	out := make([]string, 0, len(projectable))
	for _, notePath := range projectable {
		if _, ok := sealed[notePath]; !ok {
			out = append(out, notePath)
		}
	}
	return out
}

func validatePublishedProjectableEntries(projectable []string, sealed, read []projectedNoteEntry, formats noteformat.Runtime) ([]projectedNoteEntry, error) {
	entries := append(append([]projectedNoteEntry(nil), sealed...), read...)
	if len(entries) != len(projectable) {
		return nil, fmt.Errorf("published projectable source count does not match selected paths")
	}
	expected := make(map[string]struct{}, len(projectable))
	for _, notePath := range projectable {
		expected[notePath] = struct{}{}
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		path := entry.Source.Path().String()
		if _, ok := expected[path]; !ok {
			return nil, fmt.Errorf("projected source %q is not selected", path)
		}
		if _, duplicate := seen[path]; duplicate {
			return nil, fmt.Errorf("projectable source %q was projected more than once", path)
		}
		if err := validatePublishedSource(formats, entry.Source.Path(), entry.Source); err != nil {
			return nil, err
		}
		seen[path] = struct{}{}
	}
	for _, notePath := range projectable {
		if _, ok := seen[notePath]; !ok {
			return nil, fmt.Errorf("projectable selected source %q was not projected", notePath)
		}
	}
	sort.SliceStable(entries, func(left, right int) bool { return entries[left].Source.Path() < entries[right].Source.Path() })
	return entries, nil
}

func publishedDescriptorRows(ctx context.Context, store Store, formats noteformat.Runtime, descriptors map[string]noteformat.Descriptor) ([]semdb.NoteMetadataRow, error) {
	descriptorPaths := make([]string, 0, len(descriptors))
	for notePath, descriptor := range descriptors {
		if !formats.CanProject(descriptor.ID) {
			descriptorPaths = append(descriptorPaths, notePath)
		}
	}
	if len(descriptorPaths) == 0 {
		return nil, nil
	}
	provider, ok := store.(durableMetadataRowsProvider)
	if !ok {
		return nil, fmt.Errorf("note metadata store does not expose durable source rows")
	}
	sort.Strings(descriptorPaths)
	rowsByPath, err := provider.DurableNoteMetadataRowsByPaths(ctx, descriptorPaths)
	if err != nil {
		return nil, err
	}
	rows := make([]semdb.NoteMetadataRow, 0, len(descriptorPaths))
	for _, notePath := range descriptorPaths {
		row, found := rowsByPath[notePath]
		if !found {
			return nil, fmt.Errorf("published descriptor-only note metadata row is missing for %q", notePath)
		}
		if row.Path != notePath || !descriptorOnlyStaleProjection(formats, row) {
			return nil, fmt.Errorf("published descriptor-only note metadata row is not current durable provenance for %q", notePath)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func publishedRowsHaveNoUnexpectedPaths(ctx context.Context, store Store, selected []string) error {
	materialized, err := store.AllNoteMetadataPaths(ctx)
	if err != nil {
		return err
	}
	canonical, err := canonicalStoredNotePaths(materialized)
	if err != nil {
		return err
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, notePath := range selected {
		selectedSet[notePath] = struct{}{}
	}
	for _, notePath := range canonical {
		if _, ok := selectedSet[notePath]; !ok {
			return fmt.Errorf("published note metadata row %q is not in the complete selected path set", notePath)
		}
	}
	return nil
}

func currentProjectedEntries(entries []projectedNoteEntry) []projectedNoteEntry {
	out := make([]projectedNoteEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Projection.Status == noteformat.ProjectionStatusCurrent {
			out = append(out, entry)
		}
	}
	return out
}

func publishedMetadataLoadedAt(ctx context.Context, store Store) (int64, error) {
	state, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return 0, err
	}
	return nextLoadedAt(state.LoadedAt), nil
}

// publishedMetadataCurrent applies the normal provider-aware freshness
// contract to the ownership-routed complete path set. Descriptor-only stale
// rows are deliberately current source identity for this check, just as they
// are for EnsureIndexed: their provider has no executable projection to redo.
func (i Indexer) publishedMetadataCurrent(
	ctx context.Context,
	vaultDef obsidian.VaultDefinition,
	store Store,
	rawNotesHash string,
	canonicalPaths []string,
) (bool, error) {
	state, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return false, err
	}
	if !state.Ready || state.LoadedAt <= 0 || state.RawNotesHash != rawNotesHash {
		return false, nil
	}
	expectedStateHash, err := metadataStateHashWithSelectedProviders(vaultDef, rawNotesHash, i.formats, canonicalPaths)
	if err != nil {
		return false, err
	}
	if state.NotesHash != expectedStateHash {
		return false, nil
	}
	materialized, err := noteMetadataPathsMaterialized(ctx, store, canonicalPaths)
	if err != nil || !materialized {
		return materialized, err
	}
	stale, err := i.projectionRowsStale(ctx, store, canonicalPaths)
	if err != nil {
		return false, err
	}
	return !stale, nil
}
