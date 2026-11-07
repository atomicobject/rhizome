package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestMatchExpressionOperatorWords(t *testing.T) {
	doc := &Subject{
		Path: "Log/Sync with Pat, 2024-06-04.md",
		Tags: []string{"conversation"},
	}
	cases := []struct {
		raw  string
		want bool
	}{
		{"Log AND tag:conversation", true},
		{"tag:conversation AND Log", true},
		{"Log AND NOT tag:person", true},
		{"Log AND tag:person", false},
		{"(Log AND tag:conversation)", true},
		{"Log && tag:conversation", true},
		{"Log AND tag:conversation AND NOT tag:person", true},
		// Lowercase words stay part of a literal path.
		{"Log/Sync with Pat, 2024-06-04.md", true},
		{"Notes/Rock and Roll", false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			expr, err := parseMatchExpression(tc.raw)
			require.NoError(t, err)
			require.Equal(t, tc.want, evalMatchExpression(expr, doc))
		})
	}
}

func TestProductMembershipMatcher(t *testing.T) {
	compiled := mustLoadSDL(t, `type Selected @node(matches: ["products:alpha OR products:\"*\" OR NOT has:products"]) { title: String }`)
	for _, tc := range []struct {
		frontmatter string
		want        bool
	}{
		{"{}", true},
		{"products: null", true},
		{`products: ""`, true},
		{"products: []", false},
		{"products: [alpha]", true},
		{"products: alpha", true},
		{`products: ["*"]`, true},
		{`products: [beta, "*"]`, true},
		{"products: [beta]", false},
		{`PRODUCTS: "*"`, true},
		{"products: false", false},
		{"products: 0", false},
	} {
		t.Run(tc.frontmatter, func(t *testing.T) {
			var frontmatter map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(tc.frontmatter), &frontmatter))
			require.Equal(t, tc.want, compiled.MatchDoc("Selected", &Subject{Frontmatter: frontmatter}))
		})
	}
}

func TestPropertyMatcherAtoms(t *testing.T) {
	_, err := parseMatchExpression("products:*")
	require.Error(t, err)
	_, err = parseMatchExpression("has:")
	require.Error(t, err)
	expr, err := parseMatchExpression("has:products")
	require.NoError(t, err)
	require.False(t, evalMatchExpression(expr, &Subject{Inline: map[string][]string{"products": {"alpha"}}}))
	_, comparable := selectorClausesFromExpression(expr)
	require.False(t, comparable)
	quoted, err := parseMatchExpression(`products:"alpha"`)
	require.NoError(t, err)
	unquoted, err := parseMatchExpression("products:alpha")
	require.NoError(t, err)
	quotedClauses, comparable := selectorClausesFromExpression(quoted)
	require.True(t, comparable)
	unquotedClauses, comparable := selectorClausesFromExpression(unquoted)
	require.True(t, comparable)
	require.Equal(t, unquotedClauses, quotedClauses)
}
