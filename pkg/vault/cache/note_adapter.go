package cache

// Docs:
// - [Vault cache service (Service)](docs/reference/analysis/Vault cache service (Service).md)

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// NoteAdapter implements obsidian.NoteReader backed by the cache. It falls back
// to the underlying NoteReader when entries are missing.
type NoteAdapter struct {
	cache *Service
	base  obsidian.NoteReader
}

// NewNoteAdapter constructs a cached NoteReader.
func NewNoteAdapter(cache *Service, base obsidian.NoteReader) *NoteAdapter {
	return &NoteAdapter{
		cache: cache,
		base:  base,
	}
}

func (n *NoteAdapter) GetContents(vaultDef obsidian.VaultDefinition, noteName string) (string, error) {
	if err := n.cache.Refresh(context.Background()); err != nil {
		return "", err
	}
	if entry, ok := n.cache.Entry(noteName); ok && entry.Content != "" {
		return entry.Content, nil
	}
	return n.base.GetContents(vaultDef, noteName)
}

// Frontmatter returns parsed YAML frontmatter from the cache, if available.
//
// This is intentionally a cache-first fast path: it does not fall back to disk.
// Callers that need a guaranteed read should use GetContents + ExtractFrontmatter.
func (n *NoteAdapter) Frontmatter(vaultDef obsidian.VaultDefinition, noteName string) (map[string]interface{}, bool, error) {
	_ = vaultDef
	if err := n.cache.Refresh(context.Background()); err != nil {
		return nil, false, err
	}
	if entry, ok := n.cache.Entry(noteName); ok {
		return entry.Frontmatter, true, nil
	}
	return nil, false, nil
}

func (n *NoteAdapter) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	if err := n.cache.Refresh(context.Background()); err != nil {
		return nil, err
	}
	return n.cache.Paths(), nil
}

func (n *NoteAdapter) GetModTime(vaultDef obsidian.VaultDefinition, notePath string) (time.Time, error) {
	return n.base.GetModTime(vaultDef, notePath)
}

func (n *NoteAdapter) Title(path string) (string, bool) {
	if entry, ok := n.cache.Entry(path); ok && entry.Path != "" {
		base := filepath.Base(entry.Path)
		return strings.TrimSuffix(base, filepath.Ext(base)), true
	}
	return n.base.Title(path)
}

// MarkDirty records a vault-relative path the caller changed; follow it with
// Refresh so later reads see the change without waiting for the watcher.
func (n *NoteAdapter) MarkDirty(relPath string, kind DirtyKind) {
	n.cache.MarkDirty(relPath, kind)
}

// Refresh forces the underlying cache to reconcile watcher events.
func (n *NoteAdapter) Refresh(ctx context.Context) error {
	return n.cache.Refresh(ctx)
}

// EntriesSnapshot exposes cached entries when the consumer can take advantage of them.
func (n *NoteAdapter) EntriesSnapshot(ctx context.Context) ([]Entry, error) {
	return n.cache.EntriesSnapshot(ctx)
}

// NoteEntriesSnapshot exposes obsidian.NoteEntry slices for analysis that can reuse cached metadata.
func (n *NoteAdapter) NoteEntriesSnapshot(ctx context.Context) ([]obsidian.NoteEntry, error) {
	entries, err := n.cache.EntriesSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]obsidian.NoteEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, obsidian.NoteEntry{
			Path:        e.Path,
			Content:     e.Content,
			Frontmatter: e.Frontmatter,
			Tags:        e.Tags,
			ContentTime: e.ContentTime,
		})
	}
	return out, nil
}

// Version exposes the cache version for downstream caches.
func (n *NoteAdapter) Version() uint64 {
	return n.cache.Version()
}
