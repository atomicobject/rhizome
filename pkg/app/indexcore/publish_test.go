package indexcore_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	sqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/indexcore"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type fixture struct {
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
	store, cleanup, err := obsidian.OpenIntelStoreForWriteFromConfig(root, cfg)
	require.NoError(t, err)
	require.NotNil(t, store)
	t.Cleanup(cleanup)
	service, _ := bootstrap.NewCodeAnchorService(bootstrap.CodeAnchorServiceConfig{VaultPath: root, CodeCfg: cfg, Store: store, IncludeIndexers: true, WriteAccess: true})
	require.NotNil(t, service)
	writer := indexwriter.New(ctx, indexcore.BindWriterHandlers(indexwriter.Handlers{}, service, store))
	t.Cleanup(func() { _ = writer.Close() })
	return &fixture{root: root, request: indexcore.Request{VaultDefinition: obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}, Links: obsidian.LinkTypeBoth}, CodeConfig: cfg, NoteMetadata: indexer}, store: store, service: service, writer: writer}
}
func (f *fixture) publish(t *testing.T, request indexcore.Request) indexcore.Result {
	t.Helper()
	ctx := context.Background()
	discovery, err := indexcore.Discover(ctx, request, f.store)
	require.NoError(t, err)
	result, err := indexcore.Publish(ctx, request, discovery, f.service, f.store, f.writer, indexcore.PublishOptions{})
	require.NoError(t, err)
	if result.StructuralGeneration > 0 {
		require.NoError(t, f.writer.AcknowledgeOwnershipReconciliation(ctx, result.StructuralGeneration))
	}
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
	_, err = indexcore.Publish(ctx, f.request, discovery, f.service, f.store, f.writer, indexcore.PublishOptions{AfterPreparedOwnershipCommit: func(context.Context) error { return failed }})
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
