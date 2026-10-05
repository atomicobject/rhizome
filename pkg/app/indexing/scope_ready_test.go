package indexing

import (
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestScopeReadyIndexMatchesCodeFootprints(t *testing.T) {
	t.Parallel()

	idx := newScopeReadyIndex()
	idx.seed(map[string][]codeanchor.Anchor{
		"docs/a.md": {
			{
				ID:      1,
				Kind:    codeanchor.AnchorFunc,
				BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangGo, Pkg: "app", Name: "Run"},
			},
			{
				ID:   2,
				Kind: codeanchor.AnchorAnnotation,
				Ann: &codeanchor.AnnotationSelector{
					Symbol: codeanchor.SymbolRef{Lang: codeanchor.LangGo, Pkg: "app.annotations", Name: "Transactional"},
				},
			},
			{
				ID:         3,
				Kind:       codeanchor.AnchorPath,
				PathPrefix: "pkg/service",
			},
			{
				ID:    4,
				Kind:  codeanchor.AnchorGlob,
				Globs: []string{"pkg/**/*_test.go"},
			},
		},
	})

	idx.noteCodeFootprints([]codeanchor.ScopeReadyFootprint{{
		Path:           "pkg/service/run_test.go",
		SymbolKeys:     []string{"go|app|Run"},
		AnnotationKeys: []string{"go|app.annotations|Transactional"},
	}})

	require.Equal(t, []int64{1, 2, 3, 4}, normalizeScopeAnchorIDs(idx.drainReady()))
}

func TestScopeReadyIndexReplacesNoteAnchors(t *testing.T) {
	t.Parallel()

	idx := newScopeReadyIndex()
	idx.seed(map[string][]codeanchor.Anchor{
		"docs/a.md": {{
			ID:      1,
			Kind:    codeanchor.AnchorFunc,
			BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangGo, Pkg: "app", Name: "Run"},
		}},
	})

	idx.replaceNoteAnchors("docs/a.md", []codeanchor.Anchor{{
		ID:      2,
		Kind:    codeanchor.AnchorFunc,
		BaseSym: &codeanchor.SymbolRef{Lang: codeanchor.LangGo, Pkg: "app", Name: "Serve"},
	}})

	idx.noteCodeFootprints([]codeanchor.ScopeReadyFootprint{{
		Path:       "pkg/service/run.go",
		SymbolKeys: []string{"go|app|Run"},
	}})
	require.Empty(t, idx.drainReady())

	idx.noteCodeFootprints([]codeanchor.ScopeReadyFootprint{{
		Path:       "pkg/service/serve.go",
		SymbolKeys: []string{"go|app|Serve"},
	}})
	require.Equal(t, []int64{2}, normalizeScopeAnchorIDs(idx.drainReady()))
}
