package noderead

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const ensureApplySchema = `
type ProductSpec @node(paths: ["specs/product.md"]) { stories: StoriesSection @contains(level: H2, heading: "Stories") }
type StoriesSection implements Section { stories: [UserStory!] @contains(level: H3) }
type UserStory implements Section @node(locator: EMBEDDED) { related: ProductSpec @link }
`
const ensureApplySource = "# Product\n\n## Stories\n\n### Story A\nrelated:: [[specs/product]]\n"

func TestResolveApplyRequiresLiveAuthorityBeforeWriting(t *testing.T) {
	for _, preview := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "preview"}[preview], func(t *testing.T) {
			ctx := context.Background()
			vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
			service := NewService(vault, &obsidian.Note{}, store, schema)
			options := ScopeOptions{}
			if preview {
				enableTestLinkApply(t, service)
				options.ReadOverlay = &ReadOverlay{}
			}
			_, err := service.NewScope(ctx, options).Resolve(ctx, ensureApplyRequest())
			require.ErrorContains(t, err, "explicit live writer")
			source, err := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
			require.NoError(t, err)
			require.Equal(t, ensureApplySource, string(source))
		})
	}
}

func TestResolveApplyRetriesConvergenceAfterSourceCommit(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
	service := enableTestLinkApply(t, NewService(vault, &obsidian.Note{}, store, schema))
	healthyApply := service.ApplyLinkTargets
	incomplete := errors.New("source applied; index convergence failed")
	calls := 0
	service.ApplyLinkTargets = func(ctx context.Context, request ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
		calls++
		if calls != 1 {
			return healthyApply(ctx, request)
		}
		// Model the owner reporting a committed source with failed publication.
		result, err := (&ontology.NodeLinkService{VaultDef: vault, NoteReader: &obsidian.Note{}, Schema: schema}).LinkTargets(ctx, request)
		require.NoError(t, err)
		return result, incomplete
	}
	scope := service.NewScope(ctx, ScopeOptions{})
	_, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	require.NotEmpty(t, scope.typeInstancesByID)
	_, err = scope.Resolve(ctx, ensureApplyRequest())
	require.ErrorIs(t, err, incomplete)
	require.Empty(t, scope.typeInstancesByID)
	require.False(t, scope.noteStateLoaded)
	require.False(t, scope.notePathCacheLoaded)
	committed, err := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(committed), "^userstory-story-a-"))
	// A fresh locator read sees no fix action, but explicit Apply still repairs.
	planned, err := scope.Resolve(ctx, ResolveRequest{Targets: ensureApplyRequest().Targets, EnsureLinkTarget: ontology.EnsureLinkTargetPlan})
	require.NoError(t, err)
	require.Nil(t, planned.FixPlan)
	fixed, err := scope.Resolve(ctx, ensureApplyRequest())
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Len(t, fixed.Resolved, 1)
	after, err := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
	require.NoError(t, err)
	require.Equal(t, committed, after)
	traversal := TraverseRequest{Sources: []ontology.NodeRef{{NotePath: "specs/product.md", Kind: ontology.NodeKindNote}}, Structural: true, Relation: "related"}
	node := fixed.Resolved[0]
	assertEnsureApplyEdgeConvergence(t, ctx, store, scope, traversal, node.Ref, node.Locator.SourceLocator, node.Locator.LinkTarget.BlockID)
}

func ensureApplyRequest() ResolveRequest {
	return ResolveRequest{Targets: []NodeTarget{{Input: "specs/product.md#Story A"}}, EnsureLinkTarget: ontology.EnsureLinkTargetApply, Hydrate: HydrateOptions{Profile: HydrateSummary}}
}
