package noderead

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/stretchr/testify/require"
)

type shapeFixtureStore struct {
	snapshot readmodel.ShapeSnapshot
	fields   []string
	links    bool
}

func (s *shapeFixtureStore) OntologyShapeSnapshot(_ context.Context, fields []string, links bool, _ int) (readmodel.ShapeSnapshot, error) {
	s.fields = fields
	s.links = links
	return s.snapshot, nil
}

func TestOntologyShapeContract(t *testing.T) {
	schema := &ontology.Schema{
		Types: map[string]*ontology.NoteType{
			"Idea": {Name: "Idea", Role: ontology.TypeRoleNote, Implements: []string{"Work"}, Fields: []*ontology.Field{
				{Name: "phase", Kind: ontology.FieldKindEnum, TypeName: "Stage"},
				{Name: "nextStep", Kind: ontology.FieldKindScalar, TypeName: "String", Display: ontology.FieldDisplay{Importance: ontology.FieldImportanceKey}},
				{Name: "opportunities", Kind: ontology.FieldKindLink, TypeName: "Opportunity"},
				{Name: "otherWork", Kind: ontology.FieldKindLink, TypeName: "Work"},
				{Name: "related", Kind: ontology.FieldKindLink, TypeName: "Note"},
				{Name: "siblings", Kind: ontology.FieldKindLink, TypeName: "Idea"},
			}},
			"Opportunity": {Name: "Opportunity", Role: ontology.TypeRoleNote, Implements: []string{"Work"}},
			"Empty":       {Name: "Empty", Role: ontology.TypeRoleNote},
			"Fragment":    {Name: "Fragment", Role: ontology.TypeRoleEmbeddedNode, Implements: []string{"Work"}},
		},
		Interfaces: map[string]*ontology.InterfaceType{"Work": {Name: "Work", Fields: []*ontology.Field{
			{Name: "state", Kind: ontology.FieldKindEnum, TypeName: "Stage"},
			{Name: "rationale", Kind: ontology.FieldKindScalar, TypeName: "String", Display: ontology.FieldDisplay{Importance: ontology.FieldImportanceKey}},
		}}},
		EnumTypes: map[string]*ontology.EnumType{"Stage": {Name: "Stage", Values: []*ontology.EnumValue{{Name: "open", View: ontology.EnumValueView{Stage: ontology.StageOpen}}, {Name: "active", View: ontology.EnumValueView{Stage: ontology.StageActive}}}}},
	}
	store := &shapeFixtureStore{snapshot: readmodel.ShapeSnapshot{
		Notes: []readmodel.ShapeNote{
			{Path: "Notes/i1.md", Type: "Idea", NodeID: "i1", Changed: 10, HasIssues: true},
			{Path: "Notes/i2.md", Type: "Idea", NodeID: "i2", Changed: 20},
			{Path: "Other/o1.md", Type: "Opportunity", NodeID: "o1", Changed: 30},
			{Path: "Other/o2.md", Type: "Opportunity", NodeID: "o2", Changed: 40},
			{Path: "Notes/u.md", NodeID: "u", Changed: 50, Ambiguous: true},
			{Path: "root.md", NodeID: "root", Changed: 60},
		},
		Fields: []readmodel.ShapeField{
			{Path: "Notes/i1.md", Field: "phase", Value: "open"}, {Path: "Notes/i2.md", Field: "phase", Value: "active"},
			{Path: "Notes/i1.md", Field: "nextstep", Value: "write"},
			{Path: "Notes/i1.md", Field: "state", Value: "active"}, {Path: "Other/o1.md", Field: "state", Value: "open"},
			{Path: "Other/o1.md", Field: "rationale", Value: "why"}, {Path: "Notes/i2.md", Field: "rationale", Value: " "},
		},
		Edges: []readmodel.ShapeEdge{
			{Source: "Notes/i1.md", Target: "Other/o1.md", SourceNode: "i1", TargetNode: "o1", Field: "opportunities", Relation: true},
			{Source: "Notes/i1.md", Target: "Other/o1.md", Field: "otherWork", Relation: true},
			{Source: "Other/o1.md", Target: "Notes/i1.md"},                                   // mixed evidence, counted once
			{Source: "Notes/i2.md", Target: "Other/o2.md", Field: "related", Relation: true}, // broad field is plain
			{Source: "Notes/i1.md", Target: "Notes/u.md"},
			{Source: "Notes/u.md", Target: "Other/o1.md"},
			{Source: "Notes/i1.md", Target: "Notes/i2.md", Field: "siblings", Relation: true},
			{Source: "Notes/i1.md", Target: "Other/o2.md", SourceNode: "fragment", Field: "opportunities", Relation: true}, // embedded endpoint excluded
			{Source: "Notes/i1.md", Target: "Notes/i1.md", Field: "siblings", Relation: true},                              // reflexive note pair also stays in the type diagonal
			{Source: "root.md", Target: "Notes/u.md"},
		},
	}}
	assessment, err := json.Marshal(ontology.NoteAssessment{NotePath: "Notes/u.md", CandidateTypes: []string{"Idea", "Opportunity"}})
	require.NoError(t, err)
	store.snapshot.Notes[4].Ambiguous = false
	store.snapshot.Notes[4].AssessmentJSON = string(assessment)
	shape, err := OntologyShape(t.Context(), store, schema, ShapeParts{Members: true, Links: true, Folders: true})
	require.NoError(t, err)
	require.Equal(t, 6, shape.TotalNotes)
	require.Equal(t, 4, shape.TypedNotes)
	require.Equal(t, 2, shape.UntypedNotes)
	require.Equal(t, 1, shape.AmbiguousNotes)
	require.False(t, shape.Rebuilding)
	require.Len(t, shape.Members.Types, 3)
	idea := shape.Members.Types[1]
	require.Equal(t, "Idea", idea.Name)
	require.Equal(t, 2, idea.Count)
	require.Zero(t, idea.IssueCount, "the HTTP layer supplies published validation scope counts")
	require.EqualValues(t, 20, idea.LastChanged)
	require.Equal(t, &ShapeLifecycle{Field: "phase", Values: []ShapeValue{{Name: "active", Count: 1}, {Name: "open", Count: 1}}}, idea.Lifecycle)
	require.Equal(t, []ShapeGap{{Field: "nextStep", Empty: 1}}, idea.Gaps)
	require.Equal(t, []ShapeTargetSet{{Types: []string{"Opportunity"}, Records: 1}, {Types: []string{"Opportunity", UntypedShapeMember}, Records: 1}}, idea.TargetSets)
	require.Nil(t, shape.Members.Types[0].Lifecycle)
	require.Zero(t, shape.Members.Types[0].Count)
	iface := shape.Members.Interfaces[0]
	require.Equal(t, []string{"Idea", "Opportunity"}, iface.Implementors)
	require.Equal(t, 4, iface.Count)
	require.Equal(t, &ShapeLifecycle{Field: "state", Values: []ShapeValue{{Name: "active", Count: 1}, {Name: "open", Count: 1}}}, iface.Lifecycle)
	require.Equal(t, []ShapeGap{{Field: "rationale", Empty: 3}}, iface.Gaps)
	require.Equal(t, 3, shape.Members.Untyped.Links)
	require.Equal(t, []ShapePair{
		{A: "Idea", B: "Idea", Links: 2, RelationLinks: 2, Fields: []ShapePairField{{Type: "Idea", Field: "siblings", Count: 2}}},
		{A: "Idea", B: "Opportunity", Links: 2, RelationLinks: 1, PlainLinks: 1, Fields: []ShapePairField{{Type: "Idea", Field: "opportunities", Count: 1}, {Type: "Idea", Field: "otherWork", Count: 1}}},
		{A: "Idea", B: UntypedShapeMember, Links: 1, PlainLinks: 1, Fields: []ShapePairField{}},
		{A: "Opportunity", B: UntypedShapeMember, Links: 1, PlainLinks: 1, Fields: []ShapePairField{}},
		{A: UntypedShapeMember, B: UntypedShapeMember, Links: 1, PlainLinks: 1, Fields: []ShapePairField{}},
	}, shape.Links.Pairs)
	require.Equal(t, []ShapeFolder{
		{Folder: "", Total: 1, Untyped: 1, Typed: map[string]int{}, UntypedLinksTo: map[string]int{}},
		{Folder: "Notes", Total: 3, Untyped: 1, Typed: map[string]int{"Idea": 2}, UntypedLinksTo: map[string]int{"Idea": 1, "Opportunity": 1}},
		{Folder: "Other", Total: 2, Typed: map[string]int{"Opportunity": 2}, UntypedLinksTo: map[string]int{}},
	}, shape.Folders.Rows)
	require.Contains(t, store.fields, "rationale")
	require.Contains(t, store.fields, "nextStep")
	require.True(t, store.links)
	store.snapshot.Rebuilding = true
	partial, err := OntologyShape(t.Context(), store, schema, ShapeParts{Folders: true})
	require.NoError(t, err)
	require.True(t, partial.Rebuilding)
	require.Nil(t, partial.Members)
	require.Nil(t, partial.Links)
	require.NotNil(t, partial.Folders)
	require.Empty(t, store.fields)
}
