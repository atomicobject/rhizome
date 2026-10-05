package notemeta

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// loadProjectedEntries is a normal source-to-provider path. Cache
// adapters may supply captured bytes and filesystem facts, never parsed syntax
// facts; direct and cached reads therefore use the same projector result.
func (i Indexer) loadProjectedEntries(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader) ([]projectedNoteEntry, error) {
	if noteMgr == nil {
		return nil, fmt.Errorf("note reader is required")
	}
	if provider, ok := noteMgr.(cacheEntriesProvider); ok {
		entries, err := provider.EntriesSnapshot(ctx)
		if err == nil {
			return i.projectCacheEntries(ctx, entries)
		}
	}
	pathsList, err := noteMgr.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}
	return i.loadProjectedEntriesForPaths(ctx, vaultDef, noteMgr, pathsList)
}

func (i Indexer) loadProjectedEntriesForPaths(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, pathsList []string) ([]projectedNoteEntry, error) {
	pathsList = dedupeStrings(pathsList)
	entries, err := loadBounded(ctx, len(pathsList), func(index int) (projectedNoteEntry, error) {
		return i.readProjectedEntry(vaultDef, noteMgr, pathsList[index])
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(entries, func(left, right int) bool { return entries[left].Entry.Path < entries[right].Entry.Path })
	return entries, nil
}

func (i Indexer) projectCacheEntries(ctx context.Context, entries []cache.Entry) ([]projectedNoteEntry, error) {
	selected := make([]cache.Entry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Path) == "" {
			continue
		}
		selected = append(selected, entry)
	}
	sort.SliceStable(selected, func(left, right int) bool { return selected[left].Path < selected[right].Path })
	projected, err := loadBounded(ctx, len(selected), func(index int) (projectedNoteEntry, error) {
		entry := selected[index]
		return i.projectEntry(entry.Path, entry.Content, entry.ModTime.Unix())
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(projected, func(left, right int) bool { return projected[left].Entry.Path < projected[right].Entry.Path })
	return projected, nil
}

func (i Indexer) readProjectedEntry(vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, notePath string) (projectedNoteEntry, error) {
	content, err := noteMgr.GetContents(vaultDef, notePath)
	if err != nil {
		return projectedNoteEntry{}, fmt.Errorf("read note %s: %w", notePath, err)
	}
	modTime, err := noteMgr.GetModTime(vaultDef, notePath)
	if err != nil {
		return projectedNoteEntry{}, fmt.Errorf("stat note %s: %w", notePath, err)
	}
	return i.projectEntry(notePath, content, modTime.Unix())
}

func (i Indexer) projectEntry(path, content string, mtime int64) (projectedNoteEntry, error) {
	formats, err := i.runtime()
	if err != nil {
		return projectedNoteEntry{}, err
	}
	source, err := i.authoredSource(path, content, mtime)
	if err != nil {
		return projectedNoteEntry{}, err
	}
	projection, err := formats.Project(source)
	if err != nil {
		return projectedNoteEntry{}, fmt.Errorf("project note %q: %w", source.Path().String(), err)
	}
	return projectionEntryFrom(source, projection)
}
