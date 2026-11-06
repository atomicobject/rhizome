package codeanchor_test

import (
	"context"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

func TestNotesForFileUsesPHPIdentityAndPreservesUnknownPaths(t *testing.T) {
	for _, tt := range []struct {
		path string
		want []string
	}{
		{"src/worker.php", []string{"php-run"}},
		{"src/worker.template", []string{"php-run", "python-run"}},
	} {
		t.Run(tt.path, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			store, err := semdb.Open(currentSchemaAnchorTestDBPath(t, filepath.Join(root, "notes-language.db")))
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			svc := codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
			require.NoError(t, store.ReplaceFileSummary(ctx, codeanchor.FileSummary{
				FilePath: tt.path, Lang: codeanchor.LangPhp,
				Symbols: []codeanchor.Symbol{{Lang: codeanchor.LangPhp, Kind: codeanchor.SymFunc, File: tt.path, Name: "run"}},
			}))
			for _, anchor := range []codeanchor.Anchor{
				{Label: "php-run", Lang: codeanchor.LangPhp, Kind: codeanchor.AnchorFunc, BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangPhp, Name: "run"}},
				{Label: "python-run", Lang: codeanchor.LangPy, Kind: codeanchor.AnchorFunc, BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangPy, Name: "run"}},
			} {
				require.NoError(t, svc.ApplyNoteIndexBatch(ctx, []codeanchor.NoteIndexWork{{
					Path: anchor.Label + ".md", Note: codeanchor.Note{Path: anchor.Label + ".md", Title: anchor.Label, DefinedAnchors: []codeanchor.Anchor{anchor}}, KeepLabels: []string{anchor.Label},
				}}))
			}
			id, ok := store.AnchorIDByLabel(ctx, "php-run")
			require.True(t, ok)
			// Dirty selectors must be matched against the current file while an old scope is still present.
			require.NoError(t, store.SetAnchorScope(ctx, id, []string{"previous"}, nil))
			require.True(t, svc.HasDirtyAnchors())
			got, err := svc.NotesForFile(ctx, tt.path)
			require.NoError(t, err)
			require.ElementsMatch(t, tt.want, got.Anchors)
		})
	}
}
