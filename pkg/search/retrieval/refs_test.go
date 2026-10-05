package retrieval

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestDocLinksRetriever_DedupIsPerKind(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "doc-links.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	now := time.Now().Unix()
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "src/alpha.go", []codeanchor.DocLink{{
		SrcType:   "code",
		SrcPath:   "src/alpha.go",
		DstKind:   "note",
		DstPath:   "notes/shared.MD",
		Snippet:   "alpha",
		UpdatedAt: now,
	}}))
	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "notes/shared.MD", []codeanchor.DocLink{{
		SrcType:   "code",
		SrcPath:   "notes/shared.MD",
		DstKind:   "note",
		DstPath:   "notes/other.html",
		Snippet:   "beta",
		UpdatedAt: now,
	}}))

	retriever := DocLinksRetriever{Store: store, Limit: 10}
	spec := search.QuerySpec{
		Seeds: []knowledge.Handle{
			knowledge.NoteHandle("notes/shared.MD"),
			knowledge.FileHandle("notes/shared.MD"),
		},
	}
	results, err := retriever.Retrieve(ctx, spec)
	require.NoError(t, err)

	var hasFile bool
	var hasNote bool
	for _, res := range results {
		if res.Type == "file" && res.Path == "src/alpha.go" {
			hasFile = true
		}
		if res.Type == "note" && res.Path == "notes/other.html" {
			hasNote = true
		}
	}
	require.True(t, hasFile, "expected file result from note seed")
	require.True(t, hasNote, "expected note result from file seed")
}
