package views

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func TestExecuteMissingFilterMatchesRowsWithoutAValue(t *testing.T) {
	target := ontology.NodeRef{NotePath: "people/ann.md", Kind: ontology.NodeKindNote}
	rows := []TableRow{
		{Ref: ontology.NodeRef{NotePath: "a.md"}, Title: "Filled", Fields: map[string]any{"summary": "Text", "status": "open", "owner": "[[Ann]]"},
			RelationValues: map[string][]TableRelationValue{"owner": {{Value: "[[Ann]]", Ref: &target}}}},
		{Ref: ontology.NodeRef{NotePath: "b.md"}, Title: "Blank", Fields: map[string]any{"summary": "  ", "status": "", "owner": []any{}}},
		{Ref: ontology.NodeRef{NotePath: "c.md"}, Title: "Absent", Fields: map[string]any{}},
	}
	service := New(ServiceOptions{
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "work",
			Name:       "Work",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Defaults:   viewconfig.DefaultsSpec{Sort: []viewconfig.SortSpec{{Field: "title"}}},
			Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
		}},
		SourceResolver: SourceResolverFunc(func(context.Context, viewconfig.ViewDefinition, ExecuteRequest) (SourceResult, error) {
			return SourceResult{
				Rows: append([]TableRow(nil), rows...),
				Capabilities: []FieldCapability{
					{Key: "summary", ValueKind: "string", cardinalityKnown: true},
					{Key: "status", ValueKind: "enum", EnumValues: []FieldEnumValue{{Value: "open"}}, cardinalityKnown: true},
					{Key: "owner", ValueKind: "relation", List: true, cardinalityKnown: true},
				},
			}, nil
		}),
	})

	for _, field := range []string{"summary", "status", "owner"} {
		t.Run(field, func(t *testing.T) {
			resp, err := service.Execute(context.Background(), "work", ExecuteRequest{
				Filters: []viewconfig.FilterSpec{{Field: field, Op: "missing"}},
			})
			require.NoError(t, err)
			require.Equal(t, []string{"Absent", "Blank"}, rowTitles(resp.Rows))
		})
	}
}

func TestOntologyFieldCapabilitiesCarryPolicyReasonAndMissing(t *testing.T) {
	schema := &ontology.Schema{Types: map[string]*ontology.NoteType{"Spec": {
		Name: "Spec",
		Fields: []*ontology.Field{{
			Name: "owner", Kind: ontology.FieldKindLink, TypeName: "Person", SourceKind: ontology.FieldSourceFrontmatter,
			Policy: &ontology.PolicyHint{Reason: "Assigned at kickoff"},
		}},
	}}}
	resolver := defaultSourceResolver{opts: ServiceOptions{Schema: schema}}

	caps := resolver.capabilitiesForField(context.Background(), nil, schema.Types["Spec"].Fields[0])

	require.NotEmpty(t, caps)
	for _, capability := range caps {
		require.Equal(t, "Assigned at kickoff", capability.PolicyReason)
		require.Contains(t, capability.FilterOps, "missing")
		require.NotContains(t, capability.IndexedFilterOps, "missing")
	}
}
