package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
)

func TestAnchorIDsByPathPrefix_ReturnsNestedPaths(t *testing.T) {
	ctx := context.Background()
	store, err := OpenCode(context.Background(), File(filepath.Join(t.TempDir(), "code.db")), Options{Access: WriteIndex, Dimensions: 8})
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{Provider: "test", Model: "test", Dimensions: 8}))

	require.NoError(t, store.UpsertItemMeta(ctx, codeindex.Item{AnchorID: "a1", Path: "pkg/a.go", Kind: "module"}))
	require.NoError(t, store.UpsertItemMeta(ctx, codeindex.Item{AnchorID: "a2", Path: "pkg/sub/b.go", Kind: "module"}))
	require.NoError(t, store.UpsertItemMeta(ctx, codeindex.Item{AnchorID: "a3", Path: "other/c.go", Kind: "module"}))

	ids, err := store.AnchorIDsByPathPrefix(ctx, "pkg", 10)
	require.NoError(t, err)
	require.Equal(t, []codeindex.AnchorID{"a1", "a2"}, ids)
}
