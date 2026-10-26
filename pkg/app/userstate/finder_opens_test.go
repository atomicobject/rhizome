package userstate

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFinderOpensCountAndSurviveReopen(t *testing.T) {
	t.Parallel()
	ctx, root := context.Background(), t.TempDir()
	at := time.UnixMilli(1_760_000_000_000)
	store, err := Open(ctx, root)
	require.NoError(t, err)
	require.NoError(t, store.RecordFinderOpen(ctx, "docs/a.md", at))
	require.NoError(t, store.RecordFinderOpen(ctx, "docs/a.md", at.Add(time.Minute)))
	require.NoError(t, store.RecordFinderOpen(ctx, "pkg/b.go", at))
	for _, bad := range []string{"", "/abs.md", "../escape.md", "a/../b.md", `a\b.md`, "./a.md"} {
		require.ErrorIs(t, store.RecordFinderOpen(ctx, bad, at), ErrInvalid, bad)
	}
	require.NoError(t, store.Close())

	store = openTestStore(t, root)
	opens, err := store.FinderOpens(ctx)
	require.NoError(t, err)
	require.Equal(t, []FinderOpen{
		{Path: "docs/a.md", Count: 2, LastOpened: at.Add(time.Minute)},
		{Path: "pkg/b.go", Count: 1, LastOpened: at},
	}, opens)
}
