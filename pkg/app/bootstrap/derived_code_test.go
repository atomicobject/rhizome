package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
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

type countingCodeProvider struct {
	*controlledWorkloadProvider
	texts atomic.Int64
}

func (p *countingCodeProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.texts.Add(int64(len(texts)))
	return p.controlledWorkloadProvider.EmbedTexts(ctx, texts)
}

func TestDerivedCodeInterruptedPassReusesEveryReturnedVector(t *testing.T) {
	for _, interrupted := range []string{"cancelled", "superseded"} {
		t.Run(interrupted, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o600))
			w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
			t.Cleanup(func() { c.Close(); store.Close() })
			w.codeCfg.Enabled = true
			w.codeCfg.GoRoots = []string{root}
			provider := &countingCodeProvider{controlledWorkloadProvider: &controlledWorkloadProvider{
				inner:   embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}),
				entered: make(chan struct{}, 1), release: make(chan struct{}),
			}}
			releaseProvider := shutdownRelease(t, provider.release)
			index, err := embsqlite.OpenCode(t.Context(), embsqlite.File(filepath.Join(root, "code.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 8})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, index.Close()) })
			w.codeSyncer = &embedindex.Syncer{Index: index, Provider: provider, Intel: store, Root: root, ChunkWriter: store, EmbeddingWriter: store}
			w.codeSemanticStore = true
			w.retainWatchEvents(nil, true)
			c.MarkDirty("main.go", cache.DirtyModified)
			batch := w.processOwnershipBatch()
			watcherJobTerminal(t, batch)
			require.NoError(t, batch.Err())
			work, err := store.PendingDerivedWork(t.Context(), time.Now(), 100)
			require.NoError(t, err)
			var ticket codeanchor.DerivedWork
			for _, candidate := range work {
				if candidate.Kind == codeanchor.DerivedCode {
					ticket = candidate
				}
			}
			require.Equal(t, codeanchor.DerivedCode, ticket.Kind)
			files, err := store.IndexedFilePaths(t.Context())
			require.NoError(t, err)
			require.Contains(t, files, "main.go")
			provider.block.Store(true)
			d := &derivedScheduler{watcher: w, ctx: t.Context()}
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			done := make(chan error, 1)
			go func() { done <- d.executeObserved(ctx, ticket) }()
			select {
			case <-provider.entered:
			case err := <-done:
				t.Fatalf("code pass finished before contacting the provider: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("code provider did not start")
			}
			retry := ticket
			if interrupted == "cancelled" {
				cancel()
			} else {
				require.NoError(t, d.inLane(t.Context(), "derived_prepare", func(ctx context.Context) error {
					next, err := store.MarkDerivedDirty(ctx, []codeanchor.DerivedScope{ticket.DerivedScope})
					if err != nil {
						return err
					}
					retry = next[0]
					return store.ActivateDerivedWork(ctx, next)
				}))
			}
			releaseProvider()
			if interrupted == "cancelled" {
				require.ErrorIs(t, <-done, context.Canceled)
			} else {
				require.ErrorIs(t, <-done, errDerivedSuperseded)
			}
			require.Zero(t, provider.canceled.Load(), "in-flight calls finish after a pass is cancelled")
			sent := provider.texts.Load()
			require.Positive(t, sent)
			require.NoError(t, d.execute(retry))
			require.Equal(t, sent, provider.texts.Load(), "retry must send zero already embedded texts")
			var published int
			require.NoError(t, store.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM intel_embeddings`).Scan(&published))
			require.Positive(t, published)
			pending, err := store.HasDerivedWork(t.Context(), codeanchor.DerivedCode)
			require.NoError(t, err)
			require.False(t, pending)
		})
	}
}

func TestDerivedNoteEditEmbedsBeforeGlobalCodeTicket(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\nOriginal content.\n"), 0o600))
	for i := range 500 {
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("file%d.go", i)), []byte(fmt.Sprintf("package example\nfunc F%d() {}\n", i)), 0o600))
	}
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	t.Cleanup(func() { c.Close(); store.Close() })
	w.codeCfg.Enabled = true
	w.codeCfg.GoRoots = []string{root}
	w.retainWatchEvents(nil, true)
	c.MarkDirty("note.md", cache.DirtyModified)
	batch := w.processOwnershipBatch()
	watcherJobTerminal(t, batch)
	require.NoError(t, batch.Err())
	files, err := store.IndexedFilePaths(t.Context())
	require.NoError(t, err)
	require.Len(t, files, 500)
	work, err := store.PendingDerivedWork(t.Context(), time.Now(), 1000)
	require.NoError(t, err)
	require.NoError(t, store.AckDerivedWork(t.Context(), work))
	index, err := embsqlite.OpenNotes(t.Context(), embsqlite.File(filepath.Join(root, "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 8})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, index.Close()) })
	provider := &controlledWorkloadProvider{inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8}), entered: make(chan struct{}, 1), release: make(chan struct{})}
	releaseProvider := shutdownRelease(t, provider.release)
	w.noteSyncer = &embedindex.NoteSyncer{Index: index, Provider: provider, Intel: store, ChunkWriter: store, EmbeddingWriter: store, EmbeddingCache: store}
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\nEdited content.\n"), 0o600))
	c.MarkDirty("note.md", cache.DirtyModified)
	batch = w.processOwnershipBatch()
	watcherJobTerminal(t, batch)
	require.NoError(t, batch.Err())
	work, err = store.PendingDerivedWork(t.Context(), time.Now(), 1000)
	require.NoError(t, err)
	require.NotEmpty(t, work)
	require.Equal(t, codeanchor.DerivedNotes, work[0].Kind)
	provider.block.Store(true)
	d := newDerivedScheduler(w)
	t.Cleanup(d.close)
	t.Cleanup(releaseProvider)
	d.notify()
	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the note's provider call did not start")
	}
	d.mu.Lock()
	running := d.running
	d.mu.Unlock()
	require.Equal(t, codeanchor.DerivedNotes, running)
	current, err := store.CurrentDerivedWork(t.Context(), work[0])
	require.NoError(t, err)
	require.True(t, current)
	var codeTickets int
	require.NoError(t, store.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM derived_work WHERE kind='code' AND path=''`).Scan(&codeTickets))
	require.Equal(t, 1, codeTickets)
	releaseProvider()
	require.Eventually(t, func() bool {
		var vectors int
		err := store.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM intel_embeddings`).Scan(&vectors)
		return err == nil && vectors > 0
	}, 5*time.Second, 5*time.Millisecond)
}
