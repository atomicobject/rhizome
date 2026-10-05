package mocks

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/mock"
)

type NoteReader struct {
	mock.Mock
}

func (m *NoteReader) GetContents(def obsidian.VaultDefinition, noteName string) (string, error) {
	args := m.Called(def, noteName)
	return args.String(0), args.Error(1)
}

func (m *NoteReader) GetNotesList(def obsidian.VaultDefinition) ([]string, error) {
	args := m.Called(def)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	if notes, ok := args.Get(0).([]string); ok {
		return notes, args.Error(1)
	}
	return nil, errors.New("invalid type returned for notes list")
}

func (m *NoteReader) GetModTime(def obsidian.VaultDefinition, notePath string) (time.Time, error) {
	args := m.Called(def, notePath)
	if args.Get(0) == nil {
		return time.Time{}, args.Error(1)
	}
	if t, ok := args.Get(0).(time.Time); ok {
		return t, args.Error(1)
	}
	return time.Time{}, errors.New("invalid type returned for mod time")
}

func (m *NoteReader) Title(path string) (string, bool) {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}
