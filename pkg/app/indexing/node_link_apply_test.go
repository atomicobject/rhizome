package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestApplyNodeLinkTargetsCommitsAndConvergesStructuralEdge(t *testing.T) {
	ctx := context.Background()
	root, request := nodeLinkApplyFixture(t)
	sourcePath := filepath.Join(root, "specs", "product.md")

	first, err := ApplyNodeLinkTargets(ctx, request)
	require.NoError(t, err)
	require.True(t, first.Applied)
	target := first.Targets[request.LinkTarget.Refs[0].String()]
	require.True(t, target.Exists)
	require.False(t, target.RequiresFix)
	require.NotEmpty(t, target.BlockID)

	afterFirst, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(afterFirst), "^"+target.BlockID))

	store, cleanup, err := ontology.OpenStoreForWrite(root)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	edges, err := store.OntologyEdgesForPath(ctx, "specs/product.md", false, "related", 0)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, target.Ref.NodeID, edges[0].SrcNodeID)
	require.Equal(t, "specs/product.md", edges[0].DstPath)
	require.True(t, edges[0].Structural)
	require.Equal(t, "field", edges[0].Provenance)

	second, err := ApplyNodeLinkTargets(ctx, request)
	require.NoError(t, err)
	require.False(t, second.Applied, "a durable target only recovers exact projection rows")
	afterSecond, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	require.Equal(t, afterFirst, afterSecond)
}

func TestApplyNodeLinkTargetsConvergesDependentEmbeddedLinkEdges(t *testing.T) {
	ctx := context.Background()
	root, request := nodeLinkApplyDependencyFixture(t)

	initial, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     request.VaultDef,
		NoteMetadata: request.NoteMetadata,
		NoteReader:   request.NoteReader,
		Target:       ValidationProjectionLive,
	})
	require.NoError(t, err)
	before, err := initial.Runtime.Store.OntologyEdgesForPath(ctx, "specs/consumer.md", false, "related", 0)
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.NoError(t, initial.Close())

	considered := 0
	result, err := applyNodeLinkTargets(ctx, request, func(ctx context.Context, projectionRequest ValidationProjectionRequest, store *semdb.Store, indexPath string, cleanup func() error) (*ValidationProjectionResult, error) {
		projection, projectionErr := refreshValidationProjectionWithHeldStore(ctx, projectionRequest, store, indexPath, cleanup)
		if projection != nil {
			considered = projection.Counters.OntologyNotesConsidered
		}
		return projection, projectionErr
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.GreaterOrEqual(t, considered, 2, "source-published sync must revisit the dependent note")

	store, cleanup, err := ontology.OpenStoreForWrite(root)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	after, err := store.OntologyEdgesForPath(ctx, "specs/consumer.md", false, "related", 0)
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Equal(t, before[0].DstNodeID, after[0].DstNodeID)
}

func TestApplyNodeLinkTargetsPreflightFailuresLeaveSourceUnchanged(t *testing.T) {
	t.Run("invalid indexer", func(t *testing.T) {
		root, request := nodeLinkApplyFixture(t)
		before := nodeLinkApplySource(t, root)
		request.NoteMetadata = notemeta.Indexer{}

		_, err := ApplyNodeLinkTargets(context.Background(), request)
		require.ErrorContains(t, err, "note metadata indexer")
		require.Equal(t, before, nodeLinkApplySource(t, root))
	})

	t.Run("canceled context", func(t *testing.T) {
		root, request := nodeLinkApplyFixture(t)
		before := nodeLinkApplySource(t, root)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := ApplyNodeLinkTargets(ctx, request)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, before, nodeLinkApplySource(t, root))
	})

	t.Run("store open", func(t *testing.T) {
		root, request := nodeLinkApplyFixture(t)
		before := nodeLinkApplySource(t, root)
		require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte(":"), 0o644))

		_, err := ApplyNodeLinkTargets(context.Background(), request)
		require.Error(t, err)
		require.Equal(t, before, nodeLinkApplySource(t, root))
	})
}

func TestApplyNodeLinkTargetsReportsCommittedSourceWhenConvergenceFailsAndRetries(t *testing.T) {
	ctx := context.Background()
	root, request := nodeLinkApplyFixture(t)
	before := nodeLinkApplySource(t, root)

	result, err := applyNodeLinkTargets(ctx, request, func(context.Context, ValidationProjectionRequest, *semdb.Store, string, func() error) (*ValidationProjectionResult, error) {
		return nil, errors.New("injected projection failure")
	})
	require.ErrorContains(t, err, "source applied; index convergence incomplete")
	require.True(t, result.Applied)
	afterFailure := nodeLinkApplySource(t, root)
	require.NotEqual(t, before, afterFailure)

	retry, err := ApplyNodeLinkTargets(ctx, request)
	require.NoError(t, err)
	require.False(t, retry.Applied, "retry publishes an already committed source edit")
	target := retry.Targets[request.LinkTarget.Refs[0].String()]
	require.NotEmpty(t, target.BlockID)

	store, cleanup, err := ontology.OpenStoreForWrite(root)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	edges, err := store.OntologyEdgesForPath(ctx, "specs/product.md", false, "related", 0)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, target.Ref.NodeID, edges[0].SrcNodeID)
}

func TestApplyNodeLinkTargetsRetriesAfterPublishedMetadataOntologyFailure(t *testing.T) {
	ctx := context.Background()
	root, request := nodeLinkApplyFixture(t)
	before := nodeLinkApplySource(t, root)
	published := false

	result, err := applyNodeLinkTargets(ctx, request, func(ctx context.Context, projectionRequest ValidationProjectionRequest, store *semdb.Store, _ string, _ func() error) (*ValidationProjectionResult, error) {
		queue := indexwriter.New(ctx, indexwriter.Handlers{ApplyNoteMetadataDelta: store.ApplyNoteMetadataDelta})
		defer func() { _ = queue.StopAndWait() }()
		delta, deltaErr := projectionRequest.NoteMetadata.BuildPathDelta(ctx, projectionRequest.VaultDef, projectionRequest.NoteReader, store, []string{"specs/product.md"}, nil)
		if deltaErr != nil {
			return nil, deltaErr
		}
		if delta == nil {
			return nil, errors.New("expected metadata delta")
		}
		if submitErr := queue.SubmitNoteMetadataDelta(ctx, *delta); submitErr != nil {
			return nil, submitErr
		}
		if flushErr := queue.FlushAndWait(ctx); flushErr != nil {
			return nil, flushErr
		}
		if closeErr := queue.Close(); closeErr != nil {
			return nil, closeErr
		}
		published = true
		return nil, errors.New("injected ontology failure after metadata publication")
	})
	require.ErrorContains(t, err, "source applied; index convergence incomplete")
	require.True(t, result.Applied)
	require.True(t, published)
	afterFailure := nodeLinkApplySource(t, root)
	require.NotEqual(t, before, afterFailure)

	store, cleanup, err := ontology.OpenStoreForWrite(root)
	require.NoError(t, err)
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"specs/product.md"})
	require.NoError(t, err)
	require.Len(t, rows, 1, "the injected failure follows queued metadata publication")
	cleanup()

	retry, err := ApplyNodeLinkTargets(ctx, request)
	require.NoError(t, err)
	require.False(t, retry.Applied)
	require.Equal(t, afterFailure, nodeLinkApplySource(t, root), "retry must not edit committed source again")
	target := retry.Targets[request.LinkTarget.Refs[0].String()]
	store, cleanup, err = ontology.OpenStoreForWrite(root)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	edges, err := store.OntologyEdgesForPath(ctx, "specs/product.md", false, "related", 0)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, target.Ref.NodeID, edges[0].SrcNodeID)
}

func TestApplyNodeLinkTargetsReportsCommittedSourceOnPostCommitCancellation(t *testing.T) {
	ctx := context.Background()
	root, request := nodeLinkApplyFixture(t)
	before := nodeLinkApplySource(t, root)

	result, err := applyNodeLinkTargets(ctx, request, func(context.Context, ValidationProjectionRequest, *semdb.Store, string, func() error) (*ValidationProjectionResult, error) {
		return nil, context.Canceled
	})
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "source applied; index convergence incomplete")
	require.True(t, result.Applied)
	require.NotEqual(t, before, nodeLinkApplySource(t, root))
}

func nodeLinkApplyFixture(t *testing.T) (string, NodeLinkApplyRequest) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	schemaText := "\ntype ProductSpec @node(paths: [\"specs/product.md\"]) {\n  stories: StoriesSection @contains(level: H2, heading: \"Stories\")\n}\n\ntype StoriesSection implements Section {\n  stories: [UserStory!] @contains(level: H3)\n}\n\ntype UserStory implements Section @node(locator: EMBEDDED) {\n  related: ProductSpec @link\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(schemaText), 0o644))
	sourceText := "# Product\n\n## Stories\n\n### Story A\nrelated:: [[specs/product]]\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "product.md"), []byte(sourceText), 0o644))

	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	projection, err := ontology.ProjectNode(context.Background(), vaultDef, &obsidian.Note{}, schema, ontology.NodeRef{
		NotePath: "specs/product.md",
		Fragment: "Story A",
		Kind:     ontology.NodeKindEmbedded,
	})
	require.NoError(t, err)
	return root, NodeLinkApplyRequest{
		VaultDef:     vaultDef,
		NoteMetadata: testNoteMetadataIndexer(t),
		NoteReader:   &obsidian.Note{},
		SchemaHash:   schema.Hash,
		LinkTarget: ontology.LinkTargetRequest{
			Refs:   []ontology.NodeRef{projection.Ref},
			Ensure: ontology.EnsureLinkTargetApply,
		},
	}
}

func nodeLinkApplyDependencyFixture(t *testing.T) (string, NodeLinkApplyRequest) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	schemaText := "\ntype ProductSpec @node(paths: [\"specs/*.md\"]) {\n  stories: StoriesSection @contains(level: H2, heading: \"Stories\")\n}\n\ntype StoriesSection implements Section {\n  stories: [UserStory!] @contains(level: H3)\n}\n\ntype UserStory implements Section @node(locator: EMBEDDED) {\n  related: ProductSpec @link\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(schemaText), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "product.md"), []byte("# Product\n\n## Stories\n\n### Story A\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "consumer.md"), []byte("# Consumer\n\n## Stories\n\n### Story B\nrelated:: [[specs/product]]\n"), 0o644))

	vaultDef := obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	projection, err := ontology.ProjectNode(context.Background(), vaultDef, &obsidian.Note{}, schema, ontology.NodeRef{
		NotePath: "specs/product.md",
		Fragment: "Story A",
		Kind:     ontology.NodeKindEmbedded,
	})
	require.NoError(t, err)
	return root, NodeLinkApplyRequest{
		VaultDef:     vaultDef,
		NoteMetadata: testNoteMetadataIndexer(t),
		NoteReader:   &obsidian.Note{},
		SchemaHash:   schema.Hash,
		LinkTarget: ontology.LinkTargetRequest{
			Refs:   []ontology.NodeRef{projection.Ref},
			Ensure: ontology.EnsureLinkTargetApply,
		},
	}
}

func nodeLinkApplySource(t *testing.T, root string) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(root, "specs", "product.md"))
	require.NoError(t, err)
	return string(source)
}
