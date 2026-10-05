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

// NoteSourceSnapshot is the canonical raw-note source contract.
//
// WHY: downstream domains need note-file facts without inheriting a typed
// ontology meaning or the legacy anchors.Note DTO.
type NoteSourceSnapshot struct {
	Path        paths.NotePath
	Format      noteformat.FormatID
	Projection  noteformat.Projection
	Content     string
	RawSource   []byte
	ContentHash string
	Mtime       int64
	Size        int64
	Title       string

	Frontmatter     map[string]any
	InlineProps     map[string][]string
	Tags            []string
	Aliases         []string
	Links           []ResolvedNoteLink
	FragmentTargets []noteformat.FragmentTargetFact
	SearchRegions   []noteformat.SearchRegionFact
	Diagnostics     []noteformat.Diagnostic
	DocumentBase    *noteformat.DocumentBaseFact
}

// BuildNoteSourceSnapshots projects current authored sources through the
// Indexer's explicit runtime before adapting them into the downstream raw-note
// contract. It never re-parses provider syntax in notemeta.
func (i Indexer) BuildNoteSourceSnapshots(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader) ([]NoteSourceSnapshot, error) {
	if _, err := i.runtime(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := i.loadProjectedEntries(ctx, vaultDef, noteMgr)
	if err != nil {
		return nil, err
	}
	return sourceSnapshotsFromProjectedEntries(ctx, vaultDef, entries)
}

func sourceSnapshotsFromProjectedEntries(ctx context.Context, vaultDef obsidian.VaultDefinition, entries []projectedNoteEntry) ([]NoteSourceSnapshot, error) {
	pathsList := make([]string, 0, len(entries))
	aliases := make(map[string][]string, len(entries))
	for _, entry := range entries {
		if entry.Projection.Status != noteformat.ProjectionStatusCurrent {
			return nil, ErrProjectionNotCurrent
		}
		path := entry.Source.Path().String()
		pathsList = append(pathsList, path)
		if values := aliasValuesFromFacts(entry.Aliases); len(values) > 0 {
			aliases[path] = values
		}
	}
	cache := obsidian.BuildNotePathCacheWithAliases(pathsList, aliases)
	out := make([]NoteSourceSnapshot, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out = append(out, sourceSnapshotFromProjectedEntry(vaultDef, cache, entry))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// BuildNoteSourceFacts builds a deterministic current-source inventory for a
// consumer that owns structured link discovery. Production authority is always
// the live filesystem, never a caller-supplied cache adapter. Each enumerated
// note is read once and Links remain empty so the consumer performs exactly one
// sealed structured-link scan over the captured content.
func BuildNoteSourceFacts(ctx context.Context, vaultDef obsidian.VaultDefinition) ([]NoteSourceSnapshot, error) {
	return buildNoteSourceFactsWithReader(ctx, vaultDef, &obsidian.Note{})
}

// buildNoteSourceFactsWithReader is an internal instrumentation seam. Hiding
// optional cache-provider methods preserves the production one-read algorithm.
func buildNoteSourceFactsWithReader(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader) ([]NoteSourceSnapshot, error) {
	if noteMgr == nil {
		return nil, fmt.Errorf("note reader is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := loadEntries(ctx, vaultDef, directNoteReader{NoteReader: noteMgr})
	if err != nil {
		return nil, err
	}
	out := make([]NoteSourceSnapshot, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out = append(out, sourceFactsSnapshotFromEntry(entry))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// directNoteReader intentionally exposes only obsidian.NoteReader's methods;
// optional cache-provider methods on the wrapped implementation remain hidden.
type directNoteReader struct {
	obsidian.NoteReader
}

// LoadNoteSourceSnapshots loads source snapshots only when every projectable
// persisted source involved in candidate matching is current for this Indexer's
// selected provider runtime. A registered descriptor-only provider's expected
// stale source row remains durable but unavailable: it contributes no source
// reads, aliases, link-cache target, or downstream evidence.
func (i Indexer) LoadNoteSourceSnapshots(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store Store, notePaths []string) ([]NoteSourceSnapshot, error) {
	if store == nil {
		return nil, fmt.Errorf("note metadata store is required")
	}
	if noteMgr == nil {
		return nil, fmt.Errorf("note reader is required")
	}
	formats, err := i.runtime()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	allPaths, err := store.CurrentNoteMetadataPaths(ctx)
	if err != nil {
		return nil, err
	}
	allRows, err := store.CurrentNoteMetadataRowsByPaths(ctx, allPaths)
	if err != nil {
		return nil, err
	}
	eligibleAllPaths := make([]string, 0, len(allPaths))
	eligible := make(map[string]struct{}, len(allPaths))
	fatal := make(map[string]struct{})
	for _, path := range allPaths {
		row, ok := allRows[path]
		if !ok {
			return nil, ErrProjectionNotCurrent
		}
		if descriptorOnlyStaleProjection(formats, row) {
			continue
		}
		if row.Projection.Status == semdb.NoteProjectionStatusFatal {
			fatal[path] = struct{}{}
			continue
		}
		descriptor, selected := selectedProviderDescriptorForRow(formats, row)
		if selected && !formats.CanProject(descriptor.ID) {
			return nil, ErrProjectionNotCurrent
		}
		if !i.projectionCurrentForRow(row) {
			return nil, ErrProjectionNotCurrent
		}
		eligibleAllPaths = append(eligibleAllPaths, path)
		eligible[path] = struct{}{}
	}
	if len(notePaths) == 0 {
		notePaths = eligibleAllPaths
	}
	requestedPaths := make([]string, 0, len(notePaths))
	for _, path := range dedupeStrings(notePaths) {
		if _, known := allRows[path]; !known {
			return nil, ErrProjectionNotCurrent
		}
		if _, rejected := fatal[path]; rejected {
			return nil, ErrProjectionNotCurrent
		}
		if _, available := eligible[path]; available {
			requestedPaths = append(requestedPaths, path)
		}
	}
	entries, err := i.loadProjectedEntriesForPaths(ctx, vaultDef, noteMgr, requestedPaths)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		row, ok := allRows[entry.Source.Path().String()]
		if !ok || row.ContentHash != entry.Source.ContentHash() || row.Mtime != entry.Source.Mtime() || row.Size != entry.Source.Size() {
			return nil, ErrProjectionNotCurrent
		}
	}
	aliases, err := store.CurrentNoteAliases(ctx)
	if err != nil {
		return nil, err
	}
	if aliases == nil {
		aliases = make(map[string][]string)
	}
	for path := range aliases {
		if _, available := eligible[path]; !available {
			delete(aliases, path)
		}
	}
	for _, entry := range entries {
		path := entry.Source.Path().String()
		if values := aliasValuesFromFacts(entry.Aliases); len(values) > 0 {
			aliases[path] = values
		} else {
			delete(aliases, path)
		}
	}
	cache := obsidian.BuildNotePathCacheWithAliases(eligibleAllPaths, aliases)
	out := make([]NoteSourceSnapshot, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Projection.Status != noteformat.ProjectionStatusCurrent {
			return nil, ErrProjectionNotCurrent
		}
		out = append(out, sourceSnapshotFromProjectedEntry(vaultDef, cache, entry))
	}
	sort.Slice(out, func(left, right int) bool { return out[left].Path < out[right].Path })
	return out, nil
}

// ResolvedNoteLink is raw source evidence for an authored internal note link.
type ResolvedNoteLink struct {
	SourcePath  paths.NotePath
	TargetInput string
	TargetPath  paths.NotePath
	Kind        string
}
