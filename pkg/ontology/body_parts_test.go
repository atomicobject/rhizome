package ontology

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestSplitOntologyBody_PrefersMarkdownBoundaries(t *testing.T) {
	paragraphs := []string{
		"ALPHA-START " + strings.Repeat("alpha ", 8) + "ALPHA-END",
		"BETA-START " + strings.Repeat("beta ", 8) + "BETA-END",
		"```go\nfmt.Println(\"hello\")\n```",
		"GAMMA-START " + strings.Repeat("gamma ", 8) + "GAMMA-TAIL",
	}
	body := strings.Join(paragraphs, "\n\n")

	parts := splitOntologyBody(body, 80)
	require.GreaterOrEqual(t, len(parts), 2)
	for _, part := range parts {
		require.NotEmpty(t, part)
	}
	joined := strings.Join(parts, "\n")
	for _, paragraph := range paragraphs {
		require.Contains(t, joined, paragraph)
	}
	require.Equal(t, 1, strings.Count(joined, paragraphs[2]), "bounded code fence must stay whole")
}

func TestSplitOntologyBody_PreservesTailChunks(t *testing.T) {
	body := strings.Join([]string{
		"part-one " + strings.Repeat("alpha ", 20),
		"part-two " + strings.Repeat("beta ", 20),
		"part-three " + strings.Repeat("gamma ", 20),
		"TAIL-SENTINEL " + strings.Repeat("omega ", 20),
	}, "\n\n")

	parts := splitOntologyBody(body, 90)
	require.Greater(t, len(parts), 3)
	require.Contains(t, strings.Join(parts, "\n"), "TAIL-SENTINEL")
}
