package retrieval

import (
	"context"
	"fmt"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
)

type observedIntelLexicalStore struct {
	rows                                            []semdb.IntelSearchRow
	ftsReads, sectionReads, moduleReads, batchReads int
}

func (s *observedIntelLexicalStore) SearchIntelFTSFiltered(context.Context, string, int, semdb.IntelSearchFilters) ([]semdb.IntelSearchRow, error) {
	s.ftsReads++
	return s.rows, nil
}
func (s *observedIntelLexicalStore) IntelDocSectionsByPath(_ context.Context, path string) ([]codeanchor.IntelDocSection, error) {
	s.sectionReads++
	return []codeanchor.IntelDocSection{{SectionID: path, Path: path, Title: "Ranking", Level: 1, Content: "# Ranking\nmatch"}}, nil
}
func (s *observedIntelLexicalStore) IntelModuleAnchorIDByPath(_ context.Context, lang, path string) (string, bool, error) {
	s.moduleReads++
	return "module-" + path, true, nil
}
func (s *observedIntelLexicalStore) ModuleAnchorIDsByPaths(_ context.Context, paths []string) (map[string]string, error) {
	s.batchReads++
	out := map[string]string{}
	for _, path := range paths {
		out[path] = "module-" + path
	}
	return out, nil
}

func TestIntelLexicalRetrieverReadCounts(t *testing.T) {
	store := &observedIntelLexicalStore{}
	for i := 0; i < 2; i++ {
		path := fmt.Sprintf("notes/%d.md", i)
		store.rows = append(store.rows, semdb.IntelSearchRow{Type: "doc_section", ID: path, SourceNodeID: path + "#ranking-0", SectionIndex: 0, Path: path, Title: "Ranking", BodyHit: true, Snippet: "match"})
	}
	for i := 0; i < 3; i++ {
		path := fmt.Sprintf("src/match%d.go", i)
		store.rows = append(store.rows, semdb.IntelSearchRow{Type: "anchor", ID: path, Path: path, Lang: "go", Kind: "function"})
	}
	candidates, err := (&IntelLexicalRetriever{Store: store}).Retrieve(t.Context(), search.QuerySpec{Text: "match", Limits: search.Limits{Total: 10}})
	require.NoError(t, err)
	require.Len(t, candidates, 5)
	require.Equal(t, "notes/0.md#ranking-0", candidates[0].NodeRef.NodeID)
	require.Equal(t, "module-src/match0.go", candidates[2].AnchorID)
	require.Equal(t, 1, store.ftsReads)
	require.Equal(t, 0, store.sectionReads, "search must not hydrate section bodies")
	require.Equal(t, 0, store.moduleReads, "search must not look up modules per hit")
	require.Equal(t, 1, store.batchReads)
	t.Logf("reads: fts=%d section-bodies=%d module-per-hit=%d module-batch=%d total=%d", store.ftsReads, store.sectionReads, store.moduleReads, store.batchReads, store.ftsReads+store.sectionReads+store.moduleReads+store.batchReads)
}
