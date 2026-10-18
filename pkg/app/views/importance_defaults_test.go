package views

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func TestGeneratedDefaultsAreNamedForTheTypeLabel(t *testing.T) {
	schema := &ontoschema.Schema{
		Types: map[string]*ontoschema.NoteType{
			"IPInitiative": {Name: "IPInitiative", Label: "IPInitiative"},
			"Effort":       {Name: "Effort", Label: "Effort", PluralLabel: "Efforts"},
			"Meeting":      {Name: "Meeting", Label: "Team meeting", LabelAuthored: true},
		},
		Interfaces: map[string]*ontoschema.InterfaceType{
			"WorkItem":  {Name: "WorkItem"},
			"Traceable": {Name: "Traceable", Label: "Traceable note", PluralLabel: "Traceable notes"},
		},
	}
	names := map[string]string{}
	for _, def := range generatedDefaults(schema) {
		names[def.ID] = def.Name
	}
	require.Equal(t, map[string]string{
		viewconfig.GeneratedTypeID("IPInitiative"):   "IP initiative",
		viewconfig.GeneratedTypeID("Effort"):         "Efforts",
		viewconfig.GeneratedTypeID("Meeting"):        "Team meeting",
		viewconfig.GeneratedInterfaceID("WorkItem"):  "Work item",
		viewconfig.GeneratedInterfaceID("Traceable"): "Traceable notes",
	}, names)
}

func TestGeneratedDefaultsDoNotUseDetailSummaryAsCardPreview(t *testing.T) {
	noteType := &ontoschema.NoteType{
		Name: "ProductSpec",
		Fields: []*ontoschema.Field{
			{Name: "summary", Kind: ontoschema.FieldKindScalar, TypeName: "String", Display: ontoschema.FieldDisplay{Importance: ontoschema.FieldImportanceDetail}},
			{Name: "priority", Kind: ontoschema.FieldKindEnum, TypeName: "Priority", Display: ontoschema.FieldDisplay{Importance: ontoschema.FieldImportanceKey}},
		},
	}
	schema := &ontoschema.Schema{
		Types:     map[string]*ontoschema.NoteType{"ProductSpec": noteType},
		EnumTypes: map[string]*ontoschema.EnumType{"Priority": {Name: "Priority", Values: []*ontoschema.EnumValue{{Name: "high"}, {Name: "low"}}}},
	}
	profile, ok := subjectProfile(schema, "ProductSpec")
	require.True(t, ok)
	require.Equal(t, "summary", profile.SummaryField)

	def := generatedTypeDefault(schema, "ProductSpec", noteType, profile)

	require.Empty(t, def.Variants.Card.Preview)
	require.Equal(t, []viewconfig.ViewColumn{
		{Field: "title"},
		{Field: "priority", Label: "Priority"},
		{Field: "updatedAt", Label: "Changed"},
	}, def.Variants.Table.Columns)
}

func TestImplicitViewFieldsUseImportanceWhileAuthoredListsStayExact(t *testing.T) {
	capabilities := []FieldCapability{
		{Key: "title", CanonicalField: "title", SemanticRole: "title", Importance: ontoschema.FieldImportanceNormal},
		{Key: "id", CanonicalField: "id", SemanticRole: "identifier", Importance: ontoschema.FieldImportanceDetail, schemaOrder: 2, schemaOrderKnown: true},
		{Key: "summary", CanonicalField: "summary", Importance: ontoschema.FieldImportanceDetail, schemaOrder: 0, schemaOrderKnown: true},
		{Key: "priority", CanonicalField: "priority", Importance: ontoschema.FieldImportanceKey, schemaOrder: 3, schemaOrderKnown: true},
		{Key: "owner", CanonicalField: "owner", Importance: ontoschema.FieldImportanceKey, schemaOrder: 1, schemaOrderKnown: true},
		{Key: "status", CanonicalField: "status", Importance: ontoschema.FieldImportanceNormal, schemaOrder: 4, schemaOrderKnown: true},
	}
	implicit := viewconfig.ViewDefinition{Variants: viewconfig.VariantSet{Card: &viewconfig.CardSpec{}}}

	require.Equal(t, []TableColumn{
		{Field: "title", Label: "Title"},
		{Field: "id", Label: "ID"},
		{Field: "owner", Label: "Owner"},
		{Field: "priority", Label: "Priority"},
		{Field: "updatedAt", Label: "Changed"},
		{Field: "status", Label: "Status"},
	}, tableColumns(implicit, capabilities))

	card := resolveCardLayout(implicit, "card", capabilities)
	require.Equal(t, "title", card.Title.Field)
	require.Equal(t, "id", card.Eyebrow.Field)
	require.Nil(t, card.Preview)
	require.Equal(t, []TableColumn{
		{Field: "owner", Label: "Owner"},
		{Field: "priority", Label: "Priority"},
		{Field: "status", Label: "Status"},
	}, card.Fields)

	authored := viewconfig.ViewDefinition{Variants: viewconfig.VariantSet{
		Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "summary", Label: "Authored summary"}, {Field: "title"}}},
		Card:  &viewconfig.CardSpec{Fields: []viewconfig.ViewColumn{}},
	}}
	require.Equal(t, []TableColumn{
		{Field: "summary", Label: "Authored summary"},
		{Field: "title", Label: "Title"},
	}, tableColumns(authored, capabilities))
	card = resolveCardLayout(authored, "card", capabilities)
	require.Empty(t, card.Fields)
}

func TestImplicitNormalOnlyViewFieldsKeepLegacyDefaults(t *testing.T) {
	capabilities := []FieldCapability{
		{Key: "title", CanonicalField: "title", SemanticRole: "title", Importance: ontoschema.FieldImportanceNormal},
		{Key: "id", CanonicalField: "id", SemanticRole: "identifier", Importance: ontoschema.FieldImportanceNormal},
		{Key: "summary", CanonicalField: "summary", Importance: ontoschema.FieldImportanceNormal},
	}
	def := viewconfig.ViewDefinition{Variants: viewconfig.VariantSet{Card: &viewconfig.CardSpec{}}}

	require.Equal(t, []TableColumn{{Field: "title", Label: "Title"}}, tableColumns(def, capabilities))
}
