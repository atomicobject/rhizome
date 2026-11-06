package views

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNestedFrontmatterDisplayAndCapability(t *testing.T) {
	field := &ontoschema.Field{Name: "speaker", SourcePath: "provenance.speaker", SourceKind: ontoschema.FieldSourceFrontmatter, Kind: ontoschema.FieldKindScalar, TypeName: "String"}
	noteType := &ontoschema.NoteType{Fields: []*ontoschema.Field{field}}
	for _, record := range []noderead.NodeRecord{
		{Frontmatter: map[string]any{"provenance": map[string]any{"speaker": "Nested"}, "provenance.speaker": "Literal"}},
		{FieldValues: map[string][]readmodel.FieldValueRow{"speaker": {{ValueText: "Nested"}}}},
	} {
		fields := map[string]any{}
		addSemanticFields(fields, noteType, record)
		require.Equal(t, "Nested", fields["speaker"])
	}
	caps := (defaultSourceResolver{}).capabilitiesForField(context.Background(), nil, field)
	require.Len(t, caps, 1)
	require.Nil(t, caps[0].Edit)
	require.Equal(t, "Nested frontmatter paths are read-only.", caps[0].PolicyReason)
}
