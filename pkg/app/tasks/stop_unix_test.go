//go:build !windows

package tasks

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

const stopRunID = "20261122T120000Z-0123abcd"

// A fixture run-due holds run.lock as a session leader with a child in its
// group. One ignores SIGTERM, so Stop must kill it; the other records its
// run on SIGTERM and exits, leaving its child behind. Either way the whole
// tree ends and the run is recorded once, as stopped.
func TestStopEndsTheRunsProcessTreeAndRecordsItOnce(t *testing.T) {
	for _, mode := range []string{"ignore", "record"} {
		t.Run(mode, func(t *testing.T) {
			svc := newService(t, newRepo(t, nil), &fakeAgent{}, "auto")
			ctx := context.Background()
			var item string
			setState(t, svc, func(st *State) {
				item = st.AddItem(Item{Task: "slow", Kind: KindTask, State: StateRunning}, time.Now()).ID
				st.Running = &RunRecord{ID: stopRunID, Task: "slow", Item: item, StartedAt: time.Now(), Stage: StageAgent}
			})
			pidFile := filepath.Join(t.TempDir(), "child")
			helper := exec.Command(os.Args[0], "-test.run=^TestStopHelper$", "--", "stop-helper", mode, svc.stateDir, pidFile)
			helper.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			require.NoError(t, helper.Start())
			exited := make(chan struct{})
			go func() { _ = helper.Wait(); close(exited) }()
			t.Cleanup(func() { _ = syscall.Kill(-helper.Process.Pid, syscall.SIGKILL) })
			require.Eventually(t, func() bool { return svc.RunLockHeld() && fileExists(pidFile) }, 10*time.Second, 20*time.Millisecond)
			data, err := os.ReadFile(pidFile)
			require.NoError(t, err)
			child, err := strconv.Atoi(strings.TrimSpace(string(data)))
			require.NoError(t, err)

			stopped, err := svc.Stop(ctx, 300*time.Millisecond)
			require.NoError(t, err)
			require.True(t, stopped)
			select {
			case <-exited:
			case <-time.After(10 * time.Second):
				t.Fatal("the run-due fixture survived Stop")
			}
			require.Eventually(t, func() bool { return errors.Is(syscall.Kill(child, 0), syscall.ESRCH) }, 5*time.Second, 20*time.Millisecond, "its child survived Stop")

			st := mustState(t, svc)
			require.Nil(t, st.Running)
			require.Empty(t, st.Stopping)
			var records []RunRecord
			for _, r := range st.History {
				if r.ID == stopRunID {
					records = append(records, r)
				}
			}
			require.Len(t, records, 1)
			require.Equal(t, OutcomeStopped, records[0].Outcome)
			require.Equal(t, OutcomeStopped, st.Last["slow"].Outcome)
			if mode == "ignore" {
				it, err := st.Item(item)
				require.NoError(t, err)
				require.Equal(t, StateAttention, it.State)
				require.Equal(t, stoppedMessage, it.Reason)
			}

			again, err := svc.Stop(ctx, time.Second)
			require.NoError(t, err)
			require.False(t, again, "nothing is running any more")
		})
	}
}

// TestStopHelper is the fixture run-due; it does nothing in a normal run.
func TestStopHelper(t *testing.T) {
	arg := slices.Index(os.Args, "stop-helper")
	if arg < 0 {
		return
	}
	mode, stateDir, pidFile := os.Args[arg+1], os.Args[arg+2], os.Args[arg+3]
	terminated := make(chan os.Signal, 1)
	signal.Notify(terminated, syscall.SIGTERM)
	release, ok, err := indexlock.TryAcquireWithOptions(filepath.Join(stateDir, runLock), indexlock.AcquireOptions{Role: "tasks/run"})
	if err != nil || !ok {
		os.Exit(2)
	}
	child := exec.Command("sleep", "300")
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		os.Exit(4)
	}
	<-terminated
	if mode == "ignore" {
		time.Sleep(time.Hour)
	}
	// As run-due does when its context is cancelled mid-run.
	_ = Store{Dir: stateDir}.Update(context.Background(), func(st *State) error {
		r := *st.Running
		r.At, r.Outcome, r.Message = time.Now(), OutcomeFailed, "context canceled"
		st.Record(r)
		return nil
	})
	_ = release()
	os.Exit(0)
}
