package codeanchor_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

type codePublicationCountingIndexer struct {
	codeanchor.LanguageIndexer
	calls atomic.Int64
}

func (a *codePublicationCountingIndexer) IndexFile(content []byte, path paths.CodePathRef) (codeanchor.FileSummary, error) {
	a.calls.Add(1)
	return a.LanguageIndexer.IndexFile(content, path)
}

func TestIndexCodeFileUnchangedWithSharedWriterMutex(t *testing.T) {
	ctx, root, store, _ := codePublicationFixture(t, codeanchor.LangGo)
	var shared sync.Mutex
	store.SetWriteMu(&shared)
	idx := &codePublicationCountingIndexer{LanguageIndexer: codeanchor.NewGoIndexer()}
	counted := &countingCodeBatchStore{Store: store}
	svc := codeanchor.NewServiceWithOptions(counted, []codeanchor.LanguageIndexer{idx}, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache(), codeanchor.WithWriteAccess())
	source := []byte("package audit\nfunc Current(){}\n")
	path := filepath.Join(root, "source.go")
	require.NoError(t, os.WriteFile(path, source, 0644))
	completed := make(chan error, 1)
	go func() { completed <- svc.IndexCodeFile(ctx, codeanchor.LangGo, path, source) }()
	select {
	case err := <-completed:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("public indexing did not complete with shared writer mutex")
	}
	require.EqualValues(t, 1, idx.calls.Load())
	_, err := store.DB().Exec("CREATE TRIGGER unchanged_write BEFORE INSERT ON files BEGIN SELECT RAISE(ABORT,'unchanged must not publish'); END")
	require.NoError(t, err)
	require.NoError(t, svc.IndexCodeFile(ctx, codeanchor.LangGo, path, source))
	require.EqualValues(t, 1, idx.calls.Load())
	require.EqualValues(t, 1, counted.persistenceCalls.Load())
	require.Zero(t, counted.summaryBatchCalls.Load())
	require.Zero(t, counted.fileMetaCalls.Load())
	t.Log("unchanged retry parsed zero additional files and performed zero freshness writes; shared writer mutex completed")
}

func TestIndexCodeFileCancellationDuringSQLRemainsRetryable(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprintf("batch=%v", batch), func(t *testing.T) {
			ctx, root, store, svc := codePublicationFixture(t, codeanchor.LangGo)
			old := []byte("package audit\nfunc Before(){}\n")
			updated := []byte("package audit\nfunc After(){}\n")
			path := filepath.Join(root, "source.go")
			require.NoError(t, publishCodeFile(t, ctx, svc, codeanchor.LangGo, path, old, batch))
			before := codePublicationHash(t, ctx, store, "source.go")
			store.DB().SetMaxOpenConns(1)
			canceled, cancel := context.WithCancel(ctx)
			defer cancel()
			var invoked atomic.Bool
			conn, err := store.DB().Conn(ctx)
			require.NoError(t, err)
			require.NoError(t, conn.Raw(func(raw any) error {
				return raw.(*sqlite3.SQLiteConn).RegisterFunc("audit_cancel", func() int { invoked.Store(true); cancel(); return 0 }, false)
			}))
			require.NoError(t, conn.Close())
			_, err = store.DB().Exec("CREATE TRIGGER audit_cancel_insert BEFORE INSERT ON intel_code_anchors BEGIN SELECT audit_cancel(); END")
			require.NoError(t, err)
			err = publishCodeFile(t, canceled, svc, codeanchor.LangGo, path, updated, batch)
			require.ErrorIs(t, err, context.Canceled)
			require.True(t, invoked.Load())
			after := codePublicationHash(t, ctx, store, "source.go")
			_, err = store.DB().Exec("DROP TRIGGER audit_cancel_insert")
			require.NoError(t, err)
			require.NoError(t, publishCodeFile(t, ctx, svc, codeanchor.LangGo, path, updated, batch))
			anchors, err := store.IntelAnchorsByPath(ctx, "source.go")
			require.NoError(t, err)
			var names []string
			for _, a := range anchors {
				names = append(names, a.Symbol)
			}
			t.Logf("cancellation reached SQL; hashAdvanced=%v retrySymbols=%v", before != after, names)
			require.Equal(t, before, after, "cancelled publication must preserve trusted freshness")
			require.Contains(t, names, "After")
			require.NotContains(t, names, "Before")
		})
	}
}
