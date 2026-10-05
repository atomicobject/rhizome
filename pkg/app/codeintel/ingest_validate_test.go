package codeintel

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateMarkdownCodeAnchorSyntaxParsesMarkdown(t *testing.T) {
	err := validateMarkdownCodeAnchorSyntax("notes/current.md", "---\ncode-anchors:\n  go:\n    - Foo\n---")
	require.Error(t, err)
}
