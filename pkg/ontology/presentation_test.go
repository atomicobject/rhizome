package ontology

import (
	"testing"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
)

func TestIdentifierFieldName(t *testing.T) {
	first := &ontoschema.Field{Name: "first", IsIdentifier: true}
	preferred := &ontoschema.Field{Name: "preferred", IsIdentifier: true, IsPreferredIdentifier: true}
	for _, tc := range []struct {
		name     string
		noteType *ontoschema.NoteType
		want     string
	}{
		{"nil", nil, ""},
		{"none", &ontoschema.NoteType{}, ""},
		{"first", &ontoschema.NoteType{Fields: []*ontoschema.Field{nil, first}}, "first"},
		{"preferred", &ontoschema.NoteType{Fields: []*ontoschema.Field{first, preferred}}, "preferred"},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, IdentifierFieldName(tc.noteType)) })
	}
}

func TestInterpolatePreviewTemplate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		missing func(string) string
		want    string
	}{
		{"empty missing value", func(string) string { return "" }, "Heading: ready /  /  / {{not-valid}}"},
		{"literal missing value", func(placeholder string) string { return placeholder }, "Heading: ready / {{ optional }} /  / {{not-valid}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, InterpolatePreviewTemplate("{{ title }}: {{status}} / {{ optional }} / {{empty}} / {{not-valid}}", map[string]string{"title": "Ignored", "status": "ready", "empty": ""}, "Heading", tc.missing))
		})
	}
}
