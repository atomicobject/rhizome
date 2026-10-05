package graphdb

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func TestComputeAnchorPageRank_UsesCallGraph(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := semdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().Unix()

	a1 := codeanchor.IntelAnchor{
		AnchorID:    "a1",
		Lang:        "go",
		Kind:        "function",
		Path:        "pkg/foo/a.go",
		Symbol:      "Foo",
		Fingerprint: "fp-a1",
		StartByte:   0, EndByte: 1, StartLine: 1, EndLine: 1,
		UpdatedAt: now,
	}
	a2 := codeanchor.IntelAnchor{
		AnchorID:    "a2",
		Lang:        "go",
		Kind:        "function",
		Path:        "pkg/qux/d.go", // Different path from bmod so both are preserved
		Symbol:      "Bar",
		Fingerprint: "fp-a2",
		StartByte:   0, EndByte: 1, StartLine: 1, EndLine: 1,
		UpdatedAt: now,
	}
	bmod := codeanchor.IntelAnchor{
		AnchorID:    "bmod",
		Lang:        "go",
		Kind:        "module",
		Path:        "pkg/bar/b.go",
		Symbol:      "b.go",
		Fingerprint: "fp-bmod",
		StartByte:   0, EndByte: 1, StartLine: 1, EndLine: 1,
		UpdatedAt: now,
	}
	cmod := codeanchor.IntelAnchor{
		AnchorID:    "cmod",
		Lang:        "go",
		Kind:        "module",
		Path:        "pkg/baz/c.go",
		Symbol:      "c.go",
		Fingerprint: "fp-cmod",
		StartByte:   0, EndByte: 1, StartLine: 1, EndLine: 1,
		UpdatedAt: now,
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, a1.Path, []codeanchor.IntelAnchor{a1}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, a2.Path, []codeanchor.IntelAnchor{a2}, nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, bmod.Path, []codeanchor.IntelAnchor{bmod}, []codeanchor.IntelEdge{
		{SrcID: "bmod", DstID: "a1", Kind: "calls"},
		{SrcID: "bmod", DstID: "a2", Kind: "calls"},
	}, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, cmod.Path, []codeanchor.IntelAnchor{cmod}, []codeanchor.IntelEdge{
		{SrcID: "cmod", DstID: "a1", Kind: "calls"},
	}, nil))

	scores, err := ComputeAnchorPageRank(ctx, store)
	require.NoError(t, err)
	// All 4 anchors participate in the call graph: 2 callers (bmod, cmod) and 2 callees (a1, a2).
	require.Len(t, scores, 4)

	// Higher PageRank should go to Foo (more inbound from the caller file).
	pr := map[string]float64{}
	for _, sc := range scores {
		pr[sc.AnchorID] = sc.PageRank
	}
	require.Greater(t, pr["a1"], pr["a2"])
	require.NotZero(t, pr["a1"])
	require.NotZero(t, pr["a2"])
}
