package indexcore_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	"github.com/atomicobject/rhizome/pkg/app/indexcore"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	sqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	lease   *indexlock.Lease
	root    string
	request indexcore.Request
	store   *sqlite.Store
	service *anchors.Service
	writer  *indexwriter.Writer
}

func newFixture(t *testing.T) *fixture {
	return newFixtureWithContext(t, context.Background())
}

func newFixtureWithContext(t *testing.T, ctx context.Context) *fixture {
	t.Helper()
	root := t.TempDir()
	lease, acquired, err := indexlock.TryAcquireLease(root, indexlock.AcquireOptions{Role: "test/structural"})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "source.md"), []byte("# Source\nSee [[target]].\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "target.md"), []byte("# Target\nSee [[source]].\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "untouched.md"), []byte("# Untouched\n"), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte("type Record @node(paths: [\"notes/*.md\"]) { title: String }\n"), 0600))
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, markdown.New())
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	cfg := anchors.DefaultConfig(root)
	store, cleanup, err := indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, Config: &cfg, Create: true})
	require.NoError(t, err)
	require.NotNil(t, store)
	t.Cleanup(cleanup)
	service, _ := codeintel.NewCodeAnchorService(codeintel.CodeAnchorServiceConfig{VaultPath: root, CodeCfg: cfg, Store: store, IncludeIndexers: true, WriteAccess: true})
	require.NotNil(t, service)
	writer := indexwriter.New(ctx, indexcore.BindWriterHandlers(indexwriter.Handlers{}, service, store))
	t.Cleanup(func() { _ = writer.Close() })
	return &fixture{lease: lease, root: root, request: indexcore.Request{VaultDefinition: obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}, Links: obsidian.LinkTypeBoth}, CodeConfig: cfg, NoteMetadata: indexer}, store: store, service: service, writer: writer}
}
func (f *fixture) publish(t *testing.T, request indexcore.Request) indexcore.Result {
	t.Helper()
	ctx := context.Background()
	discovery, err := indexcore.Discover(ctx, request, f.store)
	require.NoError(t, err)
	result, err := indexcore.Publish(ctx, f.lease, request, discovery, f.service, f.store, f.writer, indexcore.PublishOptions{})
	require.NoError(t, err)

	return result
}

func TestCompleteAndScopedPublicationConvergeWithoutUntouchedSourceReads(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	initial := f.publish(t, f.request)
	require.True(t, initial.FullDiscovery)
	for _, ticket := range initial.DerivedWork {
		require.NoError(t, f.store.AckDerivedWork(ctx, []anchors.DerivedWork{ticket}))
	}
	original, err := f.store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/untouched.md"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(f.root, "notes", "source.md"), []byte("# Source\nUpdated. See [[target]].\n"), 0600))
	// The scope contains only the edit; removing the other authored source proves
	// scoped preparation does not call the full persisted-source reader.
	hidden := filepath.Join(f.root, "notes", "untouched.hidden")
	require.NoError(t, os.Rename(filepath.Join(f.root, "notes", "untouched.md"), hidden))
	scoped := f.request
	scoped.Paths = []paths.RelPath{"notes/source.md"}
	result := f.publish(t, scoped)
	require.False(t, result.FullDiscovery)
	require.ElementsMatch(t, []string{"notes/source.md", "notes/target.md"}, result.Notes.ChangedPaths)
	rows, err := f.store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/untouched.md"})
	require.NoError(t, err)
	require.Equal(t, original, rows)
	require.NoError(t, os.Rename(hidden, filepath.Join(f.root, "notes", "untouched.md")))
	scopedEdges, err := f.store.AllGraphDocEdgesWithConfidence(ctx)
	require.NoError(t, err)
	require.Len(t, scopedEdges, 4)
	metadata, err := f.store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	f.publish(t, f.request)
	complete, err := f.store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.Equal(t, metadata.NotesHash, complete.NotesHash)
	completeEdges, err := f.store.AllGraphDocEdgesWithConfidence(ctx)
	require.NoError(t, err)
	require.Equal(t, scopedEdges, completeEdges)
}

func TestFailedStructuralPublicationRetainsInactiveDebtAndForcesQuietRecovery(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	discovery, err := indexcore.Discover(ctx, f.request, f.store)
	require.NoError(t, err)
	failed := errors.New("stop after prepared commit")
	_, err = indexcore.Publish(ctx, f.lease, f.request, discovery, f.service, f.store, f.writer, indexcore.PublishOptions{AfterPreparedOwnershipCommit: func(context.Context) error { return failed }})
	require.ErrorIs(t, err, failed)
	debt, err := f.store.HasUnreadyDerivedWork(ctx)
	require.NoError(t, err)
	require.True(t, debt)
	pending, err := f.store.PendingDerivedWork(ctx, time.Now(), 32)
	require.NoError(t, err)
	require.Empty(t, pending)
	scoped := f.request
	scoped.Paths = []paths.RelPath{"notes/source.md"}
	recovered := f.publish(t, scoped)
	require.True(t, recovered.FullDiscovery)
	debt, err = f.store.HasUnreadyDerivedWork(ctx)
	require.NoError(t, err)
	require.False(t, debt)
	pending, err = f.store.PendingDerivedWork(ctx, time.Now(), 32)
	require.NoError(t, err)
	require.NotEmpty(t, pending)
}

func TestExplicitEmptyScopeDoesNotMarkDebtOrPublishStructure(t *testing.T) {
	f := newFixture(t)
	request := f.request
	request.Paths = []paths.RelPath{}
	result := f.publish(t, request)
	require.Empty(t, result.DerivedWork)
	require.Zero(t, result.StructuralGeneration)
	state, err := f.store.GetNoteMetadataState(context.Background())
	require.NoError(t, err)
	require.False(t, state.Ready)
}

func TestPublicationExpandsRetiredTargetDependenciesBeforeAcknowledgement(t *testing.T) {
	t.Parallel()
	f, fresh := newFixture(t), newFixture(t)
	ctx := context.Background()
	initial := f.publish(t, f.request)
	for _, ticket := range initial.DerivedWork {
		require.NoError(t, f.store.AckDerivedWork(ctx, []anchors.DerivedWork{ticket}))
	}
	for _, fixture := range []*fixture{f, fresh} {
		require.NoError(t, os.WriteFile(filepath.Join(fixture.root, ".rhizome", "ignore"), []byte("notes/target.md\n"), 0600))
	}
	scoped := f.request
	scoped.Paths = []paths.RelPath{"notes/target.md"}
	collector := indexingperf.New()
	traced := indexingperf.WithCollector(ctx, collector)
	discovery, err := indexcore.Discover(traced, scoped, f.store)
	require.NoError(t, err)
	require.False(t, discovery.FullDiscovery)
	result, err := indexcore.Publish(traced, f.lease, scoped, discovery, f.service, f.store, f.writer, indexcore.PublishOptions{})
	require.NoError(t, err)
	var passes int64
	for _, counter := range collector.Snapshot().Counters {
		if counter.Name == "ownership.transition_passes" {
			passes += counter.Total
		}
	}
	require.GreaterOrEqual(t, passes, int64(2), "retiring the target must rediscover its referring source")
	require.Contains(t, result.Notes.ChangedPaths, "notes/source.md")
	fresh.publish(t, fresh.request)
	actual, err := f.store.CurrentNoteMetadataPaths(ctx)
	require.NoError(t, err)
	expected, err := fresh.store.CurrentNoteMetadataPaths(ctx)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	actualEdges, err := f.store.AllGraphDocEdgesWithConfidence(ctx)
	require.NoError(t, err)
	expectedEdges, err := fresh.store.AllGraphDocEdgesWithConfidence(ctx)
	require.NoError(t, err)
	require.Equal(t, expectedEdges, actualEdges)
	_, pending, err := f.store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.False(t, pending)
}
