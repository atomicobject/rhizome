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

// The child starts a grandchild in its own process group and exits, the
// way Claude Code's shells outlive it. Kill still finds the grandchild.
func TestKillReachesReparentedDescendantsInOtherGroups(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "pid")
	cmd := exec.Command(os.Args[0], "-test.run=^TestProctreeHelper$", "--", "proctree-helper", marker)
	Configure(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	tracker := Track(cmd.Process, 50*time.Millisecond)
	defer tracker.Stop()
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for pid == 0 {
		if data, err := os.ReadFile(marker); err == nil {
			pid, _ = strconv.Atoi(string(data))
		}
		if time.Now().After(deadline) {
			t.Fatal("helper never started its grandchild")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // let the tracker record it
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	unrelated := exec.Command("sleep", "30")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()

	_ = tracker.Kill()
	deadline = time.Now().Add(2 * time.Second)
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
	if err := grandchild.Start(); err != nil {
		os.Exit(1)
	}
	_ = os.WriteFile(os.Args[arg+1], []byte(strconv.Itoa(grandchild.Process.Pid)), 0o600)
	time.Sleep(time.Second)
	os.Exit(0)
}
