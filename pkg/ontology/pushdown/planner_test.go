package pushdown

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func newTestSchema() (*ontology.Schema, *ontology.NoteType) {
	noteType := &ontology.NoteType{
		Name: "TechnicalSpec",
		Fields: []*ontology.Field{
			{Name: "specStatus", Kind: ontology.FieldKindEnum, TypeName: "SpecStatus", Source: "status", SourceKind: ontology.FieldSourceFrontmatter},
			{Name: "owner", Kind: ontology.FieldKindScalar, TypeName: "String", Source: "owner", SourceKind: ontology.FieldSourceInline},
			{Name: "priority", Kind: ontology.FieldKindScalar, TypeName: "Int", Source: "priority", SourceKind: ontology.FieldSourceFrontmatter},
			{Name: "due", Kind: ontology.FieldKindScalar, TypeName: "Date", Source: "due", SourceKind: ontology.FieldSourceFrontmatter},
			{Name: "linkedSpec", Kind: ontology.FieldKindLink, TypeName: "TechnicalSpec", Source: "linked", SourceKind: ontology.FieldSourceFrontmatter},
		},
	}
	noteType.ByName = map[string]*ontology.Field{}
	for _, field := range noteType.Fields {
		noteType.ByName[field.Name] = field
	}
	schema := &ontology.Schema{
		Types: map[string]*ontology.NoteType{"TechnicalSpec": noteType},
	}
	return schema, noteType
}

func TestViewResolverHandlesPrefixedAndCanonicalKeys(t *testing.T) {
	_, noteType := newTestSchema()
	r := ViewResolver{NoteType: noteType}

	cases := []struct {
		key       string
		canonical string
		source    FieldKeySource
	}{
		{"specStatus", "specStatus", FieldKeySchema},
		{"frontmatter.status", "specStatus", FieldKeyFrontmatter},
		{"inline.owner", "owner", FieldKeyInline},
		{"path", "path", FieldKeyBuiltin},
		{"notePath", "notePath", FieldKeyBuiltin},
		{"SPECSTATUS", "specStatus", FieldKeySchema}, // case-insensitive scan
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			key, _, ok := r.Resolve(tc.key)
			require.True(t, ok, "expected resolve for %q", tc.key)
			require.Equal(t, tc.canonical, key.Canonical)
			require.Equal(t, tc.source, key.Source)
		})
	}

	_, _, ok := r.Resolve("frontmatter.unknown")
	require.False(t, ok)
}

func TestSchemaResolverIsCanonicalOnly(t *testing.T) {
	_, noteType := newTestSchema()
	r := SchemaResolver{NoteType: noteType}

	key, _, ok := r.Resolve("specStatus")
	require.True(t, ok)
	require.Equal(t, "specStatus", key.Canonical)

	_, _, ok = r.Resolve("frontmatter.status")
	require.False(t, ok, "schema resolver must not honor frontmatter. prefix")

	_, _, ok = r.Resolve("SPECSTATUS")
	require.False(t, ok, "schema resolver must be case-sensitive")

	key, _, ok = r.Resolve("notePath")
	require.True(t, ok)
	require.True(t, key.IsBuiltin())
}

func TestPlannerPredicateRoundTripsTypedValues(t *testing.T) {
	schema, noteType := newTestSchema()
	planner := Planner{Schema: schema, Resolver: ViewResolver{NoteType: noteType}}

	t.Run("int eq", func(t *testing.T) {
		result, ok := planner.BuildPredicate(context.Background(), FilterInput{Field: "frontmatter.priority", Op: "gte", Value: "3"})
		require.True(t, ok)
		require.Equal(t, "priority", result.Predicate.FieldName)
		require.Equal(t, codeanchor.OntologyFieldOperator("gte"), result.Predicate.Op)
		require.Len(t, result.Predicate.Values, 1)
		require.NotNil(t, result.Predicate.Values[0].ValueInt)
		require.Equal(t, int64(3), *result.Predicate.Values[0].ValueInt)
	})

	for _, raw := range []string{"specs/foo", "[[specs/foo]]", "[[specs/foo|Foo]]"} {
		t.Run("link "+raw, func(t *testing.T) {
			result, ok := planner.BuildPredicate(context.Background(), FilterInput{Field: "linkedSpec", Op: "eq", Value: raw})
			require.True(t, ok)
			require.Equal(t, "linkedSpec", result.Predicate.FieldName)
			require.Equal(t, "specs/foo", result.Predicate.Values[0].TargetNotePath)
		})
	}
	for _, path := range []string{"docs/spec.md", "docs/spec", "docs/reference.html"} {
		t.Run("path "+path, func(t *testing.T) {
			result, ok := planner.BuildPredicate(context.Background(), FilterInput{Field: "path", Op: "eq", Value: path})
			require.True(t, ok)
			require.Equal(t, codeanchor.OntologyFieldOpEq, result.Predicate.Op)
			require.Len(t, result.Predicate.Values, 1)
			require.Equal(t, path, result.Predicate.Values[0].ValueText)
			require.Equal(t, path, result.Predicate.Values[0].ValueNorm)
		})
	}
	t.Run("empty path", func(t *testing.T) {
		_, ok := planner.BuildPredicate(context.Background(), FilterInput{Field: "path", Op: "eq", Value: ""})
		require.False(t, ok)
	})

	t.Run("unsupported op rejected", func(t *testing.T) {
		_, ok := planner.BuildPredicate(context.Background(), FilterInput{Field: "frontmatter.status", Op: "contains", Value: "x"})
		require.False(t, ok, "enum field does not support contains")
	})
}

func TestPlannerPlanSortIsPrefixMatch(t *testing.T) {
	schema, noteType := newTestSchema()
	planner := Planner{Schema: schema, Resolver: ViewResolver{NoteType: noteType}}

	for _, tc := range []struct {
		name                    string
		input, pushed, residual []SortInput
	}{
		{"all supported", []SortInput{{"specStatus", "asc"}, {"due", "desc"}, {"notePath", "asc"}}, []SortInput{{"specStatus", "asc"}, {"due", "desc"}, {"notePath", "asc"}}, nil},
		{"unsupported tail", []SortInput{{"frontmatter.status", "asc"}, {"priority", "desc"}, {"unknownField", "asc"}, {"owner", "asc"}}, []SortInput{{"frontmatter.status", "asc"}, {"priority", "desc"}}, []SortInput{{"unknownField", "asc"}, {"owner", "asc"}}},
		{"unsupported middle", []SortInput{{"due", "desc"}, {"unknownField", "asc"}, {"notePath", "desc"}}, []SortInput{{"due", "desc"}}, []SortInput{{"unknownField", "asc"}, {"notePath", "desc"}}},
		{"unsupported first", []SortInput{{"unknownField", "asc"}, {"priority", "desc"}}, []SortInput{}, []SortInput{{"unknownField", "asc"}, {"priority", "desc"}}},
		{"link middle", []SortInput{{"specStatus", "asc"}, {"linkedSpec", "desc"}, {"due", "asc"}}, []SortInput{{"specStatus", "asc"}}, []SortInput{{"linkedSpec", "desc"}, {"due", "asc"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			partition := planner.PlanSort(tc.input)
			require.Equal(t, tc.pushed, partition.Pushed)
			require.Equal(t, tc.residual, partition.Residual)
		})
	}
}

func TestPlannerBuildSortLinkFieldsRejected(t *testing.T) {
	schema, noteType := newTestSchema()
	planner := Planner{Schema: schema, Resolver: ViewResolver{NoteType: noteType}}

	_, ok := planner.BuildSort(SortInput{Field: "linkedSpec"})
	require.False(t, ok, "link-typed fields are not sortable")
}

type stubLinkResolver struct {
	target  string
	warning Warning
}

func (s stubLinkResolver) ResolveLinkTarget(ctx context.Context, field *ontology.Field, raw string) (string, Warning) {
	return s.target, s.warning
}

func TestPlannerLinkResolverOverridesTargetPath(t *testing.T) {
	schema, noteType := newTestSchema()
	planner := Planner{
		Schema:       schema,
		Resolver:     ViewResolver{NoteType: noteType},
		LinkResolver: stubLinkResolver{target: "specs/canonical.md"},
	}
	result, ok := planner.BuildPredicate(context.Background(), FilterInput{Field: "linkedSpec", Op: "eq", Value: "Foo"})
	require.True(t, ok)
	require.Equal(t, "specs/canonical.md", result.Predicate.Values[0].TargetNotePath)
}
