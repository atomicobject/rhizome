//go:build !windows

package proctree

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// The child starts a grandchild in its own process group and exits at once,
// before any record; the session still ties the grandchild to the child.
func TestKillReachesReparentedDescendantsInOtherGroups(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "pid")
	cmd := exec.Command(os.Args[0], "-test.run=^TestProctreeHelper$", "--", "proctree-helper", marker)
	Configure(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	tracker := Track(cmd.Process, time.Hour)
	defer tracker.Stop()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(data))
	unrelated := exec.Command("sleep", "30")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()

	_ = tracker.Kill()
	deadline := time.Now().Add(2 * time.Second)
	for !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("reparented grandchild %d survived Kill", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := syscall.Kill(unrelated.Process.Pid, 0); err != nil {
		t.Fatalf("Kill must not touch unrelated processes: %v", err)
	}
}

func TestProctreeHelper(t *testing.T) {
	arg := slices.Index(os.Args, "proctree-helper")
	if arg < 0 {
		return
	}
	grandchild := exec.Command("sleep", "300")
	grandchild.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if slices.Contains(os.Args, "own-session") {
		grandchild.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	if err := grandchild.Start(); err != nil {
		os.Exit(1)
	}
	_ = os.WriteFile(os.Args[arg+1], []byte(strconv.Itoa(grandchild.Process.Pid)), 0o600)
	if slices.Contains(os.Args, "own-session") {
		time.Sleep(time.Minute) // until the test ends it
	}
	os.Exit(0)
}

// Claude Code gives each shell a session of its own. A recorded shell, and
// what it started in its session, are killed even after the child exits.
func TestKillReachesRecordedDescendantsInOtherSessions(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "pid")
	cmd := exec.Command(os.Args[0], "-test.run=^TestProctreeHelper$", "--", "proctree-helper", marker, "own-session")
	Configure(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	tracker := Track(cmd.Process, time.Hour)
	defer tracker.Stop()
	var pid int
	for deadline := time.Now().Add(5 * time.Second); pid == 0; {
		if data, err := os.ReadFile(marker); err == nil {
			pid, _ = strconv.Atoi(string(data))
		}
		if time.Now().After(deadline) {
			t.Fatal("helper never started its grandchild")
		}
		time.Sleep(10 * time.Millisecond)
	}
	tracker.Record() // as Close does before ending the child
	_ = cmd.Process.Signal(syscall.SIGTERM)
	_ = cmd.Wait()
	_ = tracker.Kill()
	deadline := time.Now().Add(2 * time.Second)
	for !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d in its own session survived Kill", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
