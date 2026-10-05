//go:build windows

package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishRepairJournalManifestReplacesWithWriteThrough(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "manifest.json.tmp")
	destination := filepath.Join(dir, "manifest.json")
	require.NoError(t, os.WriteFile(source, []byte("new"), 0o600))
	require.NoError(t, os.WriteFile(destination, []byte("old"), 0o600))

	require.NoError(t, publishRepairJournalManifest(source, destination))
	assert.NoFileExists(t, source)
	content, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), content)
}

func TestDetachRepairJournalUsesNoReplaceWriteThrough(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "journal")
	destination := filepath.Join(root, ".cleanup-journal")
	require.NoError(t, os.Mkdir(source, 0o700))
	require.NoError(t, os.Mkdir(destination, 0o700))

	require.Error(t, detachRepairJournal(source, destination))
	assert.DirExists(t, source)
	assert.DirExists(t, destination)

	require.NoError(t, os.Remove(destination))
	require.NoError(t, detachRepairJournal(source, destination))
	assert.NoDirExists(t, source)
	assert.DirExists(t, destination)
}
