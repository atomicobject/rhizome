package pushdown

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"testing"

	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/stretchr/testify/require"
)

func newTestSchema() (*ontoschema.Schema, *ontoschema.NoteType) {
	noteType := &ontoschema.NoteType{
		Name: "TechnicalSpec",
		Fields: []*ontoschema.Field{
			{Name: "specStatus", Kind: ontoschema.FieldKindEnum, TypeName: "SpecStatus", Source: "status", SourceKind: ontoschema.FieldSourceFrontmatter},
			{Name: "owner", Kind: ontoschema.FieldKindScalar, TypeName: "String", Source: "owner", SourceKind: ontoschema.FieldSourceInline},
			{Name: "priority", Kind: ontoschema.FieldKindScalar, TypeName: "Int", Source: "priority", SourceKind: ontoschema.FieldSourceFrontmatter},
			{Name: "due", Kind: ontoschema.FieldKindScalar, TypeName: "Date", Source: "due", SourceKind: ontoschema.FieldSourceFrontmatter},
			{Name: "linkedSpec", Kind: ontoschema.FieldKindLink, TypeName: "TechnicalSpec", Source: "linked", SourceKind: ontoschema.FieldSourceFrontmatter},
		},
	}
	noteType.ByName = map[string]*ontoschema.Field{}
	for _, field := range noteType.Fields {
		noteType.ByName[field.Name] = field
	}
	schema := &ontoschema.Schema{
		Types: map[string]*ontoschema.NoteType{"TechnicalSpec": noteType},
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
		require.Equal(t, readmodel.FieldOperator("gte"), result.Predicate.Op)
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
			require.Equal(t, readmodel.FieldOpEq, result.Predicate.Op)
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

func TestPlannerPreservesFullResidualSort(t *testing.T) {
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
			plan := planner.Plan(context.Background(), nil, tc.input)
			require.Equal(t, tc.pushed, append([]SortInput{}, plan.PushedSort...))
			if len(tc.residual) > 0 {
				require.Equal(t, tc.input, plan.Sort)
			} else {
				require.Empty(t, plan.Sort)
			}
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
	warning diag.Warning
}

func (s stubLinkResolver) ResolveLinkTarget(ctx context.Context, field *ontoschema.Field, raw string) (string, diag.Warning) {
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

func TestPlannerRetainsResidualStringOperators(t *testing.T) {
	schema, noteType := newTestSchema()
	planner := Planner{Schema: schema, Resolver: SchemaResolver{NoteType: noteType}}
	filters := []FilterInput{
		{Field: "owner", Op: "contains", Value: "Drew"},
		{Field: "owner", Op: "neq", Value: "Drew"},
	}
	plan := planner.Plan(context.Background(), filters, nil)
	require.Empty(t, plan.Pushed.Predicates)
	require.Equal(t, filters, plan.Residual)
	equality, ok := planner.BuildPredicate(context.Background(), FilterInput{Field: "owner", Op: "eq", Value: "Drew"})
	require.True(t, ok)
	require.Equal(t, readmodel.FieldOpEq, equality.Predicate.Op)
}
