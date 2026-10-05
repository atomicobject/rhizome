//go:build !windows

package runtime

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetachProcessStartsANewSession(t *testing.T) {
	cmd := exec.Command(testBinary(t))
	detachProcess(cmd)
	require.True(t, cmd.SysProcAttr.Setsid, "Ctrl-C in the client's terminal must never reach the runtime")
}
