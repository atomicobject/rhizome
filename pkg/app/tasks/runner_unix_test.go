//go:build !windows

package tasks

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

// Drive deadline expiry after readiness; the configured elapsed timeout is
// covered by TestCheckTimeoutKillsTheWholeTree.
type checkDeadlineContext struct {
	context.Context
	waiting chan struct{}
	expired chan struct{}
	once    sync.Once
}

func (c *checkDeadlineContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.expired
}

func (c *checkDeadlineContext) Err() error {
	select {
	case <-c.expired:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func TestCheckTimeoutKillsALateOwnSessionChild(t *testing.T) {
	python, err := exec.LookPath("python3")
	require.NoError(t, err)
	pidFile := filepath.Join(t.TempDir(), "pid")
	input, trigger, err := os.Pipe()
	require.NoError(t, err)
	defer input.Close()
	defer trigger.Close()
	cmd := exec.Command(python, "-c", `import subprocess, sys
sys.stdin.readline()
p = subprocess.Popen(["sleep", "300"], start_new_session=True)
with open(sys.argv[1], "w") as f:
    f.write(str(p.pid))
p.wait()
`, pidFile)
	cmd.Stdin = input
	ctx := &checkDeadlineContext{Context: context.Background(), waiting: make(chan struct{}), expired: make(chan struct{})}
	expire := sync.OnceFunc(func() { close(ctx.expired) })
	done := make(chan struct{})
	var runErr error
	go func() {
		_, runErr = runBounded(ctx, cmd, io.Discard)
		close(done)
	}()
	t.Cleanup(func() { expire(); <-done })
	// Run observes cancellation only after its initial process-tree snapshot.
	select {
	case <-ctx.waiting:
	case <-done:
		t.Fatalf("check exited before waiting for its deadline: %v", runErr)
	}
	_, err = trigger.Write([]byte("\n"))
	require.NoError(t, err)
	var pid int
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(string(data))
		return err == nil && pid > 0
	}, 5*time.Second, time.Millisecond, "fixture did not publish its own-session child")
	require.NoError(t, syscall.Kill(pid, 0), "child must exist before the deadline")
	expire()
	<-done
	require.ErrorIs(t, runErr, context.DeadlineExceeded)
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("own-session child %d survived the check timeout", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
