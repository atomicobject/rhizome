package appbundle

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/stretchr/testify/require"
)

const retainedImageHelper = "TestPortableRetainedImageHelper"

func TestPortableRetainedImageStartsRuntimeChildrenAfterOwnerRemoval(t *testing.T) {
	root, record, _ := portableFixture(t)
	self, err := os.Executable()
	require.NoError(t, err)
	payload, err := os.ReadFile(self)
	require.NoError(t, err)
	record.ImageHash, err = hashFile(self)
	require.NoError(t, err)
	image := ImagePath(root, record.ImageHash)
	require.NoError(t, os.MkdirAll(filepath.Dir(image), 0o700))
	require.NoError(t, os.WriteFile(image, payload, 0o700))
	writePortableFixture(t, root, record)
	base := filepath.Dir(root)
	entry := filepath.Join(base, "bin", executableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(entry), 0o700))
	if runtime.GOOS == "windows" {
		require.NoError(t, os.WriteFile(entry, payload, 0o700))
	} else {
		require.NoError(t, os.Symlink(image, entry))
	}
	done := filepath.Join(base, "restart-complete")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, image, "-test.run=^"+retainedImageHelper+"$", "--", "runtime", base, entry, done)
	input, err := command.StdinPipe()
	require.NoError(t, err)
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	command.Stderr = os.Stderr
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		_ = input.Close()
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	ready := bufio.NewScanner(output)
	require.True(t, ready.Scan())
	require.Equal(t, "ready", ready.Text())
	require.NoError(t, os.Remove(record.AppPath))
	_, err = fmt.Fprintln(input, "launch")
	require.NoError(t, err)
	require.NoError(t, command.Wait())
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(done)
		return err == nil && string(data) == "retained runtime restarted"
	}, 10*time.Second, 10*time.Millisecond, "a detached helper and successor must execute after their GUI owner is removed")
	// Windows keeps an executable locked until its final child exits.
	require.Eventually(t, func() bool { return os.Remove(image) == nil }, 5*time.Second, 10*time.Millisecond)
}

func TestPortableRetainedImageHelper(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || len(os.Args) != separator+5 {
		return
	}
	mode, base, entry, done := os.Args[separator+1], os.Args[separator+2], os.Args[separator+3], os.Args[separator+4]
	portableBaseDirectory = func() (string, error) { return base, nil }
	self, err := os.Executable()
	require.NoError(t, err)
	if handled, code := dispatchEntry(self, os.Args[0], entry, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}
	next := "helper"
	switch mode {
	case "runtime":
		fmt.Println("ready")
		require.True(t, bufio.NewScanner(os.Stdin).Scan())
	case "helper":
		next = "restart"
	case "restart":
		require.NoError(t, os.WriteFile(done, []byte("retained runtime restarted"), 0o600))
		os.Exit(0)
	default:
		t.Fatalf("unexpected retained-image helper mode %q", mode)
	}
	require.NoError(t, appruntime.StartDetached(self, base, "-test.run=^"+retainedImageHelper+"$", "--", next, base, entry, done))
	os.Exit(0)
}
