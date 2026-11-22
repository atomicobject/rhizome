//go:build unix

package command

import (
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunReturnsWhileAGrandchildHoldsTheOutputPipe(t *testing.T) {
	start := time.Now()
	// The pipe wait is cut short, which exec may report as an error; only the
	// time matters here.
	_, _ = OSRunner{}.Run(context.Background(), Spec{Path: "/bin/sh", Args: []string{"-c", "sleep 60 & echo listed"}})
	if elapsed := time.Since(start); elapsed > waitDelay+10*time.Second {
		t.Fatalf("Run waited %s for a grandchild's pipe", elapsed)
	}
}

func TestKillTreeStopsWhatTheCommandStarted(t *testing.T) {
	// The grandchild writes no output, so Run returns as soon as sh exits.
	result, err := OSRunner{}.Run(context.Background(), Spec{Path: "/bin/sh", Args: []string{"-c", "sleep 60 >/dev/null 2>&1 & echo $!"}, KillTree: true})
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(result.Stdout))
	if err != nil {
		t.Fatalf("pid: %q", result.Stdout)
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatal("a process the command started outlived it")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestKillTreeCancelStopsTheTree(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := OSRunner{}.Run(ctx, Spec{Path: "/bin/sh", Args: []string{"-c", "sleep 60"}, KillTree: true})
	if err == nil || time.Since(start) > waitDelay+5*time.Second {
		t.Fatalf("Run = %v after %s, want a prompt context error", err, time.Since(start))
	}
}
