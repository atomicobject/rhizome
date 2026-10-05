package diagnostics

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCLISegmentReservationsPreserveRetainedHistory(t *testing.T) {
	root := t.TempDir()
	seedRetainedSegments(t, root, 1)
	history := filepath.Join(root, ".rhizome", "diagnostics", "events", "20261004-synthetic-000000-0.jsonl")
	require.NoError(t, os.WriteFile(history, bytes.Repeat([]byte("x"), 2<<20), 0o600))
	for i := 0; i < 2; i++ {
		runtime := openTestRecorder(t, root, Options{Role: "runtime"})
		runtime.Event(nil, slog.LevelInfo, "runtime", "runtime.started", "")
	}
	for i := 0; i < 14; i++ {
		var stderr bytes.Buffer
		cli := openTestRecorder(t, root, Options{Role: "cli", Stderr: &stderr})
		cli.Event(nil, slog.LevelInfo, "command", "command.started", "")
		require.Empty(t, stderr.String())
		require.NotNil(t, cli.file)
		require.True(t, strings.HasSuffix(cli.file.Name(), "-65536.jsonl"))
	}
	require.FileExists(t, history)
	checker := openTestRecorder(t, root, Options{})
	files, err := checker.scanLocked()
	require.NoError(t, err)
	var reserved int64
	for _, file := range files {
		reserved += file.size
	}
	require.Equal(t, int64((2<<20)+(2*DefaultMaxSegmentBytes)+(14*defaultCLISegmentBytes)), reserved)
}

func TestClosedSegmentsFinalizeAndRetainTheirBytes(t *testing.T) {
	root := t.TempDir()
	recorder := openTestRecorder(t, root, Options{Role: "cli"})
	recorder.Event(nil, slog.LevelInfo, "command", "command.started", "")
	active := recorder.file.Name()
	require.NoError(t, recorder.Close())
	require.NoFileExists(t, active)
	entries, err := os.ReadDir(filepath.Join(recorder.dir, "events"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.True(t, strings.HasSuffix(entries[0].Name(), "-0.jsonl"))
	files, err := recorder.scanLocked()
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.False(t, files[0].active)
	require.Less(t, files[0].size, defaultCLISegmentBytes)
	result, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
}

func TestLegacyUnlockedReservationNamesRemainPrunable(t *testing.T) {
	root := t.TempDir()
	recorder := openTestRecorder(t, root, Options{})
	legacy := filepath.Join(recorder.dir, "events", "20260901-synthetic-000001-4194304.jsonl")
	require.NoError(t, os.WriteFile(legacy, []byte("synthetic history\n"), 0o600))
	old := time.Now().Add(-8 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(legacy, old, old))
	recorder.Event(nil, slog.LevelInfo, "command", "command.started", "")
	require.NoFileExists(t, legacy)
}

func TestEventReservationMatchesStoredFilenameContract(t *testing.T) {
	for _, name := range []string{
		"20261004-synthetic-000001-65536.jsonl",
		"20261004-a-000001-0.jsonl",
		"20261004-a_b-c-000001-0.jsonl",
		"20261004-a-000000000-00.jsonl",
		"20261004-a-000001-9999999999999999999999999.jsonl",
		"20261004--000001-0.jsonl",
		"20261004-a-1-0.jsonl",
		"20261004-a-000001-.jsonl",
		"20261004-a-000001-0.json",
		"20261004-a b-000001-0.jsonl",
		"2026abcd-a-000001-0.jsonl",
		"synthetic.jsonl",
	} {
		t.Run(name, func(t *testing.T) {
			capacity, ok := eventReservation(name)
			parts := eventFilename.FindStringSubmatch(name)
			require.Equal(t, len(parts) > 0, ok)
			if ok {
				require.Equal(t, parts[4], capacity)
			}
		})
	}
}
