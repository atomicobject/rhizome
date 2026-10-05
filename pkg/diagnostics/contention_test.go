package diagnostics

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConcurrentCompletedReportsSurviveOrdinaryContention(t *testing.T) {
	root := t.TempDir()
	const count = 12
	start := make(chan struct{})
	errors := make(chan error, count)
	var writers sync.WaitGroup
	for i := 0; i < count; i++ {
		recorder := openTestRecorder(t, root, Options{})
		writers.Add(1)
		go func() {
			defer writers.Done()
			ctx, cancel := context.WithCancel(operationContext("index", time.Now()))
			cancel()
			<-start
			errors <- recorder.PublishReport(ctx, Report{Status: "canceled"})
		}()
	}
	close(start)
	writers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	result, err := ReadReports(root, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Reports, count)
}

func TestReportGuardWaitIsBoundedAndRecovers(t *testing.T) {
	root := t.TempDir()
	recorder := openTestRecorder(t, root, Options{})
	guard, err := openRegular(filepath.Join(root, ".rhizome", "diagnostics", ".guard"), os.O_CREATE|os.O_RDWR)
	require.NoError(t, err)
	defer guard.Close()
	require.NoError(t, tryFileLock(guard))
	defer unlockFile(guard)
	ctx := operationContext("index", time.Now())
	started := time.Now()
	require.ErrorIs(t, recorder.PublishReport(ctx, Report{Status: "success"}), errGuardBusy)
	require.Less(t, time.Since(started), reportGuardWait+500*time.Millisecond, "diagnostics must not wait indefinitely for a stuck writer")
	require.NoError(t, unlockFile(guard))
	require.NoError(t, recorder.PublishReport(ctx, Report{Status: "success"}))
}

func TestSegmentAdmissionOutlastsTransientGuardContention(t *testing.T) {
	root := t.TempDir()
	var stderr bytes.Buffer
	recorder := openTestRecorder(t, root, Options{Role: "cli", Stderr: &stderr})
	guard, err := openRegular(filepath.Join(recorder.dir, ".guard"), os.O_CREATE|os.O_RDWR)
	require.NoError(t, err)
	defer guard.Close()
	require.NoError(t, tryFileLock(guard))
	finished := make(chan struct{})
	go func() {
		recorder.Event(context.Background(), slog.LevelInfo, "command", "command.started", "")
		close(finished)
	}()
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, unlockFile(guard))
	<-finished
	require.Empty(t, stderr.String())
	result, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
}

func TestSegmentGuardWaitIsBoundedAndRecovers(t *testing.T) {
	root := t.TempDir()
	var stderr bytes.Buffer
	recorder := openTestRecorder(t, root, Options{Role: "cli", Stderr: &stderr})
	guard, err := openRegular(filepath.Join(recorder.dir, ".guard"), os.O_CREATE|os.O_RDWR)
	require.NoError(t, err)
	defer guard.Close()
	require.NoError(t, tryFileLock(guard))
	started := time.Now()
	recorder.Event(nil, slog.LevelInfo, "command", "command.started", "")
	require.Less(t, time.Since(started), segmentGuardWait+500*time.Millisecond)
	require.Contains(t, stderr.String(), errGuardBusy.Error())
	require.NoError(t, unlockFile(guard))
	recorder.Event(nil, slog.LevelInfo, "command", "command.finished", "")
	result, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.EqualValues(t, 1, result.Events[0].Attributes["diagnostics.dropped"])
}
