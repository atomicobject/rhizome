package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeDocInputPreservesAuthoredNoteExtensions(t *testing.T) {
	root := t.TempDir()

	require.Equal(t, "Notes/Decision.MD", normalizeDocInput(root, "Notes/Decision.MD", true))
	require.Equal(t, "Notes/Reference.html", normalizeDocInput(root, "Notes/Reference.html", true))
	require.Equal(t, "Notes/Reference.html", normalizeDocInput(root, filepath.Join(root, "Notes", "Reference.html"), true))
}
