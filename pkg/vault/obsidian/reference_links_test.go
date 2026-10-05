package obsidian

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractMarkdownReferenceDefinitions(t *testing.T) {
	content := stringsJoinLines(
		"[workflow]: docs/workflow.md \"Team workflow\"",
		"   [policy]: <docs/team policy.md#review> 'Policy'",
		"[multiline]:",
		"  docs/multiline.md",
		"[external]: https://example.com/policy",
		"`[inline]: docs/inline.md`",
		"    [indented]: docs/indented.md",
		"```markdown",
		"[fenced]: docs/fenced.md",
		"```",
	)

	require.Equal(t, []string{
		"docs/workflow.md",
		"docs/team policy.md#review",
		"docs/multiline.md",
	}, ExtractMarkdownReferenceDefinitions(content))
}

func stringsJoinLines(lines ...string) string {
	var out string
	for _, line := range lines {
		out += line + "\n"
	}
	return out
}
