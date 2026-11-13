package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embedindex"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/testutil/indexingworkload"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestDerivedGroupRejectsDirectSaveForOnlyItsMember(t *testing.T) {
	w, p, work := groupedWatcher(t, true, 3)
	group := groupKind(work, codeanchor.DerivedOntology)
	schema, err := ontoschema.LoadSchema(w.vaultPath)
	require.NoError(t, err)
	path := group[0].Path
	p.before = func() {
		require.NoError(t, os.WriteFile(filepath.Join(w.vaultPath, path), []byte(indexingworkload.Note(0, 3, "Direct save")), 0600))
		publishDerivedTestSave(t, w, schema, path)
	}
	d := &derivedScheduler{watcher: w, ctx: t.Context()}
	require.NoError(t, d.execute(group...))
	pending, err := w.intelStore.PendingDerivedWork(t.Context(), time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	pending = groupKind(pending, codeanchor.DerivedOntology)
	require.Len(t, pending, 1)
	require.Equal(t, path, pending[0].Path)
	var current int
	require.NoError(t, w.intelStore.DB().QueryRowContext(t.Context(), `SELECT count(DISTINCT s.note_path) FROM ontology_node_embedding_state s JOIN ontology_nodes n ON n.node_id=s.node_id JOIN intel_embeddings e ON e.chunk_id=s.chunk_id WHERE s.node_structure_fingerprint=n.structural_fingerprint`).Scan(&current))
	require.Equal(t, 2, current)
	calls, texts := p.counts()
	require.Equal(t, 1, calls)
	require.Equal(t, 3, texts)
}

func TestDerivedGroupRestartReusesCancelledVectors(t *testing.T) {
	w, p, work := groupedWatcher(t, true, 50)
	group := groupKind(work, codeanchor.DerivedOntology)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p.before = cancel
	d := &derivedScheduler{watcher: w, ctx: ctx}
	require.ErrorIs(t, d.execute(group...), context.Canceled)
	_, sent := p.counts()
	require.Equal(t, 50, sent)
	reopened, err := semdb.Open(filepath.Join(w.vaultPath, ".rhizome/db.sqlite"))
	require.NoError(t, err)
	defer reopened.Close()
	next := &unifiedSemanticWatcher{runCtx: t.Context(), lane: w.lane, vaultPath: w.vaultPath, vaultDef: w.vaultDef, noteMetadataIndexer: w.noteMetadataIndexer, intelStore: reopened,
		nodeSyncer: &embedindex.OntologyNodeSyncer{Store: reopened, Provider: p, ProviderInfo: w.nodeSyncer.ProviderInfo, EmbeddingNode: w.nodeSyncer.EmbeddingNode}}
	restarted := newDerivedScheduler(next)
	defer restarted.close()
	restarted.notify()
	require.Eventually(t, func() bool {
		var pending int
		err := reopened.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM derived_work`).Scan(&pending)
		return err == nil && pending == 0
	}, 10*time.Second, 5*time.Millisecond)
	restarted.close()
	_, after := p.counts()
	require.Equal(t, sent, after)
	var current int
	require.NoError(t, reopened.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM ontology_node_embedding_state s JOIN ontology_nodes n ON n.node_id=s.node_id JOIN intel_embeddings e ON e.chunk_id=s.chunk_id WHERE s.node_structure_fingerprint=n.structural_fingerprint`).Scan(&current))
	require.Equal(t, 50, current)
}

func TestDerivedGroupFailureCachesSuccessfulSlices(t *testing.T) {
	w, _, work := groupedWatcher(t, true, 3)
	provider := &failingTextProvider{failOn: "Project 0001 Changed"}
	w.nodeSyncer.Provider = provider
	w.nodeSyncer.EmbeddingNode = nil
	w.nodeSyncer.BatchSize = 1
	w.nodeSyncer.MaxConcurrent = 3
	group := groupKind(work, codeanchor.DerivedOntology)
	d := &derivedScheduler{watcher: w, ctx: t.Context()}
	require.ErrorContains(t, d.execute(group...), "provider outage")
	require.Len(t, provider.texts, 3)
	var current int
	require.NoError(t, w.intelStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM ontology_node_embedding_state s JOIN ontology_nodes n ON n.node_id=s.node_id WHERE s.node_structure_fingerprint=n.structural_fingerprint`).Scan(&current))
	require.Zero(t, current)
	provider.failOn = ""
	require.NoError(t, d.execute(group...))
	require.Len(t, provider.texts, 4)
	require.NoError(t, w.intelStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM ontology_node_embedding_state s JOIN ontology_nodes n ON n.node_id=s.node_id JOIN intel_embeddings e ON e.chunk_id=s.chunk_id WHERE s.node_structure_fingerprint=n.structural_fingerprint`).Scan(&current))
	require.Equal(t, 3, current)
}

func TestDerivedGroupRecordHostHoldsOnlyItsConsumedSet(t *testing.T) {
	root := t.TempDir()
	writeWatcherSchema(t, root, `type Host @node(default:true){status:String}
 type Entry @node(locator:EMBEDDED) @source(shape:RECORD,paths:["entries/*.yaml"],select:"entries",key:null,title:["$key"],space:"test",logicalKind:"entry"){name:String @field(path:"name")}
 `)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "entries"), 0755))
	write := func(path, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(body), 0600))
	}
	write("entries/a.yaml", "entries: [{name: alpha}]\n")
	write("entries/b.yaml", "entries: [{name: beta}]\n")
	write("note.md", "# Note\nFirst independent text.\n")
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Root: root, Includes: []string{"entries/*.yaml", "*.md"}})
	defer c.Close()
	defer store.Close()
	info := embeddings.ProviderConfig{Provider: "test", Model: "test", Dimensions: 8}
	p := &groupedProvider{Provider: embeddings.NewDeterministicProvider(info)}
	node := embedindex.NewSharedEmbeddingNode(t.Context(), p, 1, nil, embedindex.EmbedPackerOptions{})
	defer node.Close()
	w.nodeSyncer = &embedindex.OntologyNodeSyncer{Store: store, Provider: p, ProviderInfo: info, EmbeddingNode: node}
	w.retainWatchEvents(nil, true)
	require.Eventually(t, func() bool {
		h := w.processOwnershipBatch()
		watcherJobTerminal(t, h)
		require.NoError(t, h.Err())
		paths, err := store.NotePaths(t.Context())
		require.NoError(t, err)
		return len(paths) == 3
	}, 5*time.Second, 5*time.Millisecond)
	d := &derivedScheduler{watcher: w, ctx: t.Context()}
	work, err := store.PendingDerivedWork(t.Context(), time.Now(), 100)
	require.NoError(t, err)
	for _, ticket := range work {
		require.NoError(t, d.execute(ticket))
	}
	write("entries/a.yaml", "entries: [{name: alpha}, {name: gamma}]\n")
	write("note.md", "# Note\nChanged independent text.\n")
	for _, path := range []string{"entries/a.yaml", "entries/b.yaml", "note.md"} {
		c.MarkDirty(path, cache.DirtyModified)
	}
	h := w.processOwnershipBatch()
	watcherJobTerminal(t, h)
	require.NoError(t, h.Err())
	work, err = store.PendingDerivedWork(t.Context(), time.Now(), 100)
	require.NoError(t, err)
	group := groupKind(work, codeanchor.DerivedOntology)
	require.Len(t, group, 3)
	p.mu.Lock()
	p.batches = nil
	p.before = func() { write("entries/b.yaml", "entries: [{name: ahead}]\n") }
	p.mu.Unlock()
	require.NoError(t, d.execute(group...))
	pending, err := store.PendingDerivedWork(t.Context(), time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	pending = groupKind(pending, codeanchor.DerivedOntology)
	require.Len(t, pending, 2)
	for _, ticket := range pending {
		require.Contains(t, ticket.Path, "entries/")
		require.Zero(t, ticket.Attempt)
	}
	calls, sent := p.counts()
	require.Equal(t, 1, calls)
	require.Positive(t, sent)
	write("entries/b.yaml", "entries: [{name: beta}]\n")
	require.NoError(t, d.execute(pending...))
	_, after := p.counts()
	require.Equal(t, sent, after)
	var current int
	require.NoError(t, store.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM ontology_node_embedding_state s JOIN ontology_nodes n ON n.node_id=s.node_id JOIN intel_embeddings e ON e.chunk_id=s.chunk_id WHERE s.node_structure_fingerprint=n.structural_fingerprint`).Scan(&current))
	require.Positive(t, current, fmt.Sprint(pending))
}
