//go:build !windows

package jsonstdio

import (
	"context"
	"errors"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

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
