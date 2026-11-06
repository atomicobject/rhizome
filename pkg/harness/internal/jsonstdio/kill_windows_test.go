//go:build windows

package jsonstdio

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKillAccessDeniedRequiresReapedChild(t *testing.T) {
	exited := make(chan struct{})
	err := &os.SyscallError{Syscall: "TerminateProcess", Err: syscall.ERROR_ACCESS_DENIED}
	require.False(t, reapedKillErrorIsBenign(exited, err), "an unreaped child must retain the kill failure")
	require.False(t, reapedKillErrorIsBenign(nil, err), "missing process identity cannot prove exit")
	close(exited)
	require.True(t, reapedKillErrorIsBenign(exited, err))
	require.True(t, reapedKillErrorIsBenign(exited, syscall.ERROR_ACCESS_DENIED))
	require.False(t, reapedKillErrorIsBenign(exited, errors.New("another kill failure")))
	require.False(t, reapedKillErrorIsBenign(exited, os.ErrPermission), "only the actual Windows error is normalized")
}
