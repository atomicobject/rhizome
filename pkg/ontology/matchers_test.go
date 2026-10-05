package ontology

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchExpressionOperatorWords(t *testing.T) {
	doc := &noteDoc{
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
