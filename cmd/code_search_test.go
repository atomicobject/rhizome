package cmd

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestBestSnippet_PrefersIntelOverDocLinks(t *testing.T) {
	t.Parallel()

	snippet := bestSnippet([]search.Evidence{
		{Type: "doc_link", RawScore: 1.0, Details: map[string]string{"snippet": "from doc_link"}},
		{Type: "intel_fts_match", RawScore: 0.2, Details: map[string]string{"snippet": "from intel"}},
	})
	require.Equal(t, "from intel", snippet)
}

func TestFormatUnifiedEntryLine_NoColor(t *testing.T) {
	t.Parallel()

	line := formatUnifiedEntryLine(false, 1, 0.6, "anchor", "Embedding", "pkg/embeddings/types.go", "evidence=intel_fts_match=0.93")
	require.Contains(t, line, " 1.")
	require.Contains(t, line, "60.0%")
	require.Contains(t, line, "[anchor]")
	require.Contains(t, line, "(pkg/embeddings/types.go)")
	require.Contains(t, line, "evidence=intel_fts_match=0.93")
}

func TestJoinQueries(t *testing.T) {
	t.Parallel()

	got := joinQueries("foo bar", []string{" extra semantic query ", "", "foo bar"})
	want := "foo bar\n\nextra semantic query"
	require.Equal(t, want, got)
}

func TestMergeSeedTokens(t *testing.T) {
	t.Parallel()

	got := mergeSeedTokens([]string{"a.md", "file1.go"}, []string{"file2.go", "file1.go", " "})
	require.Equal(t, []string{"a.md", "file1.go", "file2.go"}, got)
}
