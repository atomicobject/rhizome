package planner

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestExpandNoteSeedsFromDocLinks_PreservesAuthoredNoteExtension(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "pkg/main.go", []codeanchor.DocLink{{
		SrcType:   "code",
		SrcPath:   "pkg/main.go",
		DstKind:   "note",
		DstPath:   "docs/Guide.html",
		UpdatedAt: time.Now().Unix(),
	}}))

	got := expandNoteSeedsFromDocLinks(ctx, []knowledge.Handle{knowledge.FileHandle("pkg/main.go")}, store, root, 10)
	require.Equal(t, []knowledge.Handle{knowledge.NoteHandle("docs/Guide.html")}, got)
}
