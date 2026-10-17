package validate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlockIDMigrationNeverRewritesProtectedSource(t *testing.T) {
	const source = "---\nexample: '[[target#^old]]'\n---\n\n[[target#^old|keep]] [keep](target.md#^old)\n\n```md\n[[target#^old]] [code](target.md#^old)\n```\n\n`[[target#^old]]`\n\n    [[target#^old]]\n"
	root := t.TempDir()
	code := "```md\n[[target#^old]]\n```\n"
	require.NoError(t, writeFixtureFiles(root, map[string]string{"source.md": source, "target.md": "# Target\n^old\n", "code.md": code}))
	run := linkHygieneRunContext(t, root)
	updated, count, handled, err := applyBlockFragmentRewrite(run, "source.md", source, "target.md#^old", "target.md#^new")
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, 2, count)
	require.Equal(t, "---\nexample: '[[target#^old]]'\n---\n\n[[target#^new|keep]] [keep](target.md#^new)\n\n```md\n[[target#^old]] [code](target.md#^old)\n```\n\n`[[target#^old]]`\n\n    [[target#^old]]\n", updated)
	inventory, err := validationLinksFor(context.Background(), run)
	require.NoError(t, err)
	edits := blockIDReferenceEdits(inventory, "target.md", "old", "new")
	require.Len(t, edits, 1)
	require.Equal(t, "source.md", edits[0].NotePath)
}
