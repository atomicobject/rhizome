package idalloc

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	pathsByType map[string][]string
	values      map[string]map[string]string // notePath -> property -> value
	pathsErr    error
	valuesErr   error
	nodesByType map[string][]codeanchor.IntelOntologyNode
	fieldValues []codeanchor.IntelOntologyNodeFieldValue
	nodesErr    error
	fieldsErr   error
}

func (f *fakeStore) OntologyNodesByType(_ context.Context, name string) ([]codeanchor.IntelOntologyNode, error) {
	return f.nodesByType[name], f.nodesErr
}

func (f *fakeStore) OntologyNodeFieldValuesByNodeIDs(_ context.Context, _ []string, _ []string) ([]codeanchor.IntelOntologyNodeFieldValue, error) {
	return f.fieldValues, f.fieldsErr
}

func (f *fakeStore) OntologyPathsByType(_ context.Context, typeName string, _ int) ([]string, error) {
	if f.pathsErr != nil {
		return nil, f.pathsErr
	}
	out := append([]string(nil), f.pathsByType[typeName]...)
	return out, nil
}

func (f *fakeStore) CurrentNotePropertyValues(_ context.Context, paths []string, props []string, _ semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error) {
	if f.valuesErr != nil {
		return nil, f.valuesErr
	}
	wanted := make(map[string]struct{}, len(props))
	for _, p := range props {
		wanted[p] = struct{}{}
	}
	var out []semdb.NotePropertyValueRow
	for _, p := range paths {
		for prop, val := range f.values[p] {
			if _, ok := wanted[prop]; !ok {
				continue
			}
			out = append(out, semdb.NotePropertyValueRow{
				NotePath:     p,
				PropertyName: prop,
				ValueText:    val,
			})
		}
	}
	return out, nil
}

func loadTestSchema(t *testing.T, sdl string) *ontology.Schema {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(sdl), 0o644))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	return schema
}

func TestAllocate_EmptyVaultReturnsFirstID(t *testing.T) {
	schema := loadTestSchema(t, `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
  name: String!
}
`)
	store := &fakeStore{}
	res, err := Allocate(context.Background(), schema, store, "Spec")
	require.NoError(t, err)
	require.Equal(t, ontology.IdentifierStrategySequential, res.Strategy)
	require.Equal(t, "SPEC-0001", res.Next)
	require.Equal(t, 0, *res.CurrentMax)
	require.Equal(t, 0, res.OwnersScanned)
	require.Contains(t, res.Notes, "no notes of this type yet; starting from 1")
}

func TestAllocate_DerivedIdentifierBypassesAllocator(t *testing.T) {
	schema := loadTestSchema(t, `
type Story implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, strategy: SEQUENTIAL, prefix: "STORY", derivedSuffix: "US")
}
type StorySection implements Section {
  stories: [Story!] @contains(level: H3)
}
type Spec @node(paths: ["specs/*.md"]) {
  stories: StorySection @contains(level: H2, heading: "Stories")
}
`)

	_, err := Allocate(context.Background(), schema, &fakeStore{}, "Story")
	require.Error(t, err)
	require.Equal(t, "unsupported_for_type", ErrorCode(err))
	require.Contains(t, err.Error(), "derived")
}

func TestAllocate_PicksMaxPlusOnePreservingGaps(t *testing.T) {
	schema := loadTestSchema(t, `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
  name: String!
}
`)
	store := &fakeStore{
		pathsByType: map[string][]string{
			"Spec": {"specs/a.md", "specs/b.md", "specs/c.md"},
		},
		values: map[string]map[string]string{
			"specs/a.md": {"id": "SPEC-0001"},
			"specs/b.md": {"id": "SPEC-0007"},
			"specs/c.md": {"id": "SPEC-0003"},
		},
	}
	res, err := Allocate(context.Background(), schema, store, "Spec")
	require.NoError(t, err)
	require.Equal(t, "SPEC-0008", res.Next)
	require.Equal(t, []string{"SPEC-0008"}, res.IDs)
	require.Equal(t, 1, res.Count)
	require.Equal(t, "SPEC-0008", res.Last)
	require.Equal(t, 7, *res.CurrentMax)
	require.Equal(t, "SPEC-0007", res.CurrentMaxValue)
	require.Equal(t, 3, res.OwnersScanned)
	require.Empty(t, res.OwnersSkipped)
}

func TestAllocateBatch_ReturnsContiguousIDsFromObservedMax(t *testing.T) {
	schema := loadTestSchema(t, `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
  name: String!
}
`)
	store := &fakeStore{
		pathsByType: map[string][]string{
			"Spec": {"specs/a.md"},
		},
		values: map[string]map[string]string{
			"specs/a.md": {"id": "SPEC-0007"},
		},
	}
	res, err := AllocateBatch(context.Background(), schema, store, "Spec", 3)
	require.NoError(t, err)
	require.Equal(t, "SPEC-0008", res.Next)
	require.Equal(t, []string{"SPEC-0008", "SPEC-0009", "SPEC-0010"}, res.IDs)
	require.Equal(t, 3, res.Count)
	require.Equal(t, "SPEC-0010", res.Last)
	require.Equal(t, 7, *res.CurrentMax)
}

func TestAllocateBatch_ClampsExcessiveCount(t *testing.T) {
	schema := loadTestSchema(t, `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
  name: String!
}
`)
	store := &fakeStore{}
	res, err := AllocateBatch(context.Background(), schema, store, "Spec", MaxBatchCount+1)
	require.NoError(t, err)
	require.Len(t, res.IDs, MaxBatchCount)
	require.Equal(t, MaxBatchCount, res.Count)
	require.Equal(t, "SPEC-0100", res.Last)
}

func TestAllocate_ReportsIdentifierExhaustion(t *testing.T) {
	schema := loadTestSchema(t, `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
  name: String!
}
`)
	store := &fakeStore{
		pathsByType: map[string][]string{"Spec": {"specs/max.md"}},
		values: map[string]map[string]string{
			"specs/max.md": {"id": "SPEC-" + strconv.Itoa(math.MaxInt)},
		},
	}

	_, err := Allocate(context.Background(), schema, store, "Spec")
	require.Error(t, err)
	require.Equal(t, "identifier_exhausted", ErrorCode(err))
}

func TestAllocateRequest_RejectsProspectivePathsForSequentialStrategy(t *testing.T) {
	schema := loadTestSchema(t, `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
  name: String!
}
`)

	result, err := AllocateRequest(context.Background(), schema, &fakeStore{}, Request{
		Type:  "Spec",
		Count: 1,
		Paths: []string{"specs/2026-08-05-14-32-example.md"},
	})
	require.Error(t, err)
	require.Equal(t, "invalid_input", ErrorCode(err))
	require.NotNil(t, result)
	require.Equal(t, ontology.IdentifierStrategySequential, result.Strategy)
	require.Equal(t, []string{"specs/2026-08-05-14-32-example.md"}, result.Paths)
}

func TestAllocateRequest_PreservesSequentialCompatibility(t *testing.T) {
	schema := loadTestSchema(t, `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
  name: String!
}
`)

	result, err := AllocateRequest(context.Background(), schema, &fakeStore{}, Request{Type: "Spec"})
	require.NoError(t, err)
	require.Equal(t, "SPEC-0001", result.Next)
	require.Equal(t, 1, result.Count)
	require.Empty(t, result.Paths)
	require.Empty(t, result.Allocations)
}

func TestAllocateRequest_DateTimeAllocatesFromOrderedPathsAndReservedAliases(t *testing.T) {
	schema := loadTestSchema(t, `
type Effort @node(paths: ["efforts/*.md"]) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
  name: String!
}
`)
	paths := []string{
		"efforts/2026-08-05-14-32-first.md",
		"efforts/2026-08-05-14-32-second.md",
	}

	store := &fakeStore{
		pathsByType: map[string][]string{"Effort": {"efforts/existing.md", "efforts/alias.md"}},
		values: map[string]map[string]string{
			"efforts/existing.md": {"id": "EFF-2026-08-05-14-32", "aliases": "EFF-2026-08-05-14-32"},
			"efforts/alias.md":    {"id": "EFF-2026-08-05-14-31", "aliases": "EFF-2026-08-05-14-32-3"},
		},
	}
	result, err := AllocateRequest(context.Background(), schema, store, Request{
		Type:  "Effort",
		Count: 1,
		Paths: paths,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, ontology.IdentifierStrategyDateTime, result.Strategy)
	require.Equal(t, paths, result.Paths)
	require.Nil(t, result.Pad)
	require.Nil(t, result.CurrentMax)
	require.Equal(t, []string{"EFF-2026-08-05-14-32-2", "EFF-2026-08-05-14-32-4"}, result.IDs)
	require.Equal(t, []Allocation{
		{Path: paths[0], ID: "EFF-2026-08-05-14-32-2", Base: "EFF-2026-08-05-14-32", Disambiguator: intPointer(2)},
		{Path: paths[1], ID: "EFF-2026-08-05-14-32-4", Base: "EFF-2026-08-05-14-32", Disambiguator: intPointer(4)},
	}, result.Allocations)
}

func TestAllocateRequest_DateTimeRejectsInvalidRequests(t *testing.T) {
	schema := loadTestSchema(t, `
type Effort @node(paths: ["efforts/*.md"]) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
`)

	for _, request := range []Request{
		{Type: "Effort", Count: 1},
		{Type: "Effort", Count: 2, Paths: []string{"efforts/2026-08-05-14-32-example.md"}},
		{Type: "Effort", Count: 1, Paths: []string{"efforts/2026-02-29-14-32-example.md"}},
	} {
		result, err := AllocateRequest(context.Background(), schema, &fakeStore{}, request)
		require.Error(t, err)
		require.Equal(t, "invalid_input", ErrorCode(err))
		require.NotNil(t, result)
	}
}

func TestAllocateRequest_DateTimeReservesAliasesAcrossSiblingTypes(t *testing.T) {
	schema := loadTestSchema(t, `
type Effort @node(paths: ["efforts/*.md"]) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
type ArchivedEffort @node(paths: ["archive/*.md"]) {
  legacyId: String! @field(source: "legacy-id") @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
`)
	store := &fakeStore{
		pathsByType: map[string][]string{
			"ArchivedEffort": {"archive/existing.md"},
		},
		values: map[string]map[string]string{
			"archive/existing.md": {
				"legacy-id": "EFF-2026-08-05-14-32",
				"aliases":   "EFF-2026-08-05-14-32-2",
			},
		},
	}

	result, err := AllocateRequest(context.Background(), schema, store, Request{
		Type:  "Effort",
		Count: 1,
		Paths: []string{"efforts/2026-08-05-14-32-new.md"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"EFF-2026-08-05-14-32-3"}, result.IDs)
	require.Equal(t, []string{"ArchivedEffort"}, result.SharedWith)
}

func TestAllocate_SkipsOffPatternValues(t *testing.T) {
	schema := loadTestSchema(t, `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
  name: String!
}
`)
	store := &fakeStore{
		pathsByType: map[string][]string{
			"Spec": {"specs/legacy.md", "specs/new.md", "specs/junk.md"},
		},
		values: map[string]map[string]string{
			"specs/legacy.md": {"id": "SPEC-0001a"},
			"specs/new.md":    {"id": "SPEC-0002"},
			"specs/junk.md":   {"id": "OTHER-0099"},
		},
	}
	res, err := Allocate(context.Background(), schema, store, "Spec")
	require.NoError(t, err)
	require.Equal(t, "SPEC-0003", res.Next)
	require.Len(t, res.OwnersSkipped, 2)
	require.Contains(t, res.Notes[0], "2 off-pattern identifier value(s) ignored")
}

func TestAllocate_TypeNotFound(t *testing.T) {
	schema := loadTestSchema(t, `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
  name: String!
}
`)
	_, err := Allocate(context.Background(), schema, &fakeStore{}, "Effort")
	require.Error(t, err)
	require.Equal(t, "type_not_found", ErrorCode(err))
	require.True(t, errors.Is(err, ErrTypeNotFound))
}

func TestAllocate_NoPreferredIdentifier(t *testing.T) {
	schema := loadTestSchema(t, `
type Card @node(paths: ["cards/*.md"]) {
  name: String!
}
`)
	_, err := Allocate(context.Background(), schema, &fakeStore{}, "Card")
	require.Error(t, err)
	require.Equal(t, "no_preferred_identifier", ErrorCode(err))
}

func TestAllocate_UnsupportedWhenPrefixOmitted(t *testing.T) {
	schema := loadTestSchema(t, `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true)
  name: String!
}
`)
	_, err := Allocate(context.Background(), schema, &fakeStore{}, "Spec")
	require.Error(t, err)
	require.Equal(t, "unsupported_for_type", ErrorCode(err))
}

func TestAllocate_RuntimeMissing(t *testing.T) {
	_, err := Allocate(context.Background(), nil, nil, "Spec")
	require.Error(t, err)
	require.Equal(t, "runtime_unavailable", ErrorCode(err))
}

// SPEC-0006 explicitly shares the SPEC-XXXX number-line across all spec
// variants. The allocator must pool sibling-type ids before picking max+1,
// otherwise sibling specs will hand out the same number.
func TestAllocate_SharedNumberLineAcrossSiblingTypes(t *testing.T) {
	schema := loadTestSchema(t, `
type ProcessSpec @node(paths: ["specs/process/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
  name: String!
}
type TechnicalSpec @node(paths: ["specs/technical/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
  name: String!
}
`)
	store := &fakeStore{
		pathsByType: map[string][]string{
			"ProcessSpec":   {"specs/process/a.md"},
			"TechnicalSpec": {"specs/technical/b.md", "specs/technical/c.md"},
		},
		values: map[string]map[string]string{
			"specs/process/a.md":   {"id": "SPEC-0001"},
			"specs/technical/b.md": {"id": "SPEC-0007"},
			"specs/technical/c.md": {"id": "SPEC-0009"},
		},
	}
	res, err := Allocate(context.Background(), schema, store, "ProcessSpec")
	require.NoError(t, err)
	require.Equal(t, "SPEC-0010", res.Next, "max must come from the cross-type pool, not just ProcessSpec")
	require.Equal(t, 9, *res.CurrentMax)
	require.Equal(t, []string{"TechnicalSpec"}, res.SharedWith)
	require.Equal(t, 3, res.OwnersScanned)
}

func TestAllocateStarterEffortImplementations(t *testing.T) {
	body, err := os.ReadFile("../../app/cli/init/templates/starters/agentic-engineering/rhizome/ontology/spec-driven.graphql")
	require.NoError(t, err)
	schema := loadTestSchema(t, string(body))
	for _, typeName := range []string{"EffortNote", "EffortWorkspace"} {
		t.Run(typeName, func(t *testing.T) {
			store := &fakeStore{
				pathsByType: map[string][]string{"EffortNote": {"docs/efforts/historical.md"}},
				values: map[string]map[string]string{
					"docs/efforts/historical.md": {"id": "EFF-2026-08-05-14-32"},
				},
			}
			result, err := AllocateRequest(context.Background(), schema, store, Request{
				Type: typeName, Count: 1, Paths: []string{"docs/efforts/example/2026-08-05-14-32-effort.html"},
			})
			require.NoError(t, err)
			require.Equal(t, "EFF-2026-08-05-14-32-2", result.Next)
			require.Len(t, result.SharedWith, 1)
		})
	}
}
