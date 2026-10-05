package noderead

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// EnsureLinkTargetApply is the one write-capable Scope seam. Once it makes an
// embedded node linkable, its durable catalog node and structural field edge
// must converge on the new block-backed identity for both this Scope and the
// next request's fresh Scope.
func TestResolveEnsureLinkTargetApplyConvergesStructuralFieldEdges(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  related: ProductSpec @link
}
`, "# Product\n\n## Stories\n\n### Story A\nrelated:: [[specs/product]]\n")

	sourcePath := filepath.Join(vault.Path, "specs", "product.md")
	beforeSource, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	require.NotContains(t, string(beforeSource), "#^")

	request := ResolveRequest{
		Targets:          []NodeTarget{{Input: "specs/product.md#Story A"}},
		EnsureLinkTarget: ontology.EnsureLinkTargetApply,
		Hydrate:          HydrateOptions{Profile: HydrateSummary},
	}
	traversal := TraverseRequest{
		Sources:    []ontology.NodeRef{{NotePath: "specs/product.md", Kind: ontology.NodeKindNote}},
		Structural: true,
		Relation:   "related",
	}

	scope := enableTestLinkApply(t, NewService(vault, &obsidian.Note{}, store, schema)).NewScope(ctx, ScopeOptions{})
	warm, err := scope.Traverse(ctx, traversal)
	require.NoError(t, err)
	require.Len(t, warm.EdgesBySource["specs/product.md"], 1)
	provisionalSourceID := warm.EdgesBySource["specs/product.md"][0].SrcNodeID

	fixed, err := scope.Resolve(ctx, request)
	require.NoError(t, err)
	require.Len(t, fixed.Resolved, 1)
	require.NotNil(t, fixed.Resolved[0].Locator.LinkTarget)
	require.Equal(t, ontology.NodeLocatorLinkable, fixed.Resolved[0].Locator.Status)
	blockID := fixed.Resolved[0].Locator.LinkTarget.BlockID
	require.NotEmpty(t, blockID)
	require.NotEqual(t, provisionalSourceID, fixed.Resolved[0].Ref.NodeID)

	afterFirstApply, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	require.Contains(t, string(afterFirstApply), "related:: [[specs/product]]")
	require.Equal(t, 1, strings.Count(string(afterFirstApply), "^"+blockID), "apply adds one durable block target")

	// A second apply observes the durable target and must not make another edit.
	second, err := scope.Resolve(ctx, request)
	require.NoError(t, err)
	require.Len(t, second.Resolved, 1)
	afterSecondApply, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	require.Equal(t, afterFirstApply, afterSecondApply, "apply is source-idempotent once linkable")

	assertEnsureApplyEdgeConvergence(t, ctx, store, scope, traversal, fixed.Resolved[0].Ref, fixed.Resolved[0].Locator.SourceLocator, blockID)
	freshScope := NewService(vault, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{})
	assertEnsureApplyEdgeConvergence(t, ctx, store, freshScope, traversal, fixed.Resolved[0].Ref, fixed.Resolved[0].Locator.SourceLocator, blockID)
}

func assertEnsureApplyEdgeConvergence(t *testing.T, ctx context.Context, store *semdb.Store, scope *Scope, traversal TraverseRequest, ref ontology.NodeRef, sourceLocator, blockID string) {
	t.Helper()
	nodes, err := store.OntologyNodesBySourceLocators(ctx, []string{sourceLocator})
	require.NoError(t, err)
	catalogNode, ok := nodes[sourceLocator]
	require.True(t, ok, "the durable catalog owner must use the refreshed source locator")
	require.Equal(t, ontology.OntologyNodeID(ref), catalogNode.NodeID)
	require.Equal(t, "UserStory", catalogNode.TypeName)
	require.Equal(t, blockID, catalogNode.BlockID)

	edges, err := scope.Traverse(ctx, traversal)
	require.NoError(t, err)
	require.Len(t, edges.EdgesBySource["specs/product.md"], 1)
	edge := edges.EdgesBySource["specs/product.md"][0]
	// Edge endpoints use the projection NodeID/source-locator representation;
	// catalog rows use OntologyNodeID(ref). Keep the two contracts distinct.
	require.Equal(t, ref.NodeID, edge.SrcNodeID)
	require.Equal(t, "related", edge.RelationName)
	require.Equal(t, "specs/product.md", edge.DstPath)
	require.True(t, edge.Structural)
	require.Equal(t, "field", edge.Provenance)
}

func enableTestLinkApply(t *testing.T, service *Service) *Service {
	t.Helper()
	indexer := testNoteMetadataIndexer(t)
	service.ApplyLinkTargets = func(ctx context.Context, request ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
		// The app coordinator has its own lock/queue tests. This test adapter
		// exercises Scope behavior against the canonical ontology sync contract.
		result, err := (&ontology.NodeLinkService{VaultDef: service.VaultDef, NoteReader: service.NoteReader, Schema: service.Schema}).LinkTargets(ctx, request)
		if err != nil {
			return result, err
		}
		changed := make([]string, 0, len(request.Refs))
		for _, ref := range request.Refs {
			changed = append(changed, ref.NotePath)
		}
		store := service.Store.(*semdb.Store)
		if err := indexer.SyncPaths(ctx, service.VaultDef, service.NoteReader, store, changed, nil); err != nil {
			return result, err
		}
		_, err = ontology.SyncPublishedPaths(ctx, indexer, service.VaultDef, service.NoteReader, store, nil, changed, nil)
		return result, err
	}
	return service
}
