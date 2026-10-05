package notemeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type capturedNoteContent struct {
	path    string
	content string
	mtime   int64
}

func captureNoteContents(ctx context.Context, vaultDef obsidian.VaultDefinition, reader obsidian.NoteReader) ([]capturedNoteContent, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if provider, ok := reader.(cacheEntriesProvider); ok {
		entries, err := provider.EntriesSnapshot(ctx)
		if err == nil {
			contents := make([]capturedNoteContent, 0, len(entries))
			for _, entry := range entries {
				if strings.TrimSpace(entry.Path) == "" {
					continue
				}
				path, err := paths.CleanNotePath(entry.Path)
				if err != nil {
					return nil, false, fmt.Errorf("canonical note path %q: %w", entry.Path, err)
				}
				contents = append(contents, capturedNoteContent{path: path.String(), content: entry.Content, mtime: entry.ModTime.Unix()})
			}
			sortCapturedContents(contents)
			return contents, true, nil
		}
	}
	selected, err := reader.GetNotesList(vaultDef)
	if err != nil {
		return nil, false, err
	}
	selected = dedupeStrings(selected)
	contents := make([]capturedNoteContent, 0, len(selected))
	for _, path := range selected {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		canonical, err := paths.CleanNotePath(path)
		if err != nil {
			return nil, false, fmt.Errorf("canonical note path %q: %w", path, err)
		}
		content, err := reader.GetContents(vaultDef, path)
		if err != nil {
			return nil, false, fmt.Errorf("read note %s: %w", path, err)
		}
		contents = append(contents, capturedNoteContent{path: canonical.String(), content: content})
	}
	sortCapturedContents(contents)
	return contents, false, nil
}

func sortCapturedContents(contents []capturedNoteContent) {
	sort.SliceStable(contents, func(left, right int) bool { return contents[left].path < contents[right].path })
}

func capturedNotesHash(contents []capturedNoteContent) (string, []string) {
	var aggregate [sha256.Size]byte
	selected := make([]string, 0, len(contents))
	for _, content := range contents {
		selected = append(selected, content.path)
		xorDigest(&aggregate, noteStateDigest(content.path, contentHash(content.content)))
	}
	return hex.EncodeToString(aggregate[:]), selected
}

func (i Indexer) projectCapturedContents(ctx context.Context, vaultDef obsidian.VaultDefinition, reader obsidian.NoteReader, contents []capturedNoteContent, capturedMtimes bool) ([]projectedNoteEntry, error) {
	return loadBounded(ctx, len(contents), func(index int) (projectedNoteEntry, error) {
		content := contents[index]
		mtime := content.mtime
		if !capturedMtimes {
			observed, err := reader.GetModTime(vaultDef, content.path)
			if err != nil {
				return projectedNoteEntry{}, fmt.Errorf("stat note %s: %w", content.path, err)
			}
			mtime = observed.Unix()
		}
		return i.projectEntry(content.path, content.content, mtime)
	})
}
