//go:build !windows

package hook

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAHandoffIsSupervisedUnderTheSameDeadline(t *testing.T) {
	root := repo(t)
	payload := `{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"from the pin"}}`
	script := func(name, body string) string {
		path := filepath.Join(t.TempDir(), name)
		require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755))
		return path
	}
	pidFile := filepath.Join(root, "grandchild.pid")
	cases := []struct {
		name, target, want, logged string
	}{
		// The host's payload reaches the child, and its JSON reaches the host.
		{"passes through", script("echo", "cat"), payload, ""},
		{"nonzero exit", script("exit37", "cat >/dev/null; echo '"+payload+"'; exit 37"), "", "exit status 37"},
		{"not JSON", script("garbage", "echo 'not json'"), "", "output is not JSON"},
		{"missing binary", filepath.Join(root, "no-such-rzm"), "", "no-such-rzm"},
		{"timeout", script("slow", "sleep 30 & echo $! > '"+pidFile+"'; sleep 30"), "", "timed out"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			started := time.Now()
			Delegate(context.Background(), c.target, os.Environ(), Options{
				Harness: HarnessClaude, Point: PostToolUse, Stdin: strings.NewReader(payload), Stdout: &out, Cwd: root,
				Deadline: 500 * time.Millisecond,
			})
			require.Less(t, time.Since(started), 2*time.Second)
			require.Equal(t, c.want, strings.TrimSpace(out.String()))
			if c.logged != "" {
				log, err := os.ReadFile(filepath.Join(root, ".rhizome", "hooks", "diagnostics.log"))
				require.NoError(t, err)
				require.Contains(t, string(log), c.logged)
			}
		})
	}
	data, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return !processAlive(pid) }, 2*time.Second, 20*time.Millisecond, "the child's process tree was killed")
}
