//go:build !windows

package jsonstdio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/proctree"
	"github.com/stretchr/testify/require"
)

func TestCloseKillsGrandchildren(t *testing.T) {
	// "exits" leaves its grandchild behind on stdin EOF; "ignores" must be force-killed.
	for _, mode := range []string{"grandchild-exits", "grandchild-ignores"} {
		t.Run(mode, func(t *testing.T) {
			connection, marker := startChild(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := connection.Recv(ctx)
			require.NoError(t, err)
			var pid int
			require.Eventually(t, func() bool {
				data, err := os.ReadFile(marker)
				if err != nil {
					return false
				}
				pid, err = strconv.Atoi(string(data))
				return err == nil
			}, time.Second, time.Millisecond)
			require.NoError(t, syscall.Kill(pid, 0), "grandchild should be running before Close")

			closeCtx, cancelClose := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancelClose()
			require.NoError(t, connection.Close(closeCtx))
			require.Eventually(t, func() bool {
				return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
			}, 2*time.Second, 10*time.Millisecond, "grandchild %d survived Close", pid)
		})
	}
}

// Claude Code runs shell commands in process groups of their own, so a
// group kill alone leaves them running after the CLI exits.
func TestCloseKillsDescendantsInOtherProcessGroups(t *testing.T) {
	binary, err := os.Executable()
	require.NoError(t, err)
	marker := filepath.Join(t.TempDir(), "pid")
	connection, err := Start[json.RawMessage](context.Background(), "fixture", binary,
		[]string{"-test.run=^TestJSONStdioOwnGroupHelper$", "--", "own-group-helper", marker}, t.TempDir())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = connection.Recv(ctx)
	require.NoError(t, err)
	data, err := os.ReadFile(marker)
	require.NoError(t, err)
	pid, err := strconv.Atoi(string(data))
	require.NoError(t, err)
	require.NoError(t, syscall.Kill(pid, 0), "descendant should be running before Close")

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelClose()
	require.NoError(t, connection.Close(closeCtx))
	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
	}, 2*time.Second, 10*time.Millisecond, "descendant %d in its own process group survived Close", pid)
}

func TestJSONStdioOwnGroupHelper(t *testing.T) {
	arg := slices.Index(os.Args, "own-group-helper")
	if arg < 0 {
		return
	}
	descendant := exec.Command("sleep", "300")
	descendant.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := descendant.Start(); err != nil {
		os.Exit(1)
	}
	_ = os.WriteFile(os.Args[arg+1], []byte(strconv.Itoa(descendant.Process.Pid)), 0o600)
	if slices.Contains(os.Args, "exit-after-record") {
		// Outlive one tracker interval, then exit without waiting for input.
		time.Sleep(proctree.DefaultInterval + 500*time.Millisecond)
		os.Exit(0)
	}
	_, _ = fmt.Fprintln(os.Stdout, `{ "ready": true }`)
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

// When the harness exits on its own, its children in other process groups
// are reparented before Close runs; the tracker must still reach them.
func TestCloseKillsDescendantsAfterTheHarnessExits(t *testing.T) {
	binary, err := os.Executable()
	require.NoError(t, err)
	marker := filepath.Join(t.TempDir(), "pid")
	connection, err := Start[json.RawMessage](context.Background(), "fixture", binary,
		[]string{"-test.run=^TestJSONStdioOwnGroupHelper$", "--", "own-group-helper", marker, "exit-after-record"}, t.TempDir())
	require.NoError(t, err)
	select {
	case <-connection.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("helper did not exit")
	}
	data, err := os.ReadFile(marker)
	require.NoError(t, err)
	pid, err := strconv.Atoi(string(data))
	require.NoError(t, err)

	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = connection.Close(closeCtx)
	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
	}, 2*time.Second, 10*time.Millisecond, "descendant %d survived its harness exiting first", pid)
}
