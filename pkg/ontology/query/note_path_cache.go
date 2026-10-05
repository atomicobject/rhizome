package query

import (
	"context"
	"sort"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func currentNotePaths(ctx context.Context, deps Deps) ([]string, error) {
	if deps.Store == nil {
		return nil, nil
	}
	metadataRows, err := deps.Store.CurrentNoteMetadataRows(ctx)
	if err != nil {
		return nil, err
	}
	eligible := make(map[string]struct{}, len(metadataRows))
	for _, row := range metadataRows {
		if row.Projection.Status != semdb.NoteProjectionStatusCurrent {
			continue
		}
		format := noteformat.FormatID(row.FormatID)
		if provider, ok := deps.NoteFormats.Provider(format); ok {
			selected, claimsPath := deps.NoteFormats.ProviderForPath(paths.RelPath(row.Path))
			if deps.NoteFormats.CanProject(format) && claimsPath && selected.Descriptor().ID == provider.Descriptor().ID {
				eligible[row.Path] = struct{}{}
			}
			continue
		}
		if format == noteformat.FormatID("markdown") {
			eligible[row.Path] = struct{}{}
		}
	}

	var available []string
	if provider, ok := deps.NoteReader.(obsidian.NoteEntriesProvider); ok {
		entries, err := provider.NoteEntriesSnapshot(ctx)
		if err != nil {
			return nil, err
		}
		available = make([]string, 0, len(entries))
		for _, entry := range entries {
			available = append(available, entry.Path)
		}
	} else {
		available, err = deps.NoteReader.GetNotesList(deps.VaultDef)
		if err != nil {
			return nil, err
		}
	}

	paths := make([]string, 0, len(available))
	seen := make(map[string]struct{}, len(available))
	for _, path := range available {
		if _, ok := eligible[path]; !ok {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func buildCurrentNotePathCache(ctx context.Context, deps Deps) (*obsidian.NotePathCache, error) {
	paths, err := currentNotePaths(ctx, deps)
	if err != nil {
		return nil, err
	}
	var aliasesByPath map[string][]string
	if deps.Store != nil {
		if rows, err := deps.Store.CurrentNoteAliases(ctx); err == nil {
			aliasesByPath = make(map[string][]string, len(rows))
			current := make(map[string]struct{}, len(paths))
			for _, path := range paths {
				current[path] = struct{}{}
			}
			for path, aliases := range rows {
				if _, ok := current[path]; ok {
					aliasesByPath[path] = aliases
				}
			}
		}
	}
	return obsidian.BuildNotePathCacheWithAliases(paths, aliasesByPath), nil
}
