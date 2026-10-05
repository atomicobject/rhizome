package cache

import (
	"errors"
	stdpath "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type fakeNoteReader struct {
	notes map[string]string
}

func (n *fakeNoteReader) GetContents(def obsidian.VaultDefinition, noteName string) (string, error) {
	key := paths.NormalizeNotePath(noteName).String()
	content, ok := n.notes[key]
	if !ok && stdpath.Ext(key) == "" {
		content, ok = n.notes[paths.NormalizeNote(key).String()]
	}
	if !ok {
		return "", errors.New(obsidian.NoteDoesNotExistError)
	}
	return content, nil
}

func (n *fakeNoteReader) GetNotesList(def obsidian.VaultDefinition) ([]string, error) {
	out := make([]string, 0, len(n.notes))
	for k := range n.notes {
		out = append(out, k)
	}
	return out, nil
}

func (n *fakeNoteReader) GetModTime(def obsidian.VaultDefinition, notePath string) (time.Time, error) {
	return time.Now(), nil
}

func (n *fakeNoteReader) Title(path string) (string, bool) {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}
