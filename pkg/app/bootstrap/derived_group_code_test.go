package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/embedindex"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestDerivedGroupCodeDropsOnlyChangedPathAndCachesIt(t *testing.T) {
	root := t.TempDir()
	write := func(i, n int) {
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("file%d.go", i)), []byte(fmt.Sprintf("package example\nfunc Value%d()int{return %d}\n", i, n)), 0600))
	}
	for i := range 3 {
		write(i, 1)
	}
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	defer c.Close()
	defer store.Close()
	w.codeCfg.Enabled = true
	w.codeCfg.GoRoots = []string{root}
	info := embeddings.ProviderConfig{Provider: "test", Model: "test", Dimensions: 8}
	p := &groupedProvider{Provider: embeddings.NewDeterministicProvider(info)}
	index, err := embsqlite.OpenCode(t.Context(), embsqlite.File(filepath.Join(root, "code.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 8})
	require.NoError(t, err)
	defer index.Close()
	w.codeSyncer = &embedindex.Syncer{Index: index, Provider: p, ProviderInfo: info, Intel: store, Root: root, ChunkWriter: store, EmbeddingWriter: store}
	w.retainWatchEvents(nil, true)
	require.Eventually(t, func() bool {
		h := w.processOwnershipBatch()
		watcherJobTerminal(t, h)
		require.NoError(t, h.Err())
		paths, err := store.IndexedFilePaths(t.Context())
		require.NoError(t, err)
		return len(paths) == 3
	}, 5*time.Second, 5*time.Millisecond)
	d := &derivedScheduler{watcher: w, ctx: t.Context()}
	work, err := store.PendingDerivedWork(t.Context(), time.Now(), 100)
	require.NoError(t, err)
	for _, ticket := range work {
		require.NoError(t, d.execute(ticket))
	}
	for i := range 3 {
		write(i, 10)
		c.MarkDirty(fmt.Sprintf("file%d.go", i), cache.DirtyModified)
	}
	h := w.processOwnershipBatch()
	watcherJobTerminal(t, h)
	require.NoError(t, h.Err())
	group, err := store.PendingDerivedWorkForKind(t.Context(), codeanchor.DerivedCode, time.Now(), 100)
	require.NoError(t, err)
	require.Len(t, group, 3)
	p.batches = nil
	p.before = func() { write(0, 100) }
	require.NoError(t, d.execute(group...))
	pending, err := store.PendingDerivedWorkForKind(t.Context(), codeanchor.DerivedCode, time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "file0.go", pending[0].Path)
	require.Zero(t, pending[0].Attempt)
	_, sent := p.counts()
	require.Positive(t, sent)
	write(0, 10)
	require.NoError(t, d.execute(pending...))
	_, after := p.counts()
	require.Equal(t, sent, after)
}
