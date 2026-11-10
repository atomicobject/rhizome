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
