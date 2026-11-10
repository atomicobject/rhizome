//go:build !windows

package modulelifecycle

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLifecycleFIFOIsRejectedWithoutWaitingForAWriter(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome/modules/demo")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "lifecycle.yml"), 0600))
	done := make(chan []Descriptor, 1)
	go func() { descriptors, _ := Load(root); done <- descriptors }()
	select {
	case descriptors := <-done:
		require.Len(t, descriptors, 1)
		require.Contains(t, descriptors[0].Problem, "regular file")
	case <-time.After(time.Second):
		t.Fatal("reading a lifecycle FIFO waited for a writer")
	}
}

func TestLinkedInputReplacementCannotWaitForAFIFOWriter(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("original"), 0o600))
	linkInput(t, root, "AGENTS.md")
	dir, err := os.OpenRoot(root)
	require.NoError(t, err)
	defer dir.Close()
	info, err := dir.Stat("CLAUDE.md")
	require.NoError(t, err)
	require.NoError(t, syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600))
	require.NoError(t, os.Remove(filepath.Join(root, "CLAUDE.md")))
	linkInput(t, root, "pipe")
	done := make(chan error, 1)
	go func() {
		_, err := readCheckedFile(dir, "CLAUDE.md", 1024, info)
		done <- err
	}()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("a replaced input waited for a FIFO writer")
	}
}
