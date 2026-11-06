//go:build windows

package jsonstdio

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKillErrorRequiresReapedChild(t *testing.T) {
	for _, err := range []error{
		&os.SyscallError{Syscall: "TerminateProcess", Err: syscall.ERROR_ACCESS_DENIED},
		syscall.ERROR_ACCESS_DENIED,
		syscall.EINVAL,
	} {
		exited := make(chan struct{})
		require.False(t, reapedKillErrorIsBenign(exited, err), "an unreaped child must retain the kill failure")
		require.False(t, reapedKillErrorIsBenign(nil, err), "missing process identity cannot prove exit")
		close(exited)
		require.True(t, reapedKillErrorIsBenign(exited, err))
		require.False(t, reapedKillErrorIsBenign(exited, errors.New("another kill failure")))
		require.False(t, reapedKillErrorIsBenign(exited, os.ErrPermission), "only the actual Windows errors are normalized")
	}
}

func TestKillAfterWaitUsesReleasedHandle(t *testing.T) {
	binary, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(binary, "-test.run=^TestJSONStdioEnvironmentHelper$", "--", "environment-helper")
	require.NoError(t, cmd.Run())
	err = cmd.Process.Kill()
	require.ErrorIs(t, err, syscall.EINVAL)
	exited := make(chan struct{})
	close(exited)
	require.True(t, reapedKillErrorIsBenign(exited, err))
}
