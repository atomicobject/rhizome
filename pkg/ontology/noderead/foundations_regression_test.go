package noderead

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestApplyRequiresLiveAuthorityBeforeWriting(t *testing.T) {
	for _, entry := range []string{"Resolve", "LocatorsWithEnsure"} {
		t.Run(entry, func(t *testing.T) {
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
					scope := service.NewScope(options)
					items, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
					require.NoError(t, err)
					require.Len(t, items.Items, 1)
					ref := items.Items[0].Ref
					before, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
					require.NoError(t, err)
					require.Len(t, before, 1)
					beforeSource, err := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
					require.NoError(t, err)
					if entry == "Resolve" {
						_, err = scope.Resolve(ctx, ensureApplyRequest())
					} else {
						locators, locatorErr := scope.LocatorsWithEnsure(ctx, []ontology.NodeRef{ref}, ontology.EnsureLinkTargetApply)
						err = locatorErr
						require.Empty(t, locators)
					}
					afterSource, readErr := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
					require.NoError(t, readErr)
					after, hydrateErr := scope.Hydrate(ctx, []ontology.NodeRef{ref}, HydrateOptions{Profile: HydrateContent})
					require.ErrorContains(t, err, "explicit live writer")
					require.Equal(t, string(beforeSource), string(afterSource))
					require.NoError(t, hydrateErr)
					require.Len(t, after, 1)
					require.Equal(t, before[0].Content, after[0].Content)
				})
			}
		})
	}
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
	scope := service.NewScope(ScopeOptions{})
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

type cancelSourceReader struct {
	obsidian.NoteReader
	cancel context.CancelFunc
	err    error
}

func (r *cancelSourceReader) GetContents(v obsidian.VaultDefinition, path string) (string, error) {
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

func TestApplyConvergesAndRetriesPublication(t *testing.T) {
	for _, entry := range []string{"Resolve", "LocatorsWithEnsure"} {
		t.Run(entry, func(t *testing.T) {
			for _, failFirst := range []bool{false, true} {
				t.Run(map[bool]string{false: "healthy", true: "publication-failure"}[failFirst], func(t *testing.T) {
					ctx := context.Background()
					vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
					service := enableTestLinkApply(t, NewService(vault, &obsidian.Note{}, store, schema))
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
					scope := service.NewScope(ScopeOptions{})
					inventory, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
					require.NoError(t, err)
					refs := []ontology.NodeRef{inventory.Items[0].Ref}
					apply := func(refs []ontology.NodeRef) (map[string]ontology.NodeLocator, error) {
						if entry == "LocatorsWithEnsure" {
							return scope.LocatorsWithEnsure(ctx, refs, ontology.EnsureLinkTargetApply)
						}
						request := ensureApplyRequest()
						request.Targets = make([]NodeTarget, len(refs))
						for i, ref := range refs {
							request.Targets[i].Input = mustNodeRefJSON(t, ref)
						}
						result, err := scope.Resolve(ctx, request)
						if err != nil {
							return nil, err
						}
						locators := make(map[string]ontology.NodeLocator, len(refs))
						for i, node := range result.Resolved {
							locators[refs[i].String()] = node.Locator
						}
						return locators, nil
					}
					graphRequest := GraphRequest{Paths: []string{"specs/product.md"}, Profile: GraphProfileOntologyNative}
					factsRequest := GraphFactsRequest{Paths: []string{"specs/product.md"}, IncludeOntology: true, IncludeEmbedded: true}
					beforeGraph, err := scope.Graph(ctx, graphRequest)
					require.NoError(t, err)
					beforeFacts, err := scope.GraphFacts(ctx, factsRequest)
					require.NoError(t, err)
					_, err = scope.Hydrate(ctx, refs, HydrateOptions{Profile: HydrateContent})
					require.NoError(t, err)
					traversal := NeighborhoodRequest{Sources: []ontology.NodeRef{{NotePath: "specs/product.md", Kind: ontology.NodeKindNote}}, IncludeStructural: true, Direction: TraversalDirectionInbound, RelationNames: []string{"related"}}
					_, err = scope.Edges(ctx, traversal)
					require.NoError(t, err)
					_, err = apply(refs)
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
					planned, err := scope.Resolve(ctx, ResolveRequest{Targets: ensureApplyRequest().Targets, EnsureLinkTarget: ontology.EnsureLinkTargetPlan})
					require.NoError(t, err)
					require.Nil(t, planned.FixPlan, "an already-written block needs convergence even without another source fix")
					locators, err := apply(refs)
					require.NoError(t, err)
					locator := locators[refs[0].String()]
					require.Equal(t, ontology.NodeLocatorLinkable, locator.Status)
					require.NotNil(t, locator.LinkTarget)
					canonical := locator.LinkTarget.Ref
					_, err = apply([]ontology.NodeRef{canonical})
					require.NoError(t, err)
					require.Equal(t, 3, calls, "already-linkable apply must still converge")
					after, err := os.ReadFile(filepath.Join(vault.Path, "specs/product.md"))
					require.NoError(t, err)
					require.Equal(t, committed, after)
					for _, current := range []*Scope{scope, service.NewScope(ScopeOptions{})} {
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
			locators, err := service.NewScope(ScopeOptions{ReadOverlay: &ReadOverlay{}}).LocatorsWithEnsure(ctx, []ontology.NodeRef{ref}, ontology.EnsureLinkTargetApply)
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
	inventory, err := NewService(vault, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
	require.NoError(t, err)
	refs := []ontology.NodeRef{inventory.Items[0].Ref}
	reader := &cancelSourceReader{NoteReader: &obsidian.Note{}, err: errors.New("ordinary source read failure")}
	service := NewService(vault, reader, store, schema)
	scope := service.NewScope(ScopeOptions{})
	locators, err := scope.Locators(ctx, refs)
	require.NoError(t, err)
	require.Equal(t, ontology.NodeLocatorUnresolved, locators[refs[0].String()].Status)
	require.NotEmpty(t, locators[refs[0].String()].Diagnostics)
	locators, err = scope.Locators(ctx, refs)
	require.NoError(t, err)
	require.Equal(t, ontology.NodeLocatorUnresolved, locators[refs[0].String()].Status, "ordinary scope-local unresolved results remain cached")
	locators, err = service.NewScope(ScopeOptions{}).Locators(ctx, refs)
	require.NoError(t, err)
	require.Equal(t, ontology.NodeLocatorRequiresFix, locators[refs[0].String()].Status)
}

func TestCanceledReadLeavesScopeRetryable(t *testing.T) {
	for _, test := range []struct {
		name, boundary string
		metadata       bool
		childDeadline  bool
	}{
		{name: "metadata-store", boundary: "hydrate", metadata: true},
		{name: "snapshot-source", boundary: "hydrate"},
		{name: "locator-source", boundary: "locators"},
		{name: "child-deadline/link-service", boundary: "link-service", childDeadline: true},
		{name: "child-deadline/scope-locators", boundary: "locators", childDeadline: true},
		{name: "child-deadline/scope-hydrate", boundary: "hydrate", childDeadline: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			vault, store, schema := buildFixture(t, ensureApplySchema, ensureApplySource)
			inventory, err := NewService(vault, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{}).TypeInstances(ctx, TypeInstancesRequest{TypeName: "UserStory"})
			require.NoError(t, err)
			require.Len(t, inventory.Items, 1)
			refs := []ontology.NodeRef{inventory.Items[0].Ref}
			active, cancel := context.WithCancel(ctx)
			defer cancel()
			reader := &cancelSourceReader{NoteReader: &obsidian.Note{}}
			service := NewService(vault, reader, store, schema)
			wantErr := context.Canceled
			if test.metadata {
				service.Store = &cancelMetadataStore{Store: store, cancel: cancel}
			} else if test.childDeadline {
				wantErr = context.DeadlineExceeded
				reader.err = fmt.Errorf("source child deadline: %w", wantErr)
			} else {
				reader.cancel = cancel
			}
			scope := service.NewScope(ScopeOptions{})
			linker := ontology.NodeLinkService{VaultDef: vault, NoteReader: reader, Schema: schema}
			load := func(ctx context.Context, scope *Scope) (any, error) {
				switch test.boundary {
				case "hydrate":
					return scope.Hydrate(ctx, refs, HydrateOptions{Profile: HydrateContent})
				case "locators":
					return scope.Locators(ctx, refs)
				default:
					return linker.Locators(ctx, ontology.LinkTargetRequest{Refs: refs, Ensure: ontology.EnsureLinkTargetNever})
				}
			}
			_, err = load(active, scope)
			require.ErrorIs(t, err, wantErr)
			retry, err := load(ctx, scope)
			require.NoError(t, err)
			require.NotEmpty(t, retry)
			control, err := load(ctx, service.NewScope(ScopeOptions{}))
			require.NoError(t, err)
			require.Equal(t, control, retry)
			require.NoError(t, ctx.Err())
		})
	}
}
