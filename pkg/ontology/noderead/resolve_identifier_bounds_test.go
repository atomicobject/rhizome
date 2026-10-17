package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type identifierBoundsStore struct {
	ReadStore
	plans, unboundedPlans, unboundedTypeScans, propertyReads int
}

func (s *identifierBoundsStore) OntologyNodesByTypePlan(ctx context.Context, plan readmodel.NodeQueryPlan) ([]readmodel.NodeRow, error) {
	s.plans++
	if plan.Limit <= 0 {
		s.unboundedPlans++
	}
	return s.ReadStore.OntologyNodesByTypePlan(ctx, plan)
}
func (s *identifierBoundsStore) OntologyPathsByType(ctx context.Context, name string, limit int) ([]string, error) {
	if limit <= 0 {
		s.unboundedTypeScans++
	}
	return s.ReadStore.OntologyPathsByType(ctx, name, limit)
}
func (s *identifierBoundsStore) CurrentNotePropertyValues(ctx context.Context, paths []string, names []string, source semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error) {
	s.propertyReads++
	return s.ReadStore.CurrentNotePropertyValues(ctx, paths, names, source)
}

func TestResolveIdentifierMissesUseBoundedCachedFieldReads(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `
 type ProductSpec @node(paths: ["specs/*.md"]) { id: ID! @field @identifier(preferred: true) }
 type Task implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#task") { id: ID! @field @identifier(preferred: true, derivedSuffix: "TASK") }
`, "---\nid: SPEC-0001\n---\n# Product\n\n- [ ] A task #task\n  id:: SPEC-0001-TASK-0001\n")
	spy := &identifierBoundsStore{ReadStore: store}
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vault, reader, spy, schema).NewScope(ScopeOptions{})
	initialPlans := 0
	for i, target := range []string{"MISSING", "missing", "[[MISSING]]", "^MISSING"} {
		result, err := scope.Resolve(ctx, ResolveRequest{Targets: []NodeTarget{{Input: target}}, Hydrate: HydrateOptions{Profile: HydrateSummary}})
		require.NoError(t, err)
		for _, resolved := range result.Resolved {
			require.Empty(t, resolved.Record.Path)
		}
		if i == 0 {
			initialPlans = spy.plans
			require.Greater(t, initialPlans, 0)
		} else {
			require.Equal(t, initialPlans, spy.plans, "normalized identifier misses must be shared across Resolve calls")
		}
	}
	require.Zero(t, spy.unboundedPlans)
	require.Zero(t, spy.unboundedTypeScans)
	require.Zero(t, spy.propertyReads)
	require.Zero(t, reader.contents)
	t.Logf("4 normalized misses: bounded field plans=%d, unbounded type reads=%d, property reads=%d, content reads=%d", spy.plans, spy.unboundedTypeScans, spy.propertyReads, reader.contents)
}

func TestResolvePreferredIdentifiersIncludesEmbeddedCandidates(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `
 type ProductSpec @node(paths: ["specs/*.md"]) { id: ID! @field @identifier(preferred: true) }
 type Task implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#task") { id: ID! @field @identifier(preferred: true, derivedSuffix: "TASK") }
`, "---\nid: SPEC-0001\n---\n# Product\n\n- [ ] A task #task\n  id:: SPEC-0001-TASK-0001\n")
	scope := NewService(vault, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})
	for _, test := range []struct {
		id       string
		kind     ontology.NodeKind
		typeName string
	}{{"SPEC-0001", ontology.NodeKindNote, "ProductSpec"}, {"SPEC-0001-TASK-0001", ontology.NodeKindEmbedded, "Task"}} {
		result, err := scope.Resolve(ctx, ResolveRequest{Targets: []NodeTarget{{Input: test.id}}, Hydrate: HydrateOptions{Profile: HydrateSummary}, IndexOnly: true})
		require.NoError(t, err)
		require.Len(t, result.Resolved, 1)
		require.Equal(t, test.kind, result.Resolved[0].Ref.Kind)
		require.Equal(t, test.typeName, result.Resolved[0].Ref.TypeName)
		require.NotEmpty(t, result.Resolved[0].Record.Path)
	}
}

func TestResolveBasenameIdentifierClaimBeyondFirstCandidatesIsAmbiguous(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `type ProductSpec @node(paths: ["specs/*.md"]) { id: ID! @field @identifier(preferred: true) }`, "---\nid: UNIQUE\n---\n# Product\n")
	for _, path := range []string{"specs/a.md", "specs/b.md", "specs/dupe.md"} {
		writeFixtureNote(t, vault.Path, path, "---\nid: DUPE\n---\n# Duplicate\n")
	}
	_, err := ontologytest.Publish(ctx, testkit.NoteMetadataIndexer(t), vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	result, err := NewService(vault, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{}).Resolve(ctx, ResolveRequest{Targets: []NodeTarget{{Input: "DUPE"}}, Hydrate: HydrateOptions{Profile: HydrateSummary}})
	require.NoError(t, err)
	require.Empty(t, result.Resolved)
	require.Len(t, result.Diagnostics, 1)
	require.Equal(t, "target_ambiguous", result.Diagnostics[0].Code)
	require.Len(t, result.Diagnostics[0].Candidates, 3)
	paths := []string{}
	for _, candidate := range result.Diagnostics[0].Candidates {
		paths = append(paths, candidate.NotePath)
	}
	require.ElementsMatch(t, []string{"specs/a.md", "specs/b.md", "specs/dupe.md"}, paths)
}

func TestResolvePreferredIdentifierSpellingMatchesCommittedAndStaged(t *testing.T) {
	ctx := context.Background()
	for _, authored := range []string{"^SPEC-0001", "[[SPEC-0001]]", "[[SPEC-0001|Product display]]", "SPEC-0001|Product display"} {
		t.Run(authored, func(t *testing.T) {
			content := "---\nid: '" + authored + "'\n---\n# Product\n"
			vault, store, schema := buildFixture(t, `type ProductSpec @node(paths: ["specs/*.md"]) { id: ID! @field @identifier(preferred: true) }`, content)
			for _, staged := range []bool{false, true} {
				options := ScopeOptions{}
				if staged {
					options.ReadOverlay = &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"specs/product.md": content}}
				}
				scope := NewService(vault, &obsidian.Note{}, store, schema).NewScope(options)
				for _, target := range []string{"SPEC-0001", "^SPEC-0001"} {
					result, err := scope.Resolve(ctx, ResolveRequest{Targets: []NodeTarget{{Input: target}}, Hydrate: HydrateOptions{Profile: HydrateSummary}})
					require.NoError(t, err)
					require.Len(t, result.Resolved, 1)
					require.Equal(t, "specs/product.md", result.Resolved[0].Ref.NotePath)
					require.Equal(t, "ProductSpec", result.Resolved[0].Ref.TypeName)
					require.Equal(t, []string{authored}, scope.FieldValues(result.Resolved[0].Record, "id"))
				}
			}
		})
	}
}
