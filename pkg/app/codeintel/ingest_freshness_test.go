package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/indexingpipe"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestIndexCandidates_ContentHashIsAuthoritativeAcrossMisleadingMtimes(t *testing.T) {
	tests := []struct {
		name    string
		initial time.Time
		changed time.Time
	}{
		{name: "same second nanoseconds", initial: time.Unix(1_700_000_000, 100), changed: time.Unix(1_700_000_000, 900)},
		{name: "exact preserved timestamp", initial: time.Unix(1_700_000_100, 0), changed: time.Unix(1_700_000_100, 0)},
		{name: "backdated content", initial: time.Unix(1_700_000_200, 0), changed: time.Unix(1_699_999_000, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "src", "main.go")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, []byte("package src\nfunc Before() {}\n"), 0o644))
			require.NoError(t, os.Chtimes(path, tt.initial, tt.initial))

			store := openFreshnessTestStore(t, root)
			countingStore := &countingCodePersistenceStore{Store: store}
			indexer := &countingLanguageIndexer{delegate: codeanchor.NewGoIndexer()}
			service := codeanchor.NewServiceWithOptions(countingStore, []codeanchor.LanguageIndexer{indexer}, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
			candidate := indexingpipe.FileCandidate{AbsPath: path, RelPath: "src/main.go", ModTime: tt.initial.Unix(), Kind: indexingpipe.FileKindCode, Lang: codeanchor.LangGo}

			first, err := IndexCandidates(context.Background(), service, root, []indexingpipe.FileCandidate{candidate}, nil)
			require.NoError(t, err)
			require.Equal(t, 1, first.Indexed)

			require.NoError(t, os.WriteFile(path, []byte("package src\nfunc After() {}\n"), 0o644))
			require.NoError(t, os.Chtimes(path, tt.changed, tt.changed))
			candidate.ModTime = tt.changed.Unix()
			changed, err := IndexCandidates(context.Background(), service, root, []indexingpipe.FileCandidate{candidate}, nil)
			require.NoError(t, err)
			require.Equal(t, 1, changed.Indexed)
			require.Zero(t, changed.Unchanged)
			require.Equal(t, int64(2), indexer.calls.Load())
			require.Equal(t, int64(2), countingStore.batches.Load())

			unchanged, err := IndexCandidates(context.Background(), service, root, []indexingpipe.FileCandidate{candidate}, nil)
			require.NoError(t, err)
			require.Zero(t, unchanged.Indexed)
			require.Equal(t, 1, unchanged.Unchanged)
			require.Equal(t, int64(2), indexer.calls.Load(), "unchanged bytes must skip parsing")
			require.Equal(t, int64(2), countingStore.batches.Load(), "unchanged bytes must skip persistence")
		})
	}
}

type countingLanguageIndexer struct {
	delegate codeanchor.LanguageIndexer
	calls    atomic.Int64
}

func (i *countingLanguageIndexer) IndexFile(content []byte, path paths.CodePathRef) (codeanchor.FileSummary, error) {
	i.calls.Add(1)
	return i.delegate.IndexFile(content, path)
}

func (i *countingLanguageIndexer) Lang() codeanchor.Lang { return i.delegate.Lang() }

type countingCodePersistenceStore struct {
	*semdb.Store
	batches atomic.Int64
}

func (s *countingCodePersistenceStore) ApplyCodePersistenceBatch(ctx context.Context, batch codeanchor.CodePersistenceBatch) error {
	s.batches.Add(1)
	return s.Store.ApplyCodePersistenceBatch(ctx, batch)
}

func openFreshnessTestStore(t testing.TB, root string) *semdb.Store {
	t.Helper()
	store, err := semdb.Open(filepath.Join(root, "freshness.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}
