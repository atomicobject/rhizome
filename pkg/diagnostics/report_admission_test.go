package diagnostics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLatestReplacementPreservesHistoryAtTightByteLimit(t *testing.T) {
	for _, lengths := range []struct {
		name     string
		old, new int
	}{
		{"equal", 160, 160},
		{"smaller", 240, 160},
		{"larger", 160, 240},
	} {
		t.Run(lengths.name, func(t *testing.T) {
			root := t.TempDir()
			now := time.Now().UTC().Truncate(time.Second)
			r := openTestRecorder(t, root, Options{MaxBytes: 4096, MaxReportBytes: 1024, Now: func() time.Time { return now }})
			previous := completedIndexReport("synthetic-old", now, strings.Repeat("a", lengths.old))
			next := completedIndexReport("synthetic-new", now.Add(time.Second), strings.Repeat("b", lengths.new))
			_, oldData, err := r.prepareReport(nil, previous)
			require.NoError(t, err)
			_, newData, err := r.prepareReport(nil, next)
			require.NoError(t, err)
			require.NoError(t, r.PublishReport(nil, previous))
			assertLatestAndByID(t, root, previous.OperationID)

			// Retain the old history report throughout. Peak usage is either
			// old history + old latest + new latest temporary, or old history
			// + new latest + new history temporary. Fill the remaining bytes.
			peakCopies := max(2*len(oldData)+len(newData), len(oldData)+2*len(newData))
			retained := filepath.Join(r.dir, "events", "20261004-retained-000001-0.jsonl")
			seedPaddedEvent(t, retained, int(r.opts.MaxBytes)-peakCopies, now)
			require.NoError(t, r.PublishReport(nil, next))
			assertLatestAndByID(t, root, next.OperationID)
			oldReport, err := ReadReport(root, previous.OperationID)
			require.NoError(t, err, "replacement must preserve the old history report when peak usage fits")
			require.Equal(t, previous.OperationID, oldReport.OperationID)
			require.FileExists(t, retained)
			events, err := ReadEvents(root, Filter{})
			require.NoError(t, err)
			require.Len(t, events.Events, 1)
			require.Equal(t, "retained.history", events.Events[0].Name)
			files, err := r.scanLocked()
			require.NoError(t, err)
			var total int64
			for _, file := range files {
				total += file.size
			}
			require.LessOrEqual(t, total, r.opts.MaxBytes)
			if lengths.new >= lengths.old {
				require.Equal(t, r.opts.MaxBytes, total, "equal and growing replacements exactly fill the final budget")
			}
		})
	}
}

func TestLatestReplacementUsesOneAdditionalArtifactSlot(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	r := openTestRecorder(t, root, Options{Now: func() time.Time { return now }})
	previous := completedIndexReport("synthetic-old", now, "")
	next := completedIndexReport("synthetic-new", now.Add(time.Second), "")
	require.NoError(t, r.PublishReport(nil, previous))
	assertLatestAndByID(t, root, previous.OperationID)
	const retainedCount = maxStoredFiles - 3
	for i := 0; i < retainedCount; i++ {
		path := filepath.Join(r.dir, "events", fmt.Sprintf("20261004-retained-%06d-0.jsonl", i))
		require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o600))
	}
	require.NoError(t, r.PublishReport(nil, next))
	assertLatestAndByID(t, root, next.OperationID)
	_, err := ReadReport(root, previous.OperationID)
	require.NoError(t, err, "the old history report still fits at the artifact limit")
	entries, err := os.ReadDir(filepath.Join(r.dir, "events"))
	require.NoError(t, err)
	require.Len(t, entries, retainedCount)
	files, err := r.scanLocked()
	require.NoError(t, err)
	require.Len(t, files, maxStoredFiles)
}

func TestDuplicateHistoryPublicationPreservesLatest(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	r := openTestRecorder(t, root, Options{})
	report := completedIndexReport("synthetic-duplicate", now, "original")
	require.NoError(t, r.PublishReport(nil, report))
	before, err := os.ReadFile(filepath.Join(r.dir, "latest-index.json"))
	require.NoError(t, err)
	report.Status = "error"
	report.Summary = "must not replace latest"
	require.ErrorContains(t, r.PublishReport(nil, report), "already published")
	after, err := os.ReadFile(filepath.Join(r.dir, "latest-index.json"))
	require.NoError(t, err)
	require.Equal(t, before, after)
	stored, err := ReadReport(root, report.OperationID)
	require.NoError(t, err)
	require.Equal(t, "success", stored.Status)
}

func TestLatestRemainsReadableWhenHistoryPublicationFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix directory permissions enforced for an unprivileged process")
	}
	root := t.TempDir()
	r := openTestRecorder(t, root, Options{})
	reportsDir := filepath.Join(r.dir, "reports")
	require.NoError(t, os.Chmod(reportsDir, 0o500))
	t.Cleanup(func() { require.NoError(t, os.Chmod(reportsDir, 0o700)) })
	report := completedIndexReport("synthetic-history-failure", time.Now().UTC(), "completed work")
	require.Error(t, r.PublishReport(nil, report), "the history directory cannot accept its publication")
	assertLatestAndByID(t, root, report.OperationID)
	latest, err := ReadLatest(root, "index")
	require.NoError(t, err)
	require.Equal(t, "success", latest.Status)
	entries, err := os.ReadDir(reportsDir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func completedIndexReport(id string, finished time.Time, summary string) Report {
	return Report{OperationID: id, Kind: "index", Status: "success", Summary: summary,
		StartedAt: finished.Add(-time.Second), FinishedAt: finished}
}

func assertLatestAndByID(t *testing.T, root, id string) {
	t.Helper()
	latest, err := ReadLatest(root, "index")
	require.NoError(t, err)
	require.Equal(t, id, latest.OperationID)
	byID, err := ReadReport(root, id)
	require.NoError(t, err)
	require.Equal(t, latest, byID)
}

func seedPaddedEvent(t *testing.T, path string, size int, now time.Time) {
	t.Helper()
	data, err := json.Marshal(Event{SchemaVersion: SchemaVersion, Time: now, ProcessID: "synthetic",
		Subsystem: "test", Level: "INFO", Name: "retained.history"})
	require.NoError(t, err)
	require.GreaterOrEqual(t, size, len(data)+1)
	data = append(data, bytes.Repeat([]byte(" "), size-len(data)-1)...)
	data = append(data, '\n')
	require.NoError(t, os.WriteFile(path, data, 0o600))
}
