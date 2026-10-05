package sqlite

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestTopCodePathsByPageRankPrefix_OrdersByMaxAnchorScore(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/a.go",
		[]codeanchor.IntelAnchor{{AnchorID: "a1", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/a.go", Symbol: "A", Fingerprint: "fp-a"}},
		nil,
		nil,
	))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/b.go",
		[]codeanchor.IntelAnchor{{AnchorID: "b1", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/b.go", Symbol: "B", Fingerprint: "fp-b"}},
		nil,
		nil,
	))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/sub/c.go",
		[]codeanchor.IntelAnchor{{AnchorID: "c1", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/sub/c.go", Symbol: "C", Fingerprint: "fp-c"}},
		nil,
		nil,
	))

	require.NoError(t, store.ReplaceAnchorScores(ctx, []AnchorScore{
		{AnchorID: "a1", PageRank: 0.1, Updated: 1},
		{AnchorID: "b1", PageRank: 0.9, Updated: 1},
		{AnchorID: "c1", PageRank: 0.2, Updated: 1},
	}))

	got, err := store.TopCodePathsByPageRankPrefix(ctx, "pkg", 10)
	require.NoError(t, err)
	require.Equal(t, []string{"pkg/b.go", "pkg/sub/c.go", "pkg/a.go"}, got)
}
