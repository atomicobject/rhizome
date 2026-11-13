package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	embsqlite "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/embedindex"
	"github.com/atomicobject/rhizome/pkg/testutil/indexingworkload"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type groupedProvider struct {
	embeddings.Provider
	mu      sync.Mutex
	batches [][]string
	fail    error
	before  func()
}

func (*groupedProvider) DefaultBatchSize() int     { return 1000 }
func (*groupedProvider) DefaultMaxBatchBytes() int { return 4 << 20 }
func (p *groupedProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.mu.Lock()
	p.batches = append(p.batches, slices.Clone(texts))
	before := p.before
	p.before = nil
	err := p.fail
	p.mu.Unlock()
	if before != nil {
		before()
	}
	if err != nil {
		return nil, err
	}
	return p.Provider.EmbedTexts(ctx, texts)
}
func (p *groupedProvider) counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	texts := 0
	for _, batch := range p.batches {
		texts += len(batch)
	}
	return len(p.batches), texts
}

func groupedWatcher(t *testing.T, typed bool, n int) (*unifiedSemanticWatcher, *groupedProvider, []codeanchor.DerivedWork) {
	t.Helper()
	root := t.TempDir()
	indexingworkload.Write(t, root, n, 0)
	if !typed {
		require.NoError(t, os.Remove(filepath.Join(root, ".rhizome/ontology/schema.graphql")))
	}
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}})
	t.Cleanup(func() { c.Close(); store.Close() })
	w.runCtx = t.Context()
	info := embeddings.ProviderConfig{Provider: "test", Model: "test", Dimensions: 8}
	p := &groupedProvider{Provider: embeddings.NewDeterministicProvider(info)}
	opts := embedindex.PolicyFor(embedindex.IndexRequest{Source: embedindex.IndexSourceWatcher}).NoteEmbedPacker
	node := embedindex.NewSharedEmbeddingNode(t.Context(), p, 1, nil, *opts)
	t.Cleanup(node.Close)
	if typed {
		w.nodeSyncer = &embedindex.OntologyNodeSyncer{Store: store, Provider: p, ProviderInfo: info, EmbeddingNode: node}
	} else {
		index, err := embsqlite.OpenNotes(t.Context(), embsqlite.File(filepath.Join(root, "notes.db")), embsqlite.Options{Access: embsqlite.WriteIndex, Dimensions: 8})
		require.NoError(t, err)
		t.Cleanup(func() { index.Close() })
		w.noteSyncer = &embedindex.NoteSyncer{Index: index, Provider: p, ProviderInfo: info, Intel: store, ChunkWriter: store, EmbeddingWriter: store, EmbeddingCache: store, EmbeddingNode: node, NoteEmbedPacker: opts}
	}
	w.retainWatchEvents(nil, true)
	require.Eventually(t, func() bool {
		h := w.processOwnershipBatch()
		watcherJobTerminal(t, h)
		require.NoError(t, h.Err())
		paths, err := store.NotePaths(t.Context())
		require.NoError(t, err)
		return len(paths) == n
	}, 10*time.Second, 5*time.Millisecond)
	// Finish initial global work before pinning all changed paths into one batch.
	d := &derivedScheduler{watcher: w, ctx: t.Context()}
	work, err := store.PendingDerivedWork(t.Context(), time.Now(), 10000)
	require.NoError(t, err)
	for _, ticket := range work {
		require.NoError(t, d.execute(ticket))
	}
	for i := range n {
		path := fmt.Sprintf("notes/project-%04d.md", i)
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(indexingworkload.Note(i, n, "Changed")), 0600))
		c.MarkDirty(path, cache.DirtyModified)
	}
	h := w.processOwnershipBatch()
	watcherJobTerminal(t, h)
	require.NoError(t, h.Err())
	work, err = store.PendingDerivedWork(t.Context(), time.Now(), 10000)
	require.NoError(t, err)
	p.mu.Lock()
	p.batches = nil
	p.mu.Unlock()
	return w, p, work
}

func groupKind(work []codeanchor.DerivedWork, kind codeanchor.DerivedKind) []codeanchor.DerivedWork {
	return slices.DeleteFunc(slices.Clone(work), func(ticket codeanchor.DerivedWork) bool { return ticket.Kind != kind })
}

func TestDerivedGroupFiftyChangedNotesDrainByCounts(t *testing.T) {
	for _, typed := range []bool{true, false} {
		t.Run(fmt.Sprint(typed), func(t *testing.T) {
			w, p, work := groupedWatcher(t, typed, 50)
			kind := codeanchor.DerivedNotes
			if typed {
				kind = codeanchor.DerivedOntology
			}
			require.Len(t, groupKind(work, kind), 50)
			counted := &publicationLane{Lane: w.lane}
			w.lane = counted
			var events []map[string]any
			w.publishEvent = func(_ string, data any) { events = append(events, data.(map[string]any)) }
			d := newDerivedScheduler(w)
			defer d.close()
			d.notify()
			require.Eventually(t, func() bool {
				var pending int
				err := w.intelStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM derived_work`).Scan(&pending)
				return err == nil && pending == 0
			}, 10*time.Second, 5*time.Millisecond)
			d.close()
			calls, texts := p.counts()
			require.Positive(t, texts)
			require.LessOrEqual(t, calls, 2)
			require.LessOrEqual(t, counted.submits.Load()+1, int64(20), "include the watcher batch completed before the counter was installed")
			matched := false
			for _, event := range events {
				domains := event["domains"].([]string)
				if slices.Contains(domains, string(kind)) {
					require.Len(t, event["paths"], 50)
					require.Equal(t, 50, event["count"])
					require.False(t, matched)
					matched = true
				}
			}
			require.True(t, matched)
			var paths int
			if typed {
				require.NoError(t, w.intelStore.DB().QueryRowContext(t.Context(), `SELECT count(DISTINCT s.note_path) FROM ontology_node_embedding_state s JOIN ontology_nodes n ON n.node_id=s.node_id JOIN intel_embeddings e ON e.chunk_id=s.chunk_id WHERE s.node_structure_fingerprint=n.structural_fingerprint`).Scan(&paths))
			} else {
				require.NoError(t, w.intelStore.DB().QueryRowContext(t.Context(), `SELECT count(DISTINCT d.path) FROM intel_chunks c JOIN intel_doc_sections d ON d.section_id=c.owner_id JOIN intel_embeddings e ON e.chunk_id=c.chunk_id`).Scan(&paths))
			}
			require.Equal(t, 50, paths)
		})
	}
}

func TestDerivedGroupDropsOnlyChangedMembersAndReusesVectors(t *testing.T) {
	for _, typed := range []bool{true, false} {
		t.Run(fmt.Sprint(typed), func(t *testing.T) {
			w, p, work := groupedWatcher(t, typed, 3)
			kind := codeanchor.DerivedNotes
			if typed {
				kind = codeanchor.DerivedOntology
			}
			group := groupKind(work, kind)
			require.Len(t, group, 3)
			path := group[0].Path
			p.before = func() {
				require.NoError(t, os.WriteFile(filepath.Join(w.vaultPath, path), []byte(indexingworkload.Note(0, 3, "Ahead on disk")), 0600))
			}
			d := &derivedScheduler{watcher: w, ctx: t.Context()}
			require.NoError(t, d.execute(group...))
			pending, err := w.intelStore.PendingDerivedWork(t.Context(), time.Now().Add(time.Hour), 100)
			require.NoError(t, err)
			pending = groupKind(pending, kind)
			require.Len(t, pending, 1)
			require.Equal(t, path, pending[0].Path)
			require.Zero(t, pending[0].Attempt)
			require.False(t, pending[0].RetryAt.IsZero())
			_, sent := p.counts()
			require.Positive(t, sent)
			// Restore the exact planned text. The retained vectors satisfy the retry.
			require.NoError(t, os.WriteFile(filepath.Join(w.vaultPath, path), []byte(indexingworkload.Note(0, 3, "Changed")), 0600))
			require.NoError(t, d.execute(pending...))
			_, after := p.counts()
			require.Equal(t, sent, after)
		})
	}
}

func TestDerivedGroupOutageBacklogRemainsGroupable(t *testing.T) {
	w, p, work := groupedWatcher(t, true, 50)
	group := groupKind(work, codeanchor.DerivedOntology)
	// Simulate the durable outcome of the earlier per-path scheduler failing all
	// tickets while offline, including different attempt counts.
	for i, ticket := range group {
		for range i%3 + 1 {
			require.NoError(t, w.intelStore.RetryDerivedWork(t.Context(), ticket, time.Now().Add(-time.Second), true))
		}
	}
	d := newDerivedScheduler(w)
	defer d.close()
	d.notify()
	require.Eventually(t, func() bool {
		pending, err := w.intelStore.HasDerivedWork(t.Context(), codeanchor.DerivedOntology)
		return err == nil && !pending
	}, 10*time.Second, 5*time.Millisecond)
	d.close()
	calls, texts := p.counts()
	require.Equal(t, 50, texts)
	require.LessOrEqual(t, calls, 2)
}

func TestDerivedGroupInputFailureIsolatesNextAttempt(t *testing.T) {
	for _, input := range []bool{true, false} {
		t.Run(fmt.Sprint(input), func(t *testing.T) {
			w, p, _ := groupedWatcher(t, true, 3)
			p.fail = errors.New("synthetic outage")
			if input {
				p.fail = embeddings.ErrInvalidInput
			}
			d := newDerivedScheduler(w)
			defer d.close()
			d.notify()
			require.Eventually(t, func() bool {
				var attempts int
				err := w.intelStore.DB().QueryRowContext(t.Context(), `SELECT min(attempt) FROM derived_work WHERE kind='ontology'`).Scan(&attempts)
				return err == nil && attempts == 1
			}, 3*time.Second, 5*time.Millisecond)
			d.close()
			require.Len(t, d.single, map[bool]int{true: 3, false: 0}[input])
			p.mu.Lock()
			p.fail = nil
			p.batches = nil
			p.mu.Unlock()
			require.NoError(t, d.inLane(t.Context(), "derived_retry", func(ctx context.Context) error {
				_, err := w.intelStore.DB().ExecContext(ctx, `UPDATE derived_work SET retry_at=0`)
				return err
			}))
			// Resume the same scheduler so the next attempt retains input isolation.
			d.ctx, d.cancel = context.WithCancel(t.Context())
			d.done = make(chan struct{})
			go d.run()
			d.notify()
			require.Eventually(t, func() bool {
				pending, err := w.intelStore.HasDerivedWork(t.Context(), codeanchor.DerivedOntology)
				return err == nil && !pending
			}, 5*time.Second, 5*time.Millisecond)
			d.close()
			calls, texts := p.counts()
			require.Equal(t, 3, texts)
			if input {
				require.Equal(t, 3, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}
