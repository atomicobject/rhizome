package notemeta

import (
	"context"
	"errors"
	"fmt"
	"sort"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// ErrProjectionNotCurrent reports that persisted source identity exists but
// its provider-derived facts cannot safely be consumed by a runtime-aware
// reader. Callers must rebuild metadata rather than treating stale or fatal
// facts as current.
var ErrProjectionNotCurrent = errors.New("note projection is not current")

// Indexer owns the runtime needed to turn canonical authored source bytes into
// raw note metadata. The runtime is explicit so no package-global provider
// configuration can make cache, indexing, and downstream reads disagree.
type Indexer struct {
	formats noteformat.Runtime
}

// NewIndexer constructs an explicit provider-aware note metadata indexer.
func NewIndexer(formats noteformat.Runtime) (Indexer, error) {
	if len(formats.Registry().IDs()) == 0 {
		return Indexer{}, fmt.Errorf("note format runtime is required")
	}
	return Indexer{formats: formats}, nil
}

func (i Indexer) runtime() (noteformat.Runtime, error) {
	if len(i.formats.Registry().IDs()) == 0 {
		return noteformat.Runtime{}, fmt.Errorf("note format runtime is required")
	}
	return i.formats, nil
}

// Validate reports whether the indexer has an explicit note-format runtime.
// Application entry points use this before opening stores or acquiring locks
// so a missing composition dependency fails without mutating durable state.
func (i Indexer) Validate() error {
	_, err := i.runtime()
	return err
}

// FormatRuntime returns the explicit immutable note-format runtime configured
// for this indexer. It applies the same validation as every Indexer operation
// so application composition cannot accidentally construct a second runtime.
func (i Indexer) FormatRuntime() (noteformat.Runtime, error) {
	return i.runtime()
}

// projectionCurrentForRow verifies one persisted row against the selected
// provider at its canonical authored path. The store deliberately keeps stale
// and fatal source identity inspectable; provider-aware consumers use this
// gate before consuming its derived facts.
func (i Indexer) projectionCurrentForRow(row semdb.NoteMetadataRow) bool {
	runtime, err := i.runtime()
	if err != nil {
		return false
	}
	path, err := paths.CleanNotePath(row.Path)
	if err != nil || path.String() != row.Path {
		return false
	}
	provider, ok := runtime.ProviderForPath(paths.RelPath(path))
	if !ok {
		return false
	}
	descriptor := provider.Descriptor()
	return row.FormatID == string(descriptor.ID) &&
		row.Projection.Status == semdb.NoteProjectionStatusCurrent &&
		row.Projection.ProviderVersion == descriptor.ProviderVersion &&
		row.Projection.ProjectionVersion == descriptor.ProjectionVersion &&
		row.Projection.SourceContentHash != "" &&
		row.Projection.SourceContentHash == row.ContentHash
}

// UsableNoteMetadataRowsByPaths returns provider-current durable rows without
// using the global metadata-ready bit as a read barrier. Stores that do not
// expose Rhizome's internal durable-row seam retain the existing globally
// gated behavior.
func (i Indexer) UsableNoteMetadataRowsByPaths(ctx context.Context, store Store, rawPaths []string) (map[string]MetadataRow, error) {
	if store == nil {
		return map[string]MetadataRow{}, nil
	}
	pathsList := dedupeStrings(rawPaths)
	sort.Strings(pathsList)
	provider, ok := store.(durableMetadataRowsProvider)
	var rows map[string]MetadataRow
	var err error
	if !ok {
		rows, err = store.CurrentNoteMetadataRowsByPaths(ctx, pathsList)
	} else {
		rows, err = provider.DurableNoteMetadataRowsByPaths(ctx, pathsList)
	}
	if err != nil {
		return nil, err
	}
	for path, row := range rows {
		if !i.projectionCurrentForRow(row) {
			delete(rows, path)
		}
	}
	return rows, nil
}

// UsableNoteFactsByPaths returns one atomic durable fact snapshot after
// removing rows whose format-provider projection is no longer current. The
// store must provide the atomic snapshot capability; separate reads would mix
// committed generations during concurrent ownership reconciliation.
func (i Indexer) UsableNoteFactsByPaths(ctx context.Context, store Store, rawPaths []string) (semdb.DurableNoteFacts, error) {
	if store == nil {
		return semdb.DurableNoteFacts{MetadataRows: map[string]MetadataRow{}}, nil
	}
	provider, ok := store.(durableNoteFactsProvider)
	if !ok {
		return semdb.DurableNoteFacts{}, fmt.Errorf("atomic durable note facts are unavailable")
	}
	facts, err := provider.DurableNoteFactsByPaths(ctx, dedupeStrings(rawPaths))
	if err != nil {
		return semdb.DurableNoteFacts{}, err
	}
	for path, row := range facts.MetadataRows {
		if !i.projectionCurrentForRow(row) {
			delete(facts.MetadataRows, path)
		}
	}
	if len(facts.MetadataRows) == 0 {
		facts.PropertyValues = nil
		facts.Tags = nil
		return facts, nil
	}
	properties := facts.PropertyValues[:0]
	for _, row := range facts.PropertyValues {
		if _, ok := facts.MetadataRows[row.NotePath]; ok {
			properties = append(properties, row)
		}
	}
	facts.PropertyValues = properties
	tags := facts.Tags[:0]
	for _, row := range facts.Tags {
		if _, ok := facts.MetadataRows[row.NotePath]; ok {
			tags = append(tags, row)
		}
	}
	facts.Tags = tags
	return facts, nil
}

// UsableNoteMetadataCount reports provider-current durable rows admitted by
// the caller's current ownership policy. It is used only to distinguish a
// cold store from a warm exact-note read model during startup.
func (i Indexer) UsableNoteMetadataCount(ctx context.Context, store Store, admitted func(string) bool) (int64, error) {
	if store == nil {
		return 0, nil
	}
	pathsList, err := store.AllNoteMetadataPaths(ctx)
	if err != nil {
		return 0, err
	}
	rows, err := i.UsableNoteMetadataRowsByPaths(ctx, store, pathsList)
	if err != nil {
		return 0, err
	}
	var count int64
	for path := range rows {
		if admitted == nil || admitted(path) {
			count++
		}
	}
	return count, nil
}
