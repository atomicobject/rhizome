package obsidian

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFileAtomic(t *testing.T) {
	for _, perm := range []os.FileMode{0o644, 0o600} {
		t.Run(perm.String(), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "test.txt")
			want := []byte("Hello, atomic world!")
			require.NoError(t, WriteFileAtomic(path, want, perm))
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, want, got)
			info, err := os.Stat(path)
			require.NoError(t, err)
			if runtime.GOOS == "windows" {
				assert.Equal(t, perm, info.Mode().Perm()&perm)
			} else {
				assert.Equal(t, perm, info.Mode().Perm())
			}
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, "test.txt", entries[0].Name())
		})
	}
}

func TestWriteFileAtomicOverwrite(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "fsutil-test-*")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "test.txt")

	// Write initial content
	initialContent := []byte("Initial content")
	err = os.WriteFile(testFile, initialContent, 0644)
	assert.NoError(t, err)

	// Overwrite with atomic write
	newContent := []byte("New atomic content")
	err = WriteFileAtomic(testFile, newContent, 0644)
	assert.NoError(t, err)

	// Verify new content
	readContent, err := os.ReadFile(testFile)
	assert.NoError(t, err)
	assert.Equal(t, newContent, readContent)
}

func TestWriteFileAtomicPreservingModeKeepsExistingPermissions(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "preserved.txt")
	require.NoError(t, os.WriteFile(path, []byte("before"), 0o600))

	require.NoError(t, WriteFileAtomicPreservingMode(path, []byte("after"), 0o644))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte("after"), data)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestWriteFileAtomicInvalidDir(t *testing.T) {
	// Try to write to a non-existent directory
	invalidPath := filepath.Join(t.TempDir(), "missing", "file.txt")
	testContent := []byte("Test content")

	err := WriteFileAtomic(invalidPath, testContent, 0644)
	assert.Error(t, err)
	_, statErr := os.Stat(invalidPath)
	assert.True(t, os.IsNotExist(statErr), "final file must not exist: %v", statErr)
}

func TestWriteFileAtomicRenameFailureCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yml")
	require.NoError(t, os.Mkdir(target, 0o755))
	sentinel := filepath.Join(target, "unchanged")
	require.NoError(t, os.WriteFile(sentinel, []byte("old target"), 0o644))

	require.Error(t, WriteFileAtomic(target, []byte("notes: {}\n"), 0o644))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "failed replacement must remove its temporary sibling")
	require.Equal(t, "config.yml", entries[0].Name())
	old, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	require.Equal(t, "old target", string(old))
}
