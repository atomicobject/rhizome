package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodeResetDomainPreservingCacheKeepsEmbeddingCache(t *testing.T) {
	ctx := context.Background()
	store, err := OpenCode(context.Background(), File(filepath.Join(t.TempDir(), "reset-code-cache.db")), Options{Access: WriteIndex, Dimensions: 4})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.EnsureSchema(ctx))
	_, err = store.db.ExecContext(ctx, `INSERT INTO `+codeTableEmbeddingCache+` (content_hash, embedding, dimensions, created_at) VALUES (?, ?, ?, ?)`, "h1", []byte{1, 2, 3}, 3, 1)
	require.NoError(t, err)

	require.NoError(t, store.ResetDomainPreservingCache(ctx))

	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+codeTableEmbeddingCache).Scan(&count))
	require.Equal(t, 1, count)

	require.NoError(t, store.ResetDomain(ctx))
	require.NoError(t, store.EnsureSchema(ctx))

	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+codeTableEmbeddingCache).Scan(&count))
	require.Equal(t, 0, count)
}
