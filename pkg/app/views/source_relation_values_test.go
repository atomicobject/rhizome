package views

import (
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRelationValuesForRecordPreservesOrderAndResolvedIdentity(t *testing.T) {
	alice := ontology.NodeRef{NotePath: "people/alice.md", Kind: ontology.NodeKindNote, TypeName: "Person"}
	aliceJSON, err := json.Marshal(alice)
	require.NoError(t, err)
	record := noderead.NodeRecord{
		TypeName: "ActionItem",
		FieldValues: map[string][]readmodel.FieldValueRow{
			"assignedto": {
				{ValueText: "[[people/alice|Aliased Alice]]", TargetRefJSON: string(aliceJSON), ListOrdinal: 0},
				{ValueText: "[[people/missing|Missing person]]", ListOrdinal: 1},
			},
		},
	}
	schema := &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{
		"ActionItem": {
			Name: "ActionItem",
			Fields: []*ontoschema.Field{
				{Name: "assignedTo", Kind: ontoschema.FieldKindLink, TypeName: "Person", List: true},
			},
		},
	}}
	values := relationValuesForRecord(schema, record)["assignedTo"]
	require.Equal(t, []TableRelationValue{
		{Value: "[[people/alice|Aliased Alice]]", Ref: &alice},
		{Value: "[[people/missing|Missing person]]"},
	}, values)

	rows := []TableRow{{RelationValues: map[string][]TableRelationValue{"assignedTo": values}}}
	require.Equal(t, []ontology.NodeRef{alice}, relationTargetRefs(nil, map[string]struct{}{}, rows, nil))
	hydrated := applyRelationTargetTitles(rows, []noderead.NodeRecord{{Ref: alice, Title: "Alice Example"}})
	require.Equal(t, "Alice Example", hydrated[0].RelationValues["assignedTo"][0].Title)
}

func TestRelationValueHydrationPreservesStructuralIdentity(t *testing.T) {
	first := ontology.NodeRef{NotePath: "notes/items.md", Kind: ontology.NodeKindEmbedded, Structural: "first"}
	second := ontology.NodeRef{NotePath: "notes/items.md", Kind: ontology.NodeKindEmbedded, Structural: "second"}
	rows := []TableRow{{RelationValues: map[string][]TableRelationValue{
		"related": {
			{Value: "first", Ref: &first},
			{Value: "second", Ref: &second},
		},
	}}}

	require.ElementsMatch(t, []ontology.NodeRef{first, second}, relationTargetRefs(nil, map[string]struct{}{}, rows, nil))
	hydrated := applyRelationTargetTitles(rows, []noderead.NodeRecord{
		{Ref: first, Title: "First item"},
		{Ref: second, Title: "Second item"},
	})
	require.Equal(t, "First item", hydrated[0].RelationValues["related"][0].Title)
	require.Equal(t, "Second item", hydrated[0].RelationValues["related"][1].Title)
	require.Equal(t, "first", hydrated[0].RelationValues["related"][0].Ref.Structural)
	require.Equal(t, "second", hydrated[0].RelationValues["related"][1].Ref.Structural)
}

func TestRawRelationFieldValuesReadsOverlaySources(t *testing.T) {
	record := noderead.NodeRecord{
		Frontmatter: map[string]any{"reviewers": []any{"[[people/alice]]", "[[people/bob]]"}},
		InlineProps: map[string][]string{"owner": {"[[people/carol]]"}},
	}

	require.Equal(t, []string{"[[people/alice]]", "[[people/bob]]"}, rawRelationFieldValues(record, &ontoschema.Field{
		Name: "reviewers", Kind: ontoschema.FieldKindLink, Source: "reviewers", SourceKind: ontoschema.FieldSourceFrontmatter,
	}))
	require.Equal(t, []string{"[[people/carol]]"}, rawRelationFieldValues(record, &ontoschema.Field{
		Name: "owner", Kind: ontoschema.FieldKindLink, Source: "owner", SourceKind: ontoschema.FieldSourceInline,
	}))
}

func TestExistingRelationRefsByInputSkipsMissingOverlayTargets(t *testing.T) {
	existing := ontology.NodeRef{NotePath: "people/alice.md", Kind: ontology.NodeKindNote}
	refs := existingRelationRefsByInput([]noderead.ResolvedNode{
		{
			Input:  "[[people/alice]]",
			Ref:    ontology.NodeRef{NotePath: "people/alias.md", Kind: ontology.NodeKindNote},
			Record: noderead.NodeRecord{Path: "people/alice.md", Ref: existing},
		},
		{
			Input: "[[people/missing]]",
			Ref:   ontology.NodeRef{NotePath: "people/missing.md", Kind: ontology.NodeKindNote},
		},
	})

	require.Equal(t, map[string]ontology.NodeRef{"[[people/alice]]": existing}, refs)
}
