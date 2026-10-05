package diagnostics

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const processMaxBytes int64 = 64 << 10

func TestDiagnosticsOneShotProcess(t *testing.T) {
	root := os.Getenv("RZM_DIAGNOSTICS_ONESHOT_ROOT")
	if root == "" {
		t.Skip("helper process")
	}
	r, err := Open(root, Options{Role: "cli", Stderr: os.Stderr})
	require.NoError(t, err)
	ready := filepath.Join(root, "ready-"+strconv.Itoa(os.Getpid()))
	require.NoError(t, os.WriteFile(ready, nil, 0o600))
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "start")); err == nil {
			break
		}
		require.True(t, time.Now().Before(deadline), "process barrier timeout")
		time.Sleep(time.Millisecond)
	}
	ctx := operationContext("search", time.Now())
	if os.Getenv("RZM_DIAGNOSTICS_REPORT_ONLY") != "1" {
		r.Event(ctx, slog.LevelInfo, "command", "command.started", "")
		require.Zero(t, r.dropped, "a start event must survive ordinary process contention")
	}
	require.NoError(t, r.PublishReport(ctx, Report{Status: "success"}))
	require.NoError(t, r.Close())
}

func TestConcurrentOneShotProcessesRetainStartsAndReports(t *testing.T) {
	for _, retained := range []int{0, 2300, 4000} {
		t.Run(strconv.Itoa(retained), func(t *testing.T) {
			runConcurrentOneShotProcesses(t, retained, false)
		})
	}
}

func TestConcurrentReportProcessesRetainCompletedEvidence(t *testing.T) {
	runConcurrentOneShotProcesses(t, 2300, true)
}

func runConcurrentOneShotProcesses(t *testing.T, retained int, reportOnly bool) {
	t.Helper()
	root := t.TempDir()
	seedRetainedSegments(t, root, retained)
	executable, err := os.Executable()
	require.NoError(t, err)
	const count = 12
	outputs := make([]bytes.Buffer, count)
	commands := make([]*exec.Cmd, count)
	for i := range commands {
		command := exec.Command(executable, "-test.run=^TestDiagnosticsOneShotProcess$")
		command.Env = append(os.Environ(), "RZM_DIAGNOSTICS_ONESHOT_ROOT="+root)
		if reportOnly {
			command.Env = append(command.Env, "RZM_DIAGNOSTICS_REPORT_ONLY=1")
		}
		command.Stdout, command.Stderr = &outputs[i], &outputs[i]
		require.NoError(t, command.Start())
		t.Cleanup(func() { _ = command.Process.Kill() })
		commands[i] = command
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		ready, err := filepath.Glob(filepath.Join(root, "ready-*"))
		require.NoError(t, err)
		if len(ready) == count {
			break
		}
		require.True(t, time.Now().Before(deadline), "process readiness timeout")
		time.Sleep(time.Millisecond)
	}
	started := time.Now()
	require.NoError(t, os.WriteFile(filepath.Join(root, "start"), nil, 0o600))
	for i, command := range commands {
		if err := command.Wait(); err != nil {
			t.Errorf("writer %d: %v: %s", i, err, outputs[i].String())
		}
		if strings.Contains(outputs[i].String(), "persistence unavailable") {
			t.Errorf("writer %d lost evidence: %s", i, outputs[i].String())
		}
	}
	t.Logf("%d processes, %d retained segments: %s", count, retained, time.Since(started))
	reports, err := ReadReports(root, Filter{})
	require.NoError(t, err)
	require.Len(t, reports.Reports, count)
	// Existing history exceeds the reader's normal limit. Read only the
	// new finalized files to prove every start exists without widening
	// production read bounds or retrying failed caller operations.
	events, err := os.ReadDir(filepath.Join(root, ".rhizome", "diagnostics", "events"))
	require.NoError(t, err)
	eventCount := count
	if reportOnly {
		eventCount = 0
	}
	require.Len(t, events, retained+eventCount)
	for _, entry := range events {
		if strings.Contains(entry.Name(), "-synthetic-") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, ".rhizome", "diagnostics", "events", entry.Name()))
		require.NoError(t, err)
		require.Contains(t, string(data), "command.started")
	}
}

func TestDiagnosticsWriterProcess(t *testing.T) {
	root := os.Getenv("RZM_DIAGNOSTICS_TEST_ROOT")
	if root == "" {
		t.Skip("helper process")
	}
	r, err := Open(root, Options{MaxBytes: processMaxBytes, MaxSegmentBytes: 4096, MaxEventBytes: 2048, MaxReportBytes: 2048, Stderr: io.Discard})
	require.NoError(t, err)
	ctx := operationContext("index", time.Now())
	if os.Getenv("RZM_DIAGNOSTICS_TEST_HOLD") == "1" {
		r.Event(ctx, slog.LevelInfo, "indexing", "index.started", "")
		fmt.Fprintln(os.Stdout, "ready")
		for {
			time.Sleep(time.Hour)
		}
	}
	for i := 0; i < 150; i++ {
		r.Event(ctx, slog.LevelInfo, "indexing", "phase.progress", "synthetic activity", slog.Int("count", i))
	}
	require.NoError(t, r.PublishReport(ctx, Report{Status: "success"}))
	require.NoError(t, r.Close())
}

func TestCrashedWriterReservationsBecomePrunable(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	require.NoError(t, err)
	command := exec.Command(executable, "-test.run=^TestDiagnosticsWriterProcess$")
	command.Env = append(os.Environ(), "RZM_DIAGNOSTICS_TEST_ROOT="+root, "RZM_DIAGNOSTICS_TEST_HOLD=1")
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	command.Stderr = io.Discard
	require.NoError(t, command.Start())
	t.Cleanup(func() { _ = command.Process.Kill() })
	ready := make([]byte, len("ready\n"))
	_, err = io.ReadFull(output, ready)
	require.NoError(t, err)
	require.Equal(t, "ready\n", string(ready))
	dir := filepath.Join(root, ".rhizome", "diagnostics", "events")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	held := filepath.Join(dir, entries[0].Name())
	now := time.Now().UTC()
	old := now.Add(-8 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(held, old, old))
	r := openTestRecorder(t, root, Options{MaxBytes: processMaxBytes, MaxSegmentBytes: 4096, Now: func() time.Time { return now }})
	r.Event(nil, slog.LevelInfo, "test", "admission", "")
	_, err = os.Stat(held)
	require.NoError(t, err, "live segment is protected even when old")
	require.NoError(t, command.Process.Kill())
	require.Error(t, command.Wait())
	now = now.Add(24 * time.Hour)
	r.Event(nil, slog.LevelInfo, "test", "after.crash", "")
	require.NoFileExists(t, held, "OS lock release allows pruning the crashed writer")
}

func TestConcurrentProcessesRespectTotalQuotaAndLeaveReadableReports(t *testing.T) {
	root := t.TempDir()
	checker := openTestRecorder(t, root, Options{MaxBytes: processMaxBytes, MaxSegmentBytes: 4096, MaxReportBytes: 2048})
	executable, err := os.Executable()
	require.NoError(t, err)
	var commands []*exec.Cmd
	outputs := make([]bytes.Buffer, 6)
	for i := 0; i < 6; i++ {
		command := exec.Command(executable, "-test.run=^TestDiagnosticsWriterProcess$")
		command.Env = append(os.Environ(), "RZM_DIAGNOSTICS_TEST_ROOT="+root)
		command.Stdout = &outputs[i]
		command.Stderr = &outputs[i]
		require.NoError(t, command.Start())
		commands = append(commands, command)
	}
	finished := make(chan struct{})
	failures := make(chan error, len(commands))
	go func() {
		var wg sync.WaitGroup
		for i, command := range commands {
			wg.Add(1)
			go func(command *exec.Cmd, output *bytes.Buffer) {
				defer wg.Done()
				if err := command.Wait(); err != nil {
					failures <- fmt.Errorf("writer process: %w: %s", err, output.String())
				}
			}(command, &outputs[i])
		}
		wg.Wait()
		close(finished)
	}()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for running := true; running; {
		select {
		case <-finished:
			running = false
		case <-ticker.C:
			// Publication uses the same guard. Event appends may continue, but
			// their reserved capacities were admitted before acquiring it.
			err := checker.withGuard(func() error {
				var total int64
				err := filepath.WalkDir(checker.dir, func(path string, entry os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if entry.IsDir() {
						return nil
					}
					info, err := entry.Info()
					if err != nil {
						return err
					}
					total += info.Size()
					return nil
				})
				if err != nil {
					return err
				}
				if total > processMaxBytes {
					return fmt.Errorf("diagnostics size %d exceeds %d", total, processMaxBytes)
				}
				return nil
			})
			if err != errGuardBusy {
				require.NoError(t, err)
			}
		}
	}
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	latest, err := ReadLatest(root, "index")
	require.NoError(t, err)
	require.Equal(t, "success", latest.Status)
	reports, err := ReadReports(root, Filter{})
	require.NoError(t, err)
	require.NotEmpty(t, reports.Reports)
	events, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.NotEmpty(t, events.Events)
	for _, event := range events.Events {
		require.NotEmpty(t, event.ProcessID)
		require.Positive(t, event.PID)
	}
}
