package ontology

import (
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
	"math"
	"testing"
	"time"
)

func TestScalarAssessmentAndCatalogAgree(t *testing.T) {
	for _, test := range []struct {
		typeName string
		raw      string
		kind     string
		date     string
	}{
		{"Date", "2026-10-17", "date", "2026-10-17"},
		{"Date", "2026/10/17", "date", "2026-10-17"},
		{"DateTime", "2026-10-17 10:00", "datetime", "2026-10-17T10:00:00.000000000Z"},
		{"DateTime", "2026/10/17 10:00:01", "datetime", "2026-10-17T10:00:01.000000000Z"},
		{"DateTime", "2026-10-17T10:00:01.12-04:00", "datetime", "2026-10-17T14:00:01.120000000Z"},
		{"Boolean", "TRUE", "bool", ""},
		{"bool", "false", "bool", ""},
		{"integer", "42", "int", ""},
		{"number", "1.5", "real", ""},
		{"URL", "https://example.com", "url", ""},
	} {
		t.Run(test.typeName+"/"+test.raw, func(t *testing.T) {
			field := &ontoschema.Field{Name: "value", Kind: ontoschema.FieldKindScalar, TypeName: test.typeName}
			assessment := AssessScalarFieldValues("note.md", "Test", field, &ontoschema.Schema{}, []string{test.raw}, true)
			require.Empty(t, assessment.Issues)
			require.Equal(t, []string{test.raw}, assessment.ValidValues)
			row := ontologyNodeFieldValueForValue(&ontoschema.Schema{}, readmodel.NodeRow{NodeID: "node"}, field, FieldBinding{}, test.raw, 0, 1)
			require.Equal(t, test.kind, row.ValueKind)
			if test.kind == "date" {
				require.Equal(t, test.date, *row.ValueDate)
			}
			if test.kind == "datetime" {
				require.Equal(t, test.date, *row.ValueDateTime)
			}
		})
	}
	for _, test := range []struct{ typeName, raw string }{
		{"Date", "2026-02-30"}, {"DateTime", "2026-10-17"},
		{"Int", "1.5"}, {"Float", "NaN"},
		{"Boolean", "yes"}, {"URL", "relative/path"},
	} {
		field := &ontoschema.Field{Name: "value", Kind: ontoschema.FieldKindScalar, TypeName: test.typeName}
		assessment := AssessScalarFieldValues("note.md", "Test", field, &ontoschema.Schema{}, []string{test.raw}, true)
		require.Len(t, assessment.Issues, 1, "%s %s", test.typeName, test.raw)
		require.Equal(t, "string", FieldValueRow(field, test.raw).ValueKind)
	}
}

func TestFieldValueRowPreservesInfiniteNumericComparisons(t *testing.T) {
	field := &ontoschema.Field{Kind: ontoschema.FieldKindScalar, TypeName: "Float"}
	for _, raw := range []string{"+Inf", "-Inf"} {
		row := FieldValueRow(field, raw)
		require.Equal(t, "real", row.ValueKind)
		require.NotNil(t, row.ValueReal)
		require.True(t, math.IsInf(*row.ValueReal, 0))
		require.Equal(t, raw, row.ValueText)
	}
	require.Nil(t, FieldValueRow(field, "NaN").ValueReal)
}

func TestFieldValueRowUsesSemanticIdentityForDeclaredIdentifiers(t *testing.T) {
	for _, typeName := range []string{"String", "ID"} {
		field := &ontoschema.Field{Kind: ontoschema.FieldKindScalar, TypeName: typeName, IsIdentifier: true}
		for _, test := range []struct{ raw, text string }{
			{" ^Mixed-ID ", "^Mixed-ID"}, {"Mixed-ID", "Mixed-ID"},
			{"[[Mixed-ID|Display]]", "[[Mixed-ID|Display]]"},
			{"[[^Mixed-ID|Display]]", "[[^Mixed-ID|Display]]"},
			{"Mixed-ID|Display", "Mixed-ID|Display"},
		} {
			row := FieldValueRow(field, test.raw)
			require.Equal(t, test.text, row.ValueText)
			require.Equal(t, "mixed-id", row.ValueNorm)
		}
		field.IsIdentifier = false
		require.Equal(t, "^mixed-id", FieldValueRow(field, "^Mixed-ID").ValueNorm,
			"an ordinary scalar does not acquire identifier semantics from its scalar type")
		require.Equal(t, "[[mixed-id|display]]", FieldValueRow(field, "[[Mixed-ID|Display]]").ValueNorm)
		require.Equal(t, "mixed-id|display", FieldValueRow(field, "Mixed-ID|Display").ValueNorm)
	}
}

func TestFieldValuesPreserveTextAndCompareDateTimeInstants(t *testing.T) {
	text := &ontoschema.Field{Kind: ontoschema.FieldKindScalar, TypeName: "String"}
	link := &ontoschema.Field{Kind: ontoschema.FieldKindLink}
	require.Equal(t, "[[path|alias]]", FieldValueRow(text, " [[Path|Alias]] ").ValueNorm)
	require.Equal(t, "path|alias", FieldValueRow(text, "Path|Alias").ValueNorm)
	require.Equal(t, "path", FieldValueRow(link, "[[Path|Alias]]").ValueNorm)
	require.Equal(t, "Path|Alias", FieldValueRow(link, "Path|Alias").TargetNotePath)
	require.Equal(t, "[[Path|Alias", FieldValueRow(link, "[[Path|Alias").TargetNotePath)

	field := &ontoschema.Field{Kind: ontoschema.FieldKindScalar, TypeName: "DateTime"}
	first := FieldValueRow(field, "2026-10-17T10:00:00-04:00")
	same := FieldValueRow(field, "2026-10-17T14:00:00Z")
	later := FieldValueRow(field, "2026-10-17T14:00:00.1Z")
	require.Equal(t, *first.ValueDateTime, *same.ValueDateTime)
	require.Less(t, *first.ValueDateTime, *later.ValueDateTime)
	require.Equal(t, "2026-10-17T10:00:00-04:00", first.ValueText)

	midnight := time.Date(2026, 10, 17, 0, 0, 0, 123, time.UTC)
	require.Equal(t, []string{"2026-10-17T00:00:00.000000123Z"}, FieldTextValues(field, []any{midnight}))
	require.Equal(t, []string{"2026-10-17T00:00:00Z"}, FieldTextValues(field, midnight.Truncate(time.Second)))
}
