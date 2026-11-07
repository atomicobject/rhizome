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
	// The AO KB starter's generated admission: AO reads frontmatter products exactly.
	compiled := mustLoadSDL(t, `type Selected @node(matches: ["products:\"alpha\" OR products:\"*\" OR NOT has:products"]) { title: String }`)
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
		{"products: [Alpha]", false},
		{`products: [" alpha"]`, false},
		{`products: ["[[alpha]]"]`, false},
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

func TestQuotedPropertyValuesMatchFrontmatterExactly(t *testing.T) {
	quoted, err := parseMatchExpression(`products:"alpha"`)
	require.NoError(t, err)
	unquoted, err := parseMatchExpression("products:alpha")
	require.NoError(t, err)
	frontmatter := func(value any) *Subject { return &Subject{Frontmatter: map[string]any{"products": value}} }
	for _, tc := range []struct {
		name             string
		doc              *Subject
		quoted, unquoted bool
	}{
		{"list element", frontmatter([]any{"beta", "alpha"}), true, true},
		{"scalar", frontmatter("alpha"), true, true},
		{"case variant", frontmatter([]any{"Alpha"}), false, true},
		{"surrounding space", frontmatter([]any{" alpha "}), false, true},
		{"link", frontmatter([]any{"[[alpha]]"}), false, true},
		{"inline field only", &Subject{Inline: map[string][]string{"products": {"alpha"}}}, false, true},
		{"key case", &Subject{Frontmatter: map[string]any{"Products": []any{"alpha"}}}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.quoted, evalMatchExpression(quoted, tc.doc), "quoted")
			require.Equal(t, tc.unquoted, evalMatchExpression(unquoted, tc.doc), "unquoted")
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

func TestQuotedPropertyAtomsStayLiteralInExpressions(t *testing.T) {
	doc := &Subject{Path: "people/person.md", Tags: []string{"person"}, Frontmatter: map[string]any{"owner": "Drew AND Colthorp", "products": "(beta)"}}
	for _, selector := range []string{
		`owner:"Drew AND Colthorp"`,
		`owner:"Drew AND Colthorp" AND tag:person`,
		`(owner:"Drew AND Colthorp" AND (tag:person OR NOT has:owner))`,
		`products:"(beta)" OR products:"*"`,
		`(products:"(beta)")`,
	} {
		t.Run(selector, func(t *testing.T) {
			expr, err := parseMatchExpression(selector)
			require.NoError(t, err)
			require.True(t, evalMatchExpression(expr, doc))
		})
	}
}
