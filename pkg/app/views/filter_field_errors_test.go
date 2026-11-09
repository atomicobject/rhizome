package views

import (
	"context"
	"testing"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

// SPEC-0136 R8: a view filter that would silently match nothing is an error
// naming the field, as in GraphQL; fields views can evaluate stay filterable.
func TestViewFiltersRejectUnknownFieldsAndUncountableComputedRelations(t *testing.T) {
	schema := &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{
		"Task": {Name: "Task", Implements: []string{"Work"}, Fields: []*ontoschema.Field{
			{Name: "summary", Kind: ontoschema.FieldKindScalar, TypeName: "String", SourceKind: ontoschema.FieldSourceFrontmatter},
			{Name: "meetings", Kind: ontoschema.FieldKindReverse, TypeName: "Meeting", List: true},
			{Name: "lead", Kind: ontoschema.FieldKindNeighbor, TypeName: "Person"},
		}},
		"Bug": {Name: "Bug", Implements: []string{"Work"}, Fields: []*ontoschema.Field{
			{Name: "severity", Kind: ontoschema.FieldKindScalar, TypeName: "String", SourceKind: ontoschema.FieldSourceFrontmatter},
		}},
	}}
	resolver := defaultSourceResolver{opts: ServiceOptions{Schema: schema}}
	typeView := viewconfig.ViewDefinition{ID: "tasks", SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Task"}}
	interfaceView := viewconfig.ViewDefinition{ID: "work", SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyInterface, Interface: "Work"}}
	resolve := func(view viewconfig.ViewDefinition, field string) error {
		_, err := resolver.ResolveSource(context.Background(), view, ExecuteRequest{Filters: []viewconfig.FilterSpec{{Field: field, Op: "exists"}}})
		return err
	}

	for _, field := range []string{"summary", "Summary", "frontmatter.summary", "frontmatter.undeclared", "inline.anything", "title", "path", "notePath", "resolvedType", "updatedAt", "hasIssues", "tags", "stale", "meetings"} {
		require.NoError(t, resolve(typeView, field), field)
	}
	require.NoError(t, resolve(interfaceView, "severity"), "a field one implementor declares")

	for field, message := range map[string]string{
		"nope": `unknown filter field "nope"`,
		"lead": `filter field "lead" is a computed @neighbors or @reverse relation`,
	} {
		err := resolve(typeView, field)
		require.ErrorIs(t, err, ErrInvalidRequest, field)
		require.ErrorContains(t, err, message)
	}
	require.ErrorContains(t, resolve(interfaceView, "nope"), `unknown filter field "nope"`)
}
