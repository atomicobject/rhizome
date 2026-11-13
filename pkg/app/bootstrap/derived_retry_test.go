package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embedindex"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type interruptedDerivedLane struct {
	lane.Lane
	at, submits int
	err         error
}

func (l *interruptedDerivedLane) Submit(ctx context.Context, req lane.Request) (lane.Handle, bool, error) {
	l.submits++
	if l.submits == l.at {
		req.Run = func(context.Context, *indexlock.Lease, lane.Reporter) error { return l.err }
	}
	return l.Lane.Submit(ctx, req)
}

func TestDerivedRetryCountsFailuresButNotPrepareOrPublicationInterruptions(t *testing.T) {
	for _, phase := range []struct {
		name string
		at   int
	}{{"prepare", 1}, {"publication", 2}} {
		for _, outcome := range []struct {
			name    string
			err     error
			attempt int
		}{{"yield", context.Canceled, 0}, {"preempt", lane.ErrPreempted, 0}, {"failure", errors.New("synthetic failure"), 1}} {
			t.Run(phase.name+"/"+outcome.name, func(t *testing.T) {
				s, _, _ := derivedPublicationFixture(t)
				w := s.watcher
				w.lane = &interruptedDerivedLane{Lane: w.lane, at: phase.at, err: outcome.err}
				d := newDerivedScheduler(w)
				defer d.close()
				d.notify()
				var attempt int
				var retryAt int64
				require.Eventually(t, func() bool {
					err := w.intelStore.DB().QueryRowContext(t.Context(), `SELECT attempt,retry_at FROM derived_work WHERE kind='code'`).Scan(&attempt, &retryAt)
					return err == nil && retryAt > 0
				}, 3*time.Second, 5*time.Millisecond)
				require.Equal(t, outcome.attempt, attempt)
			})
		}
	}
}

func TestDerivedOntologyWithoutSchemaDrains(t *testing.T) {
	root := t.TempDir()
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	t.Cleanup(func() { c.Close(); store.Close() })
	provider := &recordingEmbeddingProvider{}
	w.nodeSyncer = &embedindex.OntologyNodeSyncer{Store: store, Provider: provider}
	for _, path := range []string{"", "note.md"} {
		tickets, err := store.MarkDerivedDirty(t.Context(), []codeanchor.DerivedScope{{Kind: codeanchor.DerivedOntology, Path: path}})
		require.NoError(t, err)
		require.NoError(t, store.ActivateDerivedWork(t.Context(), tickets))
		d := &derivedScheduler{watcher: w, ctx: t.Context()}
		require.NoError(t, d.execute(tickets[0]))
	}
	pending, err := store.HasDerivedWork(t.Context(), codeanchor.DerivedOntology)
	require.NoError(t, err)
	require.False(t, pending)
	require.Empty(t, provider.texts)
}

func TestDerivedSchedulerDelaysUnseenEditWithoutFailedAttempt(t *testing.T) {
	root := t.TempDir()
	writeWatcherSchema(t, root, "type Project @node(default: true) { name: String }\n")
	path := "note.md"
	require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("# Project\nname:: First\n"), 0o600))
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	t.Cleanup(func() { c.Close(); store.Close() })
	provider := &recordingEmbeddingProvider{}
	w.nodeSyncer = &embedindex.OntologyNodeSyncer{Store: store, Provider: provider, ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "recording"}}
	c.MarkDirty(path, cache.DirtyModified)
	batch := w.processOwnershipBatch()
	watcherJobTerminal(t, batch)
	require.NoError(t, batch.Err())
	work, err := store.PendingDerivedWork(t.Context(), time.Now(), 100)
	require.NoError(t, err)
	var ticket codeanchor.DerivedWork
	for _, candidate := range work {
		if candidate.Kind == codeanchor.DerivedOntology {
			ticket = candidate
		} else {
			require.NoError(t, store.AckDerivedWork(t.Context(), []codeanchor.DerivedWork{candidate}))
		}
	}
	require.Equal(t, codeanchor.DerivedOntology, ticket.Kind)
	// The disk edit has not reached structural publication yet.
	require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("# Project\nname:: Second\n"), 0o600))
	counted := &publicationLane{Lane: w.lane}
	w.lane = counted
	d := newDerivedScheduler(w)
	t.Cleanup(d.close)
	d.notify()
	var attempt int
	var retryAt int64
	require.Eventually(t, func() bool {
		err := store.DB().QueryRowContext(t.Context(), `SELECT attempt,retry_at FROM derived_work WHERE kind='ontology'`).Scan(&attempt, &retryAt)
		return err == nil && retryAt > 0
	}, 3*time.Second, 5*time.Millisecond)
	require.Zero(t, attempt)
	require.LessOrEqual(t, counted.submits.Load(), int64(3), "one witness, one preparation, and one delay write")
	for range 10 {
		ready, err := store.PendingDerivedWork(t.Context(), time.UnixMilli(retryAt-1), 32)
		require.NoError(t, err)
		require.Empty(t, ready, "the delayed ticket cannot spin on scheduler ticks")
	}
	require.Empty(t, provider.texts)
	d.close()
	// Publication of the next source makes the ticket immediately runnable.
	c.MarkDirty(path, cache.DirtyModified)
	batch = w.processOwnershipBatch()
	watcherJobTerminal(t, batch)
	require.NoError(t, batch.Err())
	ready, err := store.PendingDerivedWork(context.Background(), time.Now(), 32)
	require.NoError(t, err)
	require.NotEmpty(t, ready)
}
