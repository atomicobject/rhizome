//go:build !windows

package tasks

import (
	"context"
	"os"
	"os/exec"
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

// A check that leaves a descendant in another process group holding its
// output pipes must not keep the run lock after its timeout.
func TestCheckTimeoutWithAnEscapedDescendantReleasesTheLock(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, map[string]string{"escape.yml": "name: Escape\nevery: 1d\ntimeout: 1s\ncheck:\n  command: [sh, -c, 'set -m; sleep 300 & sleep 300']\nprompt: x\n"})
	svc := newService(t, dir, &fakeAgent{}, "auto")
	start := time.Now()
	s, err := svc.RunDue(context.Background())
	if err != nil || len(s.Outcomes) != 1 || s.Outcomes[0].Outcome != OutcomeFailed {
		t.Fatalf("expected a failed check, got %+v %v", s, err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("the pass took %s; the escaped descendant held it", elapsed)
	}
	if again, err := svc.RunDue(context.Background()); err != nil || again.Skipped != "" {
		t.Fatalf("the run lock should be free again: %+v %v", again, err)
	}
}

// A check that starts a child in a session of its own just before its
// timeout must not leave that child running.
func TestCheckTimeoutKillsALateOwnSessionChild(t *testing.T) {
	shellAvailable(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("needs python3 to call setsid")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	check := "name: Late\nevery: 1d\ntimeout: 1s\ncheck:\n  command: [sh, -c, 'sleep 0.5; python3 -c \"import os,subprocess; p=subprocess.Popen([\\\"sleep\\\",\\\"300\\\"], start_new_session=True); open(\\\"" + pidFile + "\\\",\\\"w\\\").write(str(p.pid)); p.wait()\"']\nprompt: x\n"
	dir := newRepo(t, map[string]string{"late.yml": check})
	svc := newService(t, dir, &fakeAgent{}, "auto")
	s, _ := svc.RunDue(context.Background())
	if len(s.Outcomes) != 1 || s.Outcomes[0].Outcome != OutcomeFailed {
		t.Fatalf("expected the check to time out: %+v", s)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("own-session child %d survived the check timeout", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
