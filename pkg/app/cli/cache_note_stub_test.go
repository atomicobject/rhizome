package actions

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// cachedNoteReader is a lightweight NoteReader that serves cached entries and tracks calls.
type cachedNoteReader struct {
	entries          []cache.Entry
	contentsCalls    int
	notesListCalls   int
	allowNotesReturn bool
}

func (c *cachedNoteReader) EntriesSnapshot(ctx context.Context) ([]cache.Entry, error) {
	return c.entries, nil
}

func (c *cachedNoteReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	c.contentsCalls++
	return "", errors.New("GetContents should not be called when cache is used")
}
func (c *cachedNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	c.notesListCalls++
	if c.allowNotesReturn {
		return []string{}, nil
	}
	return nil, errors.New("GetNotesList should not be called when cache is used")
}

func (c *cachedNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, errors.New("GetModTime should not be called when cache is used")
}

func (c *cachedNoteReader) Title(path string) (string, bool) {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}
