package codeanchor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractMentionCandidates(t *testing.T) {
	t.Run("extracts @dotted mentions and markdown links, skips code", func(t *testing.T) {
		content := "# Note\n\n" +
			"Ref: @embeddings.codeindex.types.Item\n\n" +
			"Link: [Item](pkg/embeddings/codeindex/types.go#Item)\n\n" +
			"Ignore fenced:\n" +
			"```go\n" +
			"// @embeddings.codeindex.types.Item\n" +
			"// [Item](pkg/embeddings/codeindex/types.go#Item)\n" +
			"```\n\n" +
			"Ignore inline: `@embeddings.codeindex.types.Item` and `[Item](pkg/embeddings/codeindex/types.go#Item)`\n"
		cands := extractMentionCandidates("notes/foo.md", content)
		require.Len(t, cands, 2)
		require.Equal(t, "at", cands[0].Source)
		require.Equal(t, "embeddings.codeindex.types", cands[0].Qualifier)
		require.Equal(t, "Item", cands[0].Symbol)
		require.Equal(t, "mdlink", cands[1].Source)
		require.Equal(t, "pkg/embeddings/codeindex/types.go", cands[1].TargetPath)
		require.Equal(t, "Item", cands[1].Fragment)
	})

	t.Run("resolves relative link targets against note directory", func(t *testing.T) {
		content := `See [Item](../pkg/embeddings/codeindex/types.go#Item)`
		cands := extractMentionCandidates("vault/notes/foo.md", content)
		require.Len(t, cands, 1)
		require.Equal(t, "mdlink", cands[0].Source)
		require.Equal(t, "vault/pkg/embeddings/codeindex/types.go", cands[0].TargetPath)
	})
}
