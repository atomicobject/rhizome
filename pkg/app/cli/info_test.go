package actions

import (
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/mocks"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type infoFactsReader struct {
	obsidian.NoteReader
	facts NoteFacts
}

func (r infoFactsReader) NoteFacts() NoteFacts { return r.facts }

func TestGetFileInfo(t *testing.T) {
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: t.TempDir()}
	for _, tc := range []struct {
		name      string
		path      string
		facts     []notemeta.NoteSourceSnapshot
		source    string
		sourceErr error
		want      *FileInfo
		wantErr   string
	}{
		{
			name: "authoritative facts disagree with source",
			path: "test.md",
			facts: []notemeta.NoteSourceSnapshot{{
				Path:        paths.NormalizeNotePath("test.md"),
				Frontmatter: map[string]any{"status": "projected"},
				Tags:        []string{"indexed", "decision"},
			}},
			source: "---\nstatus: stale\n---\n#wrong",
			want:   &FileInfo{Frontmatter: map[string]any{"status": "projected"}, Tags: []string{"indexed", "decision"}},
		},
		{
			name:    "missing facts give index guidance",
			path:    "test.md",
			source:  "#existing",
			wantErr: "current note metadata is unavailable; run rzm index",
		},
		{
			name:      "missing file propagates source error",
			path:      "absent.md",
			sourceErr: errors.New("file not found"),
			wantErr:   "file not found",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := &mocks.VaultManager{}
			vault.On("Definition").Return(vaultDef, nil)
			source := &mocks.NoteReader{}
			if tc.want == nil {
				source.On("GetContents", mock.Anything, tc.path).Return(tc.source, tc.sourceErr)
			}
			got, err := GetFileInfo(vault, infoFactsReader{NoteReader: source, facts: NewNoteFacts(tc.facts)}, tc.path)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
				source.AssertNotCalled(t, "GetContents", mock.Anything, tc.path)
			}
			vault.AssertExpectations(t)
			source.AssertExpectations(t)
		})
	}
	t.Run("vault definition error", func(t *testing.T) {
		vault := &mocks.VaultManager{}
		vault.On("Definition").Return(obsidian.VaultDefinition{}, errors.New("vault path error"))
		got, err := GetFileInfo(vault, &mocks.NoteReader{}, "test.md")
		require.EqualError(t, err, "vault path error")
		require.Nil(t, got)
	})
}
