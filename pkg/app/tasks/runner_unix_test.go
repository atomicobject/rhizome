//go:build !windows

package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCheckTimeoutKillsTheWholeTree(t *testing.T) {
	shellAvailable(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	dir := newRepo(t, map[string]string{"slow.yml": "name: Slow\nevery: 1d\ntimeout: 1s\ncheck:\n  command: [sh, -c, 'sh -c \"sleep 300\" & echo $! > " + pidFile + "; wait']\nprompt: x\n"})
	svc := newService(t, dir, &fakeAgent{}, "auto")
	start := time.Now()
	s, _ := svc.RunDue(context.Background())
	if len(s.Outcomes) != 1 || s.Outcomes[0].Outcome != OutcomeFailed || time.Since(start) > 30*time.Second {
		t.Fatalf("expected the check to time out promptly: %+v", s)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d survived the timeout", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
