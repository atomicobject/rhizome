//go:build windows

package runtime

import (
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetachProcessUsesDetachedCreationFlags(t *testing.T) {
	cmd := exec.Command(testBinary(t))
	detachProcess(cmd)
	flags := cmd.SysProcAttr.CreationFlags
	require.NotZero(t, flags&detachedProcess, "the runtime must have no console")
	require.NotZero(t, flags&uint32(syscall.CREATE_NEW_PROCESS_GROUP), "Ctrl-C must never reach the runtime")
	require.NotZero(t, flags&createNoWindow, "the runtime must show no window")
}
