package serve

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRotatingFileRotatesOnceAtTheCap(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "runtime.log")
	writer, err := newRotatingFile(path, 32)
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Close() })

	_, err = writer.Write([]byte(strings.Repeat("a", 30)))
	require.NoError(t, err)
	require.NoFileExists(t, path+".1", "a write inside the cap must not rotate")

	_, err = writer.Write([]byte(strings.Repeat("b", 30)))
	require.NoError(t, err)

	rotated, err := os.ReadFile(path + ".1")
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("a", 30), string(rotated))
	current, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("b", 30), string(current))

	// A second rotation overwrites the single retained generation rather than
	// accumulating logs without bound.
	_, err = writer.Write([]byte(strings.Repeat("c", 30)))
	require.NoError(t, err)
	rotated, err = os.ReadFile(path + ".1")
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("b", 30), string(rotated))
	require.NoFileExists(t, path+".2")
}

func TestRotatingFileIsOwnerOnly(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "runtime.log")
	writer, err := newRotatingFile(path, LogMaxBytes)
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Close() })
	_, err = writer.Write([]byte("hello\n"))
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		// Windows reports no Unix permission bits for the owner-only file.
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestCaptureProcessOutputRedirectsStdoutAndStderr(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.log")
	realStdout, realStderr := os.Stdout, os.Stderr
	capture, err := CaptureProcessOutput(path, LogMaxBytes)
	require.NoError(t, err)
	require.NotEqual(t, realStdout, os.Stdout, "capture must replace the process descriptors")

	_, _ = os.Stdout.WriteString("to stdout\n")
	_, _ = os.Stderr.WriteString("to stderr\n")
	_, _ = capture.Writer().Write([]byte("direct\n"))
	require.NoError(t, capture.Close())

	require.Equal(t, realStdout, os.Stdout, "close must restore stdout")
	require.Equal(t, realStderr, os.Stderr, "close must restore stderr")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "to stdout")
	require.Contains(t, string(data), "to stderr")
	require.Contains(t, string(data), "direct")
}

func TestIdleTrackerMeasuresTimeSinceLastTouch(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tracker := &IdleTracker{now: func() time.Time { return now }}
	tracker.Touch()
	require.Zero(t, tracker.IdleFor())

	now = now.Add(90 * time.Second)
	require.Equal(t, 90*time.Second, tracker.IdleFor())

	tracker.Touch()
	require.Zero(t, tracker.IdleFor())
}

func TestIdleTrackerIsNeverIdleWhileAStreamIsHeld(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tracker := &IdleTracker{now: func() time.Time { return now }}
	tracker.Touch()
	release := tracker.Hold()
	now = now.Add(2 * time.Hour)
	require.Zero(t, tracker.IdleFor(), "an open stream keeps the runtime active")

	release()
	release()
	require.Zero(t, tracker.IdleFor(), "releasing a stream records activity")
	now = now.Add(time.Minute)
	require.Equal(t, time.Minute, tracker.IdleFor(), "a second release must not unbalance the count")
}

func TestWatchIdleExitShutsDownOnlyAfterTheTimeout(t *testing.T) {
	t.Parallel()

	base := time.Now()
	var advanced atomic.Int64 // the watcher goroutine reads the clock concurrently
	tracker := &IdleTracker{now: func() time.Time { return base.Add(time.Duration(advanced.Load())) }}
	tracker.Touch()

	reasons := make(chan string, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go watchIdleExit(ctx, tracker, nil, idleExitConfig{timeout: time.Minute, poll: time.Millisecond}, func(reason string) {
		reasons <- reason
	})

	select {
	case reason := <-reasons:
		t.Fatalf("exited while still active: %s", reason)
	case <-time.After(50 * time.Millisecond):
	}

	advanced.Store(int64(2 * time.Minute))
	select {
	case reason := <-reasons:
		require.Contains(t, reason, "idle")
	case <-time.After(2 * time.Second):
		t.Fatal("idle runtime never exited")
	}
}

func TestWatchIdleExitIsDisabledWithoutATimeout(t *testing.T) {
	t.Parallel()

	// An attached runtime never idle-exits, which Run expresses by not starting
	// the watcher; a zero timeout must be inert even if one is started.
	reasons := make(chan string, 1)
	watchIdleExit(context.Background(), NewIdleTracker(), nil, idleExitConfig{timeout: 0, poll: time.Millisecond}, func(reason string) {
		reasons <- reason
	})
	require.Empty(t, reasons)
}

func TestWatchVaultRootShutsDownWhenTheRootDisappears(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "vault")
	require.NoError(t, os.MkdirAll(root, 0o755))

	reasons := make(chan string, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go watchVaultRoot(ctx, root, time.Millisecond, func(reason string) { reasons <- reason })

	select {
	case reason := <-reasons:
		t.Fatalf("exited while the root existed: %s", reason)
	case <-time.After(50 * time.Millisecond):
	}

	require.NoError(t, os.RemoveAll(root))
	select {
	case reason := <-reasons:
		require.Contains(t, reason, root)
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not stop after its vault root was removed")
	}
}

func TestRotatingFileBoundsOversizedOutputAndMarksTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-output.log")
	writer, err := newRotatingFile(path, 128)
	require.NoError(t, err)
	input := strings.Repeat("discarded", 100) + " useful crash tail"
	n, err := writer.Write([]byte(input))
	require.NoError(t, err)
	require.Equal(t, len(input), n)
	require.NoError(t, writer.Close())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Len(t, data, 128)
	require.Contains(t, string(data), "truncated")
	require.Contains(t, string(data), "useful crash tail")
}

func TestRotatingFileBoundsInheritedOversizedStartupOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-output.log")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("initial startup output", 100)+" important tail"), 0600))
	writer, err := newRotatingFile(path, 128)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Len(t, data, 128)
	require.Contains(t, string(data), "important tail")
}

func TestRotatingFileBoundsOversizedNativeCrashBackupOnNextOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-output.log")
	require.NoError(t, os.WriteFile(path+".1", []byte(strings.Repeat("native crash output", 100)+" important crash tail"), 0600))
	writer, err := newRotatingFile(path, 128)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	data, err := os.ReadFile(path + ".1")
	require.NoError(t, err)
	require.Len(t, data, 128)
	require.Contains(t, string(data), "important crash tail")
}

func TestProcessOutputKeepsDrainingAfterMidCaptureSinkFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "capture")
	require.NoError(t, os.MkdirAll(parent, 0700))
	capture, err := CaptureProcessOutput(filepath.Join(parent, "runtime-output.log"), 64)
	require.NoError(t, err)
	defer capture.Close()
	_, err = capture.Writer().Write([]byte(strings.Repeat("a", 64)))
	require.NoError(t, err)
	// Close the OS handle without closing the writer, which would turn writes
	// into successful discards. This causes a real sink error on every platform;
	// Windows does not permit renaming a directory containing this open handle.
	capture.writer.mu.Lock()
	err = capture.writer.file.Close()
	capture.writer.mu.Unlock()
	require.NoError(t, err)
	_, err = capture.Writer().Write([]byte("failed sink probe"))
	require.ErrorIs(t, err, os.ErrClosed)
	_, err = capture.write.Write([]byte(strings.Repeat("b", 128)))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, writeErr := capture.write.Write([]byte(strings.Repeat("c", 1<<20))); done <- writeErr }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("process output blocked after diagnostic sink failed")
	}
	require.NoError(t, capture.Close())
}

func TestRawOutputRefusesSymlinkedDiagnosticsDirectory(t *testing.T) {
	target := t.TempDir()
	parent := filepath.Join(t.TempDir(), "diagnostics")
	if err := os.Symlink(target, parent); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	writer, err := newRotatingFile(filepath.Join(parent, "runtime-output.log"), 128)
	require.Error(t, err)
	require.Nil(t, writer)
	entries, err := os.ReadDir(target)
	require.NoError(t, err)
	require.Empty(t, entries, "capture must not write through an unrelated directory symlink")
}
