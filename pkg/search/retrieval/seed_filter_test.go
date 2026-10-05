package retrieval

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/stretchr/testify/require"
)

func TestSeedVectorRetrieverAppliesTestFiltersBeforeLimit(t *testing.T) {
	for _, ownerType := range []string{"anchor", "doc_section"} {
		for _, testsOnly := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/testsOnly=%t", ownerType, testsOnly), func(t *testing.T) {
				ctx := context.Background()
				store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
				require.NoError(t, err)
				t.Cleanup(func() { _ = store.Close() })
				var seed knowledge.Handle
				var targetPath string
				vectors := map[string]embeddings.Embedding{}
				for i := range 35 {
					id := fmt.Sprintf("owner-%02d", i)
					path := fmt.Sprintf("pkg/near-%02d.go", i)
					if ownerType == "doc_section" {
						path = fmt.Sprintf("notes/near-%02d.md", i)
					}
					if testsOnly && i == 34 || !testsOnly && i < 34 {
						path = "tests/" + path
					}
					if i == 34 {
						targetPath = path
					}
					if ownerType == "anchor" {
						require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, []codeanchor.IntelAnchor{{
							AnchorID: id, Lang: codeanchor.LangGo, Kind: "function", Path: path, Symbol: id, Fingerprint: id,
						}}, nil, nil))
						if i == 0 {
							seed = knowledge.AnchorHandle(id)
						}
					} else {
						require.NoError(t, store.ReplaceIntelDocSections(ctx, path, []codeanchor.IntelDocSection{{
							SectionID: id, Path: path, Title: id, Level: 1, Content: id, Fingerprint: id,
						}}, nil, nil))
						if i == 0 {
							seed = knowledge.NoteHandle(path)
						}
					}
					require.NoError(t, store.ReplaceIntelChunks(ctx, []string{id}, []codeanchor.IntelChunk{{
						ChunkID: id, OwnerID: id, OwnerType: ownerType, Granularity: "primary", ContentHash: id,
					}}))
					vectors[id] = embeddings.Embedding{1, 0, 0, 0}
				}
				vectors["owner-34"] = embeddings.Embedding{0.8, 0.2, 0, 0}
				require.NoError(t, store.UpsertEmbeddings(ctx, vectors))
				retriever := &SeedVectorRetriever{Semantic: &semantic.Searcher{IntelStore: store}}
				spec := search.QuerySpec{Seeds: []knowledge.Handle{seed}, Limits: search.Limits{Total: 1}}
				unfiltered, err := retriever.Retrieve(ctx, spec)
				require.NoError(t, err)
				require.NotEmpty(t, unfiltered)
				for _, candidate := range unfiltered {
					require.NotEqual(t, targetPath, candidate.Path)
				}
				spec.Filters.TestsOnly = testsOnly
				spec.Filters.ExcludeTests = !testsOnly
				filtered, err := retriever.Retrieve(ctx, spec)
				require.NoError(t, err)
				require.Len(t, filtered, 1)
				require.Equal(t, targetPath, filtered[0].Path)
				if ownerType == "doc_section" {
					spec.Filters.ExactSymbols = []string{"owner-34"}
					filtered, err = retriever.Retrieve(ctx, spec)
					require.NoError(t, err)
					require.Empty(t, filtered)
				}
			})
		}
	}
}
