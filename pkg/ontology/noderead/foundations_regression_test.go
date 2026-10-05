package noderead

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestLocatorsApplyRequiresLiveAuthorityBeforeWriting(t *testing.T) {
	for _, mode := range []string{"no-live-writer", "preview", "preview-with-writer"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
			service := NewService(vault, &obsidian.Note{}, store, schema)
			options := ScopeOptions{}
			if strings.HasPrefix(mode, "preview") {
				options.ReadOverlay = &ReadOverlay{}
			}
			if mode == "preview-with-writer" {
				service.ApplyLinkTargets = func(context.Context, ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
					t.Fatal("preview must reject before invoking the live writer")
					return ontology.LinkTargetResult{}, nil
				}
			}
			scope := service.NewScope(ctx, options)
			items, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
			require.NoError(t, err)
			require.Len(t, items.Items, 1)
			ref := items.Items[0].Ref
			before, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
			require.NoError(t, err)
			require.Len(t, before, 1)
			beforeSource, err := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
			require.NoError(t, err)
			locators, err := scope.LocatorsWithEnsure(ctx, []ontology.NodeRef{ref}, ontology.EnsureLinkTargetApply)
			afterSource, readErr := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
			require.NoError(t, readErr)
			after, hydrateErr := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
			require.ErrorContains(t, err, "explicit live writer")
			require.Empty(t, locators)
			require.Equal(t, string(beforeSource), string(afterSource))
			require.NoError(t, hydrateErr)
			require.Len(t, after, 1)
			require.Equal(t, before[0].Content, after[0].Content)
		})
	}
}

func TestCanceledHydrateCanRetrySameScope(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
	service := NewService(vault, &obsidian.Note{}, store, schema)
	inventory, err := service.NewScope(ctx, ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	require.Len(t, inventory.Items, 1)
	ref := inventory.Items[0].Ref
	scope := service.NewScope(ctx, ScopeOptions{})
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	first, firstErr := scope.Hydrate(canceled, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
	retry, retryErr := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
	control, controlErr := service.NewScope(ctx, ScopeOptions{}).Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, controlErr)
	require.Len(t, control, 1)
	require.ErrorIs(t, firstErr, context.Canceled)
	require.Empty(t, first)
	require.NoError(t, retryErr)
	require.Len(t, retry, 1)
	require.Equal(t, control[0].Content, retry[0].Content)
}

func TestLocatorsApplyUsesConfiguredWriter(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
	service := NewService(vault, &obsidian.Note{}, store, schema)
	sentinel := errors.New("live writer says no")
	calls := 0
	service.ApplyLinkTargets = func(context.Context, ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
		calls++
		return ontology.LinkTargetResult{}, sentinel
	}
	scope := service.NewScope(ctx, ScopeOptions{})
	items, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	require.Len(t, items.Items, 1)
	ref := items.Items[0].Ref
	for _, mode := range []ontology.EnsureLinkTargetMode{ontology.EnsureLinkTargetNever, ontology.EnsureLinkTargetPlan} {
		_, err = scope.LocatorsWithEnsure(ctx, []ontology.NodeRef{ref}, mode)
		require.NoError(t, err)
		bytes, readErr := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
		require.NoError(t, readErr)
		require.Equal(t, ensureApplySource, string(bytes))
	}
	_, err = scope.Resolve(ctx, ensureApplyRequest())
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 1, calls)
	bytes, readErr := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
	require.NoError(t, readErr)
	require.Equal(t, ensureApplySource, string(bytes))
	_, err = scope.LocatorsWithEnsure(ctx, []ontology.NodeRef{ref}, ontology.EnsureLinkTargetApply)
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 2, calls)
	bytes, readErr = os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
	require.NoError(t, readErr)
	require.Equal(t, ensureApplySource, string(bytes))
}

type cancelMetadataStore struct {
	*semdb.Store
	cancel context.CancelFunc
}

func (s *cancelMetadataStore) CurrentNoteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]semdb.NoteMetadataRow, error) {
	if s.cancel != nil {
		cancel := s.cancel
		s.cancel = nil
		cancel()
		return nil, ctx.Err()
	}
	return s.Store.CurrentNoteMetadataRowsByPaths(ctx, paths)
}
func TestInFlightCanceledHydrateCanRetrySameScope(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
	inventory, err := NewService(vault, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	require.Len(t, inventory.Items, 1)
	ref := inventory.Items[0].Ref
	active, cancel := context.WithCancel(ctx)
	defer cancel()
	cancelStore := &cancelMetadataStore{Store: store, cancel: cancel}
	service := NewService(vault, &obsidian.Note{}, cancelStore, schema)
	scope := service.NewScope(ctx, ScopeOptions{})
	first, firstErr := scope.Hydrate(active, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
	retry, retryErr := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
	control, controlErr := service.NewScope(ctx, ScopeOptions{}).Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
	require.ErrorIs(t, firstErr, context.Canceled)
	require.Empty(t, first)
	require.NoError(t, retryErr)
	require.Len(t, retry, 1)
	require.NoError(t, controlErr)
	require.Len(t, control, 1)
	require.Equal(t, control[0].Content, retry[0].Content)
}

type cancelSourceReader struct {
	obsidian.NoteReader
	cancel             context.CancelFunc
	err                error
	readsBeforeFailure int
}

func (r *cancelSourceReader) GetContents(v obsidian.VaultDefinition, path string) (string, error) {
	if r.readsBeforeFailure > 0 {
		r.readsBeforeFailure--
		return r.NoteReader.GetContents(v, path)
	}
	if r.cancel != nil || r.err != nil {
		cancel := r.cancel
		r.cancel = nil
		if cancel != nil {
			cancel()
		}
		err := r.err
		r.err = nil
		if err == nil {
			err = context.Canceled
		}
		return "", err
	}
	return r.NoteReader.GetContents(v, path)
}

func TestSourceCanceledHydrateCanRetrySameScope(t *testing.T) {
	for _, reads := range []int{0, 1} {
		t.Run(map[int]string{0: "snapshot", 1: "locator"}[reads], func(t *testing.T) {
			ctx := context.Background()
			vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
			inventory, err := NewService(vault, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
			require.NoError(t, err)
			ref := inventory.Items[0].Ref
			active, cancel := context.WithCancel(ctx)
			defer cancel()
			service := NewService(vault, &cancelSourceReader{NoteReader: &obsidian.Note{}, cancel: cancel, readsBeforeFailure: reads}, store, schema)
			scope := service.NewScope(ctx, ScopeOptions{})
			_, err = scope.Hydrate(active, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
			require.ErrorIs(t, err, context.Canceled)
			retry, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
			require.NoError(t, err)
			require.Len(t, retry, 1)
			require.Contains(t, retry[0].Content, "related::")
			require.Equal(t, ontology.NodeLocatorRequiresFix, retry[0].NodeLocator.Status)
		})
	}
}

func TestExpiredReadsCanRetrySameScope(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
	service := NewService(vault, &obsidian.Note{}, store, schema)
	inventory, err := service.NewScope(ctx, ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	refs := []ontology.NodeRef{inventory.Items[0].Ref}
	expired, cancel := context.WithDeadline(ctx, time.Time{})
	defer cancel()
	scope := service.NewScope(ctx, ScopeOptions{})
	_, err = scope.Hydrate(expired, refs, HydrateOptions{Profile: HydrateContent})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	_, err = scope.Locators(expired, refs)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	retry, err := scope.Hydrate(ctx, refs, HydrateOptions{Profile: HydrateContent})
	require.NoError(t, err)
	require.Len(t, retry, 1)
	locators, err := scope.Locators(ctx, refs)
	require.NoError(t, err)
	require.Equal(t, ontology.NodeLocatorRequiresFix, locators[refs[0].String()].Status)
}

func TestLocatorsApplyConvergesAndRetriesPublication(t *testing.T) {
	for _, failFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "healthy", true: "publication-failure"}[failFirst], func(t *testing.T) {
			ctx := context.Background()
			vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
			service := enableTestLinkApply(t, NewService(vault, &obsidian.Note{}, store, schema))
			shared := &locatorSharedCacheProbe{}
			service.SharedCache = shared
			healthyApply := service.ApplyLinkTargets
			incomplete := errors.New("source applied; index convergence failed")
			calls := 0
			service.ApplyLinkTargets = func(ctx context.Context, request ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
				calls++
				if calls != 1 || !failFirst {
					return healthyApply(ctx, request)
				}
				result, err := (&ontology.NodeLinkService{VaultDef: vault, NoteReader: &obsidian.Note{}, Schema: schema}).LinkTargets(ctx, request)
				require.NoError(t, err)
				return result, incomplete
			}
			scope := service.NewScope(ctx, ScopeOptions{})
			inventory, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
			require.NoError(t, err)
			refs := []ontology.NodeRef{inventory.Items[0].Ref}
			graphRequest := GraphRequest{Paths: []string{"specs/product.md"}, Profile: GraphProfileOntologyNative}
			factsRequest := GraphFactsRequest{Paths: []string{"specs/product.md"}, IncludeOntology: true, IncludeEmbedded: true}
			beforeGraph, err := scope.Graph(ctx, graphRequest)
			require.NoError(t, err)
			beforeFacts, err := scope.GraphFacts(ctx, factsRequest)
			require.NoError(t, err)
			_, err = scope.Hydrate(ctx, refs, HydrateOptions{Profile: HydrateContent})
			require.NoError(t, err)
			traversal := TraverseRequest{Sources: []ontology.NodeRef{{NotePath: "specs/product.md", Kind: ontology.NodeKindNote}}, Structural: true, Relation: "related"}
			_, err = scope.Traverse(ctx, traversal)
			require.NoError(t, err)
			_, err = scope.LocatorsWithEnsure(ctx, refs, ontology.EnsureLinkTargetApply)
			if failFirst {
				require.ErrorIs(t, err, incomplete)
			} else {
				require.NoError(t, err)
			}
			committed, err := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
			require.NoError(t, err)
			read, err := scope.Hydrate(ctx, refs, HydrateOptions{Profile: HydrateContent})
			require.NoError(t, err)
			require.Len(t, read, 1)
			require.Contains(t, read[0].Content, "^userstory-")
			locators, err := scope.LocatorsWithEnsure(ctx, refs, ontology.EnsureLinkTargetApply)
			require.NoError(t, err)
			locator := locators[refs[0].String()]
			require.Equal(t, ontology.NodeLocatorLinkable, locator.Status)
			require.NotNil(t, locator.LinkTarget)
			canonical := locator.LinkTarget.Ref
			_, err = scope.LocatorsWithEnsure(ctx, []ontology.NodeRef{canonical}, ontology.EnsureLinkTargetApply)
			require.NoError(t, err)
			require.Equal(t, 3, calls, "already-linkable apply must still converge")
			require.Equal(t, calls, shared.invalidations, "every attempt, including a failed publication, invalidates shared derived state")
			after, err := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
			require.NoError(t, err)
			require.Equal(t, committed, after)
			for _, current := range []*Scope{scope, service.NewScope(ctx, ScopeOptions{})} {
				assertEnsureApplyEdgeConvergence(t, ctx, store, current, traversal, canonical, locator.LinkTarget.Markdown, locator.LinkTarget.BlockID)
				collection, err := current.TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
				require.NoError(t, err)
				require.Len(t, collection.Items, 1)
				require.NotEqual(t, refs[0], collection.Items[0].Ref)
				require.Equal(t, canonical, collection.Items[0].Ref)
				graph, err := current.Graph(ctx, graphRequest)
				require.NoError(t, err)
				facts, err := current.GraphFacts(ctx, factsRequest)
				require.NoError(t, err)
				for _, endpoints := range []struct{ before, after []GraphEndpoint }{{beforeGraph.Nodes, graph.Nodes}, {beforeFacts.Nodes, facts.Nodes}} {
					story := func(nodes []GraphEndpoint) GraphEndpoint {
						var matches []GraphEndpoint
						for _, node := range nodes {
							if node.TypeName == "UserStory" {
								matches = append(matches, node)
							}
						}
						require.Len(t, matches, 1)
						return matches[0]
					}
					before, after := story(endpoints.before), story(endpoints.after)
					require.NotEqual(t, before.NodeID, after.NodeID)
					require.Equal(t, ontology.OntologyNodeID(canonical), after.NodeID)
					require.Equal(t, canonical, after.Ref)
					require.Equal(t, locator.LinkTarget.Markdown, after.SourceLocator)
				}
				records, err := current.Hydrate(ctx, []ontology.NodeRef{canonical}, HydrateOptions{Profile: HydrateContent})
				require.NoError(t, err)
				require.Len(t, records, 1)
				require.Contains(t, records[0].Content, "^"+locator.LinkTarget.BlockID)
			}
		})
	}
}

type locatorSharedCacheProbe struct{ invalidations int }

func (*locatorSharedCacheProbe) Get(NodeCacheKey) (CachedNode, bool)         { return CachedNode{}, false }
func (*locatorSharedCacheProbe) Set(NodeCacheKey, CachedNode, NodeCacheMeta) {}
func (*locatorSharedCacheProbe) InvalidatePaths([]string)                    {}
func (c *locatorSharedCacheProbe) InvalidateAll(string)                      { c.invalidations++ }

func TestChildDeadlineLocatorsCanRetryWithHealthyParent(t *testing.T) {
	for _, boundary := range []string{"link-service", "scope-locators", "scope-hydrate"} {
		t.Run(boundary, func(t *testing.T) {
			ctx := context.Background()
			vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
			inventory, err := NewService(vault, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
			require.NoError(t, err)
			refs := []ontology.NodeRef{inventory.Items[0].Ref}
			child, cancel := context.WithDeadline(ctx, time.Time{})
			defer cancel()
			reader := &cancelSourceReader{NoteReader: &obsidian.Note{}, err: fmt.Errorf("source child deadline: %w", child.Err())}
			service := NewService(vault, reader, store, schema)
			scope := service.NewScope(ctx, ScopeOptions{})
			if boundary == "scope-hydrate" {
				_, err = scope.Hydrate(ctx, refs, HydrateOptions{Profile: HydrateContent})
				require.ErrorIs(t, err, context.DeadlineExceeded)
				retry, err := scope.Hydrate(ctx, refs, HydrateOptions{Profile: HydrateContent})
				require.NoError(t, err)
				require.Len(t, retry, 1)
			} else {
				load := scope.Locators
				if boundary == "link-service" {
					linker := ontology.NodeLinkService{VaultDef: vault, NoteReader: reader, Schema: schema}
					load = func(ctx context.Context, refs []ontology.NodeRef) (map[string]ontology.NodeLocator, error) {
						return linker.Locators(ctx, ontology.LinkTargetRequest{Refs: refs, Ensure: ontology.EnsureLinkTargetNever})
					}
				}
				_, err = load(ctx, refs)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				retry, err := load(ctx, refs)
				require.NoError(t, err)
				require.Equal(t, ontology.NodeLocatorRequiresFix, retry[refs[0].String()].Status)
			}
			require.NoError(t, ctx.Err(), "only the source reader's child deadline expired")
		})
	}
}

func TestLocatorsApplyNonEmbeddedDoesNotRequireWriter(t *testing.T) {
	for _, kind := range []ontology.NodeKind{ontology.NodeKindNote, ontology.NodeKindSection} {
		t.Run(string(kind), func(t *testing.T) {
			ctx := context.Background()
			vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
			service := NewService(vault, &obsidian.Note{}, store, schema)
			ref := ontology.NodeRef{NotePath: "specs/product.md", Kind: kind}
			key, status := ref.NotePath, ontology.NodeLocatorLinkable
			if kind == ontology.NodeKindSection {
				ref.Fragment, key, status = "Stories", ref.NotePath+"#Stories", ontology.NodeLocatorUnsupported
			}
			locators, err := service.NewScope(ctx, ScopeOptions{ReadOverlay: &ReadOverlay{}}).LocatorsWithEnsure(ctx, []ontology.NodeRef{ref}, ontology.EnsureLinkTargetApply)
			require.NoError(t, err)
			require.Equal(t, status, locators[key].Status)
			source, err := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
			require.NoError(t, err)
			require.Equal(t, ensureApplySource, string(source))
		})
	}
}

func TestOrdinaryLocatorSourceFailureRemainsDiagnostic(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
	inventory, err := NewService(vault, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	refs := []ontology.NodeRef{inventory.Items[0].Ref}
	reader := &cancelSourceReader{NoteReader: &obsidian.Note{}, err: errors.New("ordinary source read failure")}
	service := NewService(vault, reader, store, schema)
	scope := service.NewScope(ctx, ScopeOptions{})
	locators, err := scope.Locators(ctx, refs)
	require.NoError(t, err)
	require.Equal(t, ontology.NodeLocatorUnresolved, locators[refs[0].String()].Status)
	require.NotEmpty(t, locators[refs[0].String()].Diagnostics)
	locators, err = scope.Locators(ctx, refs)
	require.NoError(t, err)
	require.Equal(t, ontology.NodeLocatorUnresolved, locators[refs[0].String()].Status, "ordinary scope-local unresolved results remain cached")
	locators, err = service.NewScope(ctx, ScopeOptions{}).Locators(ctx, refs)
	require.NoError(t, err)
	require.Equal(t, ontology.NodeLocatorRequiresFix, locators[refs[0].String()].Status)
}
func TestCanceledLocatorsCanRetrySameScope(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
	inventory, err := NewService(vault, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	require.Len(t, inventory.Items, 1)
	ref := inventory.Items[0].Ref
	active, cancel := context.WithCancel(ctx)
	defer cancel()
	reader := &cancelSourceReader{NoteReader: &obsidian.Note{}, cancel: cancel}
	service := NewService(vault, reader, store, schema)
	scope := service.NewScope(ctx, ScopeOptions{})
	first, firstErr := scope.Locators(active, []ontology.NodeRef{ref})
	retry, retryErr := scope.Locators(ctx, []ontology.NodeRef{ref})
	control, controlErr := service.NewScope(ctx, ScopeOptions{}).Locators(ctx, []ontology.NodeRef{ref})
	require.ErrorIs(t, firstErr, context.Canceled)
	require.Empty(t, first)
	require.NoError(t, retryErr)
	require.NoError(t, controlErr)

	require.Equal(t, ontology.NodeLocatorRequiresFix, retry[ref.String()].Status)
	require.Equal(t, ontology.NodeLocatorRequiresFix, control[ref.String()].Status)
}
