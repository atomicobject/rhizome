package actions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalInputExpressionNormalizesCommutativeOrderAndWhitespace(t *testing.T) {
	_, left, err := ParseInputsWithExpression([]string{"tag:foo", "AND", "find:bar"})
	require.NoError(t, err)
	_, right, err := ParseInputsWithExpression([]string{"find:bar", "AND", "tag:foo"})
	require.NoError(t, err)
	require.Equal(t, CanonicalInputExpression(left), CanonicalInputExpression(right))

	spaced := &InputExpression{Type: exprLeaf, Input: &ListInput{Type: InputTypeFind, Value: " bar "}}
	plain := &InputExpression{Type: exprLeaf, Input: &ListInput{Type: InputTypeFind, Value: "bar"}}
	require.Equal(t, CanonicalInputExpression(plain), CanonicalInputExpression(spaced))
}

func TestParseSearchQueryWithExpressionHoistsBareTextIntoFindAndAndsClauses(t *testing.T) {
	inputs, expr, err := ParseSearchQueryWithExpression("foo bar tag:xyz")
	require.NoError(t, err)
	require.Len(t, inputs, 2)

	info := AnalyzeExpression(expr)
	require.True(t, info.HasFind)
	require.True(t, info.HasTag)
	require.False(t, info.HasOr)

	require.Equal(t, InputTypeFind, inputs[0].Type)
	require.Equal(t, "foo bar", inputs[0].Value)
	require.Equal(t, InputTypeTag, inputs[1].Type)
	require.Equal(t, "xyz", inputs[1].Value)

	require.Equal(t, exprAnd, expr.Type)
	require.Equal(t, InputTypeFind, expr.Left.Input.Type)
	require.Equal(t, "foo bar", expr.Left.Input.Value)
	require.Equal(t, InputTypeTag, expr.Right.Input.Type)
	require.Equal(t, "xyz", expr.Right.Input.Value)
}

func TestParseSearchQueryWithExpressionSupportsBooleanOperatorsAndParens(t *testing.T) {
	inputs, expr, err := ParseSearchQueryWithExpression(`(foo OR bar) AND NOT tag:xyz`)
	require.NoError(t, err)
	require.Len(t, inputs, 3)

	require.Equal(t, exprAnd, expr.Type)
	require.Equal(t, exprOr, expr.Left.Type)
	require.Equal(t, exprNot, expr.Right.Type)

	require.Equal(t, InputTypeFind, expr.Left.Left.Input.Type)
	require.Equal(t, "foo", expr.Left.Left.Input.Value)
	require.Equal(t, InputTypeFind, expr.Left.Right.Input.Type)
	require.Equal(t, "bar", expr.Left.Right.Input.Value)
	require.Equal(t, InputTypeTag, expr.Right.Left.Input.Type)
	require.Equal(t, "xyz", expr.Right.Left.Input.Value)
}

func TestParseSearchQueryWithExpressionKeepsStructuredFiltersSeparateFromBarePhrases(t *testing.T) {
	inputs, expr, err := ParseSearchQueryWithExpression(`plan spec:specs/100-demo/spec.md`)
	require.NoError(t, err)
	require.Len(t, inputs, 2)

	require.Equal(t, exprAnd, expr.Type)
	require.Equal(t, InputTypeFind, expr.Left.Input.Type)
	require.Equal(t, "plan", expr.Left.Input.Value)
	require.Equal(t, InputTypeProperty, expr.Right.Input.Type)
	require.Equal(t, "spec", expr.Right.Input.Property)
	require.Equal(t, "specs/100-demo/spec.md", expr.Right.Input.Value)
}
