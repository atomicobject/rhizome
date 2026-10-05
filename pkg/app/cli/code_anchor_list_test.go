package actions

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

type codeAnchorListScope struct {
	symbols   []string
	callFiles []string
}

type stubCodeAnchorListStore struct {
	indexedFiles    []string
	notes           map[int64][]codeanchor.Note
	scopes          map[int64]codeAnchorListScope
	definitionFiles map[string][]string
}

func (s *stubCodeAnchorListStore) IndexedFilePaths(context.Context) ([]string, error) {
	return s.indexedFiles, nil
}

func (s *stubCodeAnchorListStore) NotesForAnchor(_ context.Context, anchorID int64) ([]codeanchor.Note, error) {
	return s.notes[anchorID], nil
}

func (s *stubCodeAnchorListStore) AnchorScope(_ context.Context, anchorID int64) ([]string, []string, error) {
	scope := s.scopes[anchorID]
	return scope.symbols, scope.callFiles, nil
}

func (s *stubCodeAnchorListStore) FilesDefiningSymbolFQN(_ context.Context, fqn string) ([]string, error) {
	return s.definitionFiles[fqn], nil
}

func TestBuildCodeAnchorListItemsAggregatesIndexedData(t *testing.T) {
	store := &stubCodeAnchorListStore{
		indexedFiles: []string{"src/a.go", "src/nested/b.go", "docs/readme.md"},
		notes: map[int64][]codeanchor.Note{
			1: {{Path: "notes/z.md"}, {Path: "notes/a.md"}},
		},
		scopes: map[int64]codeAnchorListScope{
			1: {
				symbols:   []string{"example.Z", "example.A"},
				callFiles: []string{"src/z.go", "src/a.go"},
			},
		},
		definitionFiles: map[string][]string{
			"example.A": {"src/a.go"},
			"example.Z": {"src/z.go", "src/a.go"},
		},
	}

	anchors := []codeanchor.Anchor{
		{ID: 2, Label: "Path", Kind: codeanchor.AnchorPath, PathPrefix: "src"},
		{ID: 1, Label: "Function", Kind: codeanchor.AnchorFunc, Lang: "go"},
	}

	items, err := BuildCodeAnchorListItems(context.Background(), store, anchors, 1)
	require.NoError(t, err)
	require.Equal(t, []CodeAnchorListItem{
		{
			Label:           "Function",
			Kind:            codeanchor.AnchorFunc,
			Lang:            "go",
			NotePaths:       []string{"notes/a.md", "notes/z.md"},
			SymbolCount:     2,
			CallCount:       2,
			Symbols:         []string{"example.A"},
			CallFiles:       []string{"src/a.go"},
			DefinitionFiles: []string{"src/a.go"},
			Truncated:       true,
		},
		{
			Label:             "Path",
			Kind:              codeanchor.AnchorPath,
			PathPrefix:        "src",
			NotePaths:         []string{},
			MatchedFiles:      []string{"src/a.go"},
			MatchedFilesCount: 2,
			Truncated:         true,
		},
	}, items)
	require.Equal(t, "Path", anchors[0].Label, "building results must not reorder caller-owned anchors")
}

func TestBuildCodeAnchorListItemsMatchesGlobs(t *testing.T) {
	store := &stubCodeAnchorListStore{
		indexedFiles: []string{"src/a.go", "src/a_test.go", "src/a.ts"},
		notes:        map[int64][]codeanchor.Note{},
		scopes:       map[int64]codeAnchorListScope{},
	}
	anchors := []codeanchor.Anchor{{ID: 1, Label: "Go", Kind: codeanchor.AnchorGlob, Globs: []string{"**/*.go"}}}

	items, err := BuildCodeAnchorListItems(context.Background(), store, anchors, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"src/a.go", "src/a_test.go"}, items[0].MatchedFiles)
	require.Equal(t, 2, items[0].MatchedFilesCount)
	require.False(t, items[0].Truncated)
}

func TestMatchCodeAnchorFilesByGlobsNormalizesWindowsPaths(t *testing.T) {
	files := []string{`C:\repo\src\components\Button.tsx`}
	patterns := []string{"C:/repo/src/**/*.tsx"}

	matches, count := matchCodeAnchorFilesByGlobs(files, patterns, 10)
	require.Equal(t, 1, count)
	require.Equal(t, []string{files[0]}, matches)
}

func TestMatchCodeAnchorFilesByPrefixUsesNormalizedSeparators(t *testing.T) {
	files := []string{`src\a.go`, `src\nested\b.go`, `src-other\c.go`}

	matches, count := matchCodeAnchorFilesByPrefix(files, "src", 10)
	require.Equal(t, 2, count)
	require.Equal(t, files[:2], matches)
}
