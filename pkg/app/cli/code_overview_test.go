package actions

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semstore "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type stubOverviewIntel struct {
	anchors      []codeanchor.IntelAnchor
	stats        map[string]codeanchor.DocStats
	links        map[string][]codeanchor.DocLink
	docScores    map[string]semstore.GraphDocScore
	anchorScores map[string]float64
	err          error
}

func (s *stubOverviewIntel) IntelAnchors(ctx context.Context) ([]codeanchor.IntelAnchor, error) {
	return s.anchors, s.err
}

func (s *stubOverviewIntel) DocStatsForFQNs(ctx context.Context, lang string, fqns []string) (map[string]codeanchor.DocStats, error) {
	out := map[string]codeanchor.DocStats{}
	for _, fqn := range fqns {
		if ds, ok := s.stats[fqn]; ok {
			out[fqn] = ds
		}
	}
	return out, nil
}

func (s *stubOverviewIntel) DocLinksFromCodePath(ctx context.Context, srcPath string, limit int) ([]codeanchor.DocLink, error) {
	return s.links[srcPath], nil
}

func (s *stubOverviewIntel) GraphDocScoresByPaths(ctx context.Context, paths []string) (map[string]semstore.GraphDocScore, error) {
	if s.docScores == nil {
		return map[string]semstore.GraphDocScore{}, nil
	}
	out := make(map[string]semstore.GraphDocScore)
	for _, p := range paths {
		if sc, ok := s.docScores[p]; ok {
			out[p] = sc
		}
	}
	return out, nil
}

func (s *stubOverviewIntel) AnchorScoresByIDs(ctx context.Context, anchorIDs []string) (map[string]float64, error) {
	if s.anchorScores == nil {
		return map[string]float64{}, nil
	}
	out := make(map[string]float64)
	for _, id := range anchorIDs {
		if sc, ok := s.anchorScores[id]; ok {
			out[id] = sc
		}
	}
	return out, nil
}

type stubNoteMgr struct {
	files map[string]string
}

func (s *stubNoteMgr) NoteFacts() NoteFacts {
	return newFactReader(s, s.files).facts
}

func (s *stubNoteMgr) GetNotesList(_ obsidian.VaultDefinition) ([]string, error) { return nil, nil }
func (s *stubNoteMgr) GetModTime(_ obsidian.VaultDefinition, _ string) (time.Time, error) {
	return time.Time{}, nil
}
func (s *stubNoteMgr) GetContents(_ obsidian.VaultDefinition, noteName string) (string, error) {
	return s.files[noteName], nil
}
func (s *stubNoteMgr) Title(path string) (string, bool) {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}

type stubOverviewVault struct {
	def obsidian.VaultDefinition
}

func (s *stubOverviewVault) DefaultName() (string, error)                  { return s.def.Name, nil }
func (s *stubOverviewVault) SetDefaultName(string) error                   { return nil }
func (s *stubOverviewVault) Path() (string, error)                         { return s.def.BasePath(), nil }
func (s *stubOverviewVault) Definition() (obsidian.VaultDefinition, error) { return s.def, nil }

func TestCodeOverview_RenderedMarkdown(t *testing.T) {
	intel := &stubOverviewIntel{
		anchors: []codeanchor.IntelAnchor{
			{Path: "pkg/foo.go", FQN: "example/foo.Foo", Symbol: "Foo", Lang: codeanchor.LangGo, AnchorID: "a1"},
			{Path: "pkg/foo.go", FQN: "example/foo.Bar", Symbol: "Bar", Lang: codeanchor.LangGo, AnchorID: "a2"},
			{Path: "pkg/foo_test.go", FQN: "example/foo.TestFoo", Symbol: "TestFoo", Lang: codeanchor.LangGo, AnchorID: "a3"},
		},
		stats: map[string]codeanchor.DocStats{
			"example/foo.Foo": {Mentions: 3, Links: 1},
			"example/foo.Bar": {Mentions: 2, Links: 0},
		},
		links: map[string][]codeanchor.DocLink{
			"pkg/foo.go": {
				{DstPath: "docs/foo.md"},
			},
		},
		docScores: map[string]semstore.GraphDocScore{
			"pkg": {DocPath: "pkg", DocType: "code", Authority: 0.5, Hub: 0.3},
		},
		anchorScores: map[string]float64{
			"a1": 0.8,
			"a2": 0.6,
		},
	}

	notes := &stubNoteMgr{
		files: map[string]string{
			"docs/foo.md": "---\nsummary: foo summary\n---\n# Foo\n",
		},
	}

	vaultDef := obsidian.VaultDefinition{Path: "/tmp/vault"}

	res, text, err := CodeOverview(context.Background(), CodeOverviewOptions{
		Vault:        &stubOverviewVault{def: vaultDef},
		Notes:        notes,
		VaultDef:     vaultDef,
		Intel:        intel,
		VaultPath:    "/tmp/vault",
		Roots:        []string{"pkg"},
		LimitModules: 5,
		BudgetChars:  2000,
	})
	require.NoError(t, err)
	require.False(t, res.Truncated)
	require.Len(t, res.Modules, 1)
	require.Contains(t, text, "pkg/")
	require.Contains(t, text, "Docs: foo")
	require.Contains(t, text, "foo summary")
	require.NotContains(t, text, "foo_test.go") // default includeTests=false
}

func TestBuildModuleDocsUsesFutureProviderSearchRegionsWithoutMarkdownParsing(t *testing.T) {
	t.Parallel()

	const path = "pkg/README.future"
	reader := factReader{
		NoteReader: &stubNoteMgr{files: map[string]string{path: "---\nsummary: must-not-parse\n---\n# Must Not Parse\n"}},
		facts: NewNoteFacts([]notemeta.NoteSourceSnapshot{{
			Path:        paths.NormalizeNotePath(path),
			Format:      noteformat.FormatID("future"),
			Content:     "---\nsummary: must-not-parse\n---\n# Must Not Parse\n",
			Frontmatter: map[string]any{"summary": "provider summary"},
			Projection: noteformat.Projection{Facts: noteformat.ProjectionFacts{
				SearchRegions: []noteformat.SearchRegionFact{{
					Origin:    noteformat.SearchRegionDerived,
					Kind:      noteformat.SearchRegionVisible,
					Text:      "Future provider body. This is projected text.",
					MediaType: "text/plain",
				}},
			}},
		}}),
	}

	docs := buildModuleDocs(reader, "pkg", map[string]docNote{path: {Path: path}}, paths.VaultPaths{}, []string{"README.future"}, "README.future", 1)
	require.Len(t, docs, 1)
	require.Equal(t, "provider summary", docs[0].Summary)
	require.Equal(t, "Future provider body. This is projected text.", docs[0].Body)
	require.NotContains(t, docs[0].Body, "Must Not Parse")
}
