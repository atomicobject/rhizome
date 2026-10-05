package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func writeReaderEventFile(t *testing.T, root, process string, data []byte, modified time.Time) string {
	t.Helper()
	dir := filepath.Join(root, ".rhizome", "diagnostics", "events")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, "20261004-"+process+"-000001-4194304.jsonl")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	require.NoError(t, os.Chtimes(path, modified, modified))
	return path
}

func readerEventLine(t *testing.T, event Event) []byte {
	t.Helper()
	data, err := json.Marshal(event)
	require.NoError(t, err)
	return append(data, '\n')
}

func TestReadEventsPrioritizesRecentProcessBeforeLargeOlderInfoSegment(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	info := readerEventLine(t, Event{SchemaVersion: SchemaVersion, Time: now.Add(-time.Hour), ProcessID: "zzolder", Level: "INFO", Name: "old", Message: strings.Repeat("i", 1000)})
	writeReaderEventFile(t, root, "zzolder", bytes.Repeat(info, (4<<20)/len(info)+1), now.Add(-time.Hour))
	newest := Event{SchemaVersion: SchemaVersion, Time: now, ProcessID: "aanewer", Level: "ERROR", Name: "newest-error"}
	writeReaderEventFile(t, root, "aanewer", readerEventLine(t, newest), now)
	result, err := ReadEvents(root, Filter{Level: slog.LevelError})
	require.NoError(t, err)
	require.Equal(t, []Event{newest}, result.Events)
	require.True(t, result.Coverage.Truncated)
	require.LessOrEqual(t, result.Coverage.Bytes, int64(4<<20))
	require.NotContains(t, strings.Join(result.Coverage.Warnings, " "), "malformed")
	require.NotContains(t, strings.Join(result.Coverage.Warnings, " "), "unterminated")
}

func TestReadEventsUsesNewestTailAndRetainsChronologicalMatchingSelection(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	older := readerEventLine(t, Event{SchemaVersion: SchemaVersion, Time: now.Add(-time.Hour), ProcessID: "p", Level: "INFO", Name: "old", Message: strings.Repeat("i", 1000)})
	data := bytes.Repeat(older, (4<<20)/len(older)+1)
	var wanted []Event
	for i := 0; i < 6; i++ {
		event := Event{SchemaVersion: SchemaVersion, Time: now.Add(time.Duration(i) * time.Second), ProcessID: "p", Sequence: uint64(i), Level: "ERROR", Name: fmt.Sprintf("recent-%d", i), OperationID: "op", TraceID: "trace", Subsystem: "index"}
		data = append(data, readerEventLine(t, event)...)
		if i == 3 || i == 4 {
			wanted = append(wanted, event)
		}
	}
	writeReaderEventFile(t, root, "p", data, now)
	result, err := ReadEvents(root, Filter{Limit: 2, Level: slog.LevelError, OperationID: "op", TraceID: "trace", Subsystem: "index", Since: now.Add(time.Second), Until: now.Add(4 * time.Second)})
	require.NoError(t, err)
	require.Equal(t, wanted, result.Events)
	require.True(t, result.Coverage.Truncated)
	require.Equal(t, int64(4<<20), result.Coverage.Bytes)
	require.NotContains(t, strings.Join(result.Coverage.Warnings, " "), "malformed")
	require.NotContains(t, strings.Join(result.Coverage.Warnings, " "), "unterminated")
}

func TestReadEventsBudgetBoundaryIsNotCorruption(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	for _, boundary := range []string{"inside-record", "newline", "record-start"} {
		t.Run(boundary, func(t *testing.T) {
			root := t.TempDir()
			old := readerEventLine(t, Event{SchemaVersion: SchemaVersion, Time: now.Add(-time.Second), ProcessID: "p", Level: "INFO", Name: "old", Message: strings.Repeat("x", 1000)})
			newest := Event{SchemaVersion: SchemaVersion, Time: now, ProcessID: "p", Level: "ERROR", Name: "kept"}
			line := readerEventLine(t, newest)
			data := append(old, line...)
			budget := len(line) + 20
			if boundary == "newline" {
				budget = len(line) + 1
			}
			if boundary == "record-start" {
				budget = len(line)
			}
			writeReaderEventFile(t, root, "p", data, now)
			result, err := ReadEvents(root, Filter{MaxBytes: int64(budget)})
			require.NoError(t, err)
			require.True(t, result.Coverage.Truncated)
			require.LessOrEqual(t, result.Coverage.Bytes, int64(budget))
			require.NotContains(t, strings.Join(result.Coverage.Warnings, " "), "malformed")
			require.NotContains(t, strings.Join(result.Coverage.Warnings, " "), "unterminated")
			if boundary != "record-start" {
				require.Equal(t, []Event{newest}, result.Events)
			}
		})
	}
}

func TestReadEventsTailStillWarnsAboutActualCorruption(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	line := readerEventLine(t, Event{SchemaVersion: SchemaVersion, Time: now, ProcessID: "p", Level: "ERROR", Name: "kept"})
	data := append(bytes.Repeat([]byte("old-record\n"), 100), line...)
	data = append(data, []byte("{broken}\n{\"partial\":")...)
	writeReaderEventFile(t, root, "p", data, now)
	result, err := ReadEvents(root, Filter{MaxBytes: int64(len(line) + 100)})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	warnings := strings.Join(result.Coverage.Warnings, " ")
	require.Contains(t, warnings, "malformed event record")
	require.Contains(t, warnings, "unterminated final event record")
}

func TestReadEventsToleratesConcurrentSegmentRotation(t *testing.T) {
	root := t.TempDir()
	r := openTestRecorder(t, root, Options{MaxSegmentBytes: 1024, MaxEventBytes: 1024, MaxBytes: 8192})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			r.Event(context.Background(), slog.LevelInfo, "test", "rotating", "", slog.Int("i", i))
		}
	}()
	for i := 0; i < 30; i++ {
		result, err := ReadEvents(root, Filter{MaxBytes: 2048, Limit: 5})
		require.NoError(t, err)
		require.LessOrEqual(t, result.Coverage.Bytes, int64(2048))
		for j := 1; j < len(result.Events); j++ {
			require.True(t, eventBefore(result.Events[j-1], result.Events[j]))
		}
	}
	<-done
	require.NoError(t, r.Close())
	result, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.NotEmpty(t, result.Events)
	require.Empty(t, result.Coverage.Warnings)
}

func TestReadEventsEqualModificationTimesUseDeterministicFileOrder(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	first := Event{SchemaVersion: SchemaVersion, Time: now, ProcessID: "zz", Level: "ERROR", Name: "first"}
	second := Event{SchemaVersion: SchemaVersion, Time: now, ProcessID: "aa", Level: "ERROR", Name: "second"}
	firstLine := readerEventLine(t, first)
	writeReaderEventFile(t, root, "zz", firstLine, now)
	secondLine := readerEventLine(t, second)
	writeReaderEventFile(t, root, "aa", secondLine, now)
	for i := 0; i < 3; i++ {
		result, err := ReadEvents(root, Filter{MaxBytes: int64(len(firstLine))})
		require.NoError(t, err)
		require.Equal(t, []Event{first}, result.Events)
		require.Equal(t, int64(len(firstLine)), result.Coverage.Bytes)
		require.True(t, result.Coverage.Truncated)
	}
}

func TestReadEventsSharesBudgetWithSlightlyOlderCLIError(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	runtimeLine := readerEventLine(t, Event{SchemaVersion: SchemaVersion, Time: now, ProcessID: "runtime", Level: "INFO", Name: "runtime-info", Message: strings.Repeat("i", 1000)})
	runtimeData := bytes.Repeat(runtimeLine, (4<<20)/len(runtimeLine))
	padding := (4 << 20) - len(runtimeData)
	runtimeData = append(runtimeData[:len(runtimeData)-1], bytes.Repeat([]byte(" "), padding)...)
	runtimeData = append(runtimeData, '\n')
	writeReaderEventFile(t, root, "runtime", runtimeData, now)
	cliError := Event{SchemaVersion: SchemaVersion, Time: now.Add(-time.Second), ProcessID: "cli", Level: "ERROR", Name: "cli-error"}
	writeReaderEventFile(t, root, "cli", readerEventLine(t, cliError), now.Add(-time.Second))
	result, err := ReadEvents(root, Filter{Level: slog.LevelError})
	require.NoError(t, err)
	require.Equal(t, []Event{cliError}, result.Events)
	require.Equal(t, 2, result.Coverage.Files)
	require.True(t, result.Coverage.Truncated)
	require.LessOrEqual(t, result.Coverage.Bytes, int64(4<<20))
	warnings := strings.Join(result.Coverage.Warnings, " ")
	require.NotContains(t, warnings, "malformed")
	require.NotContains(t, warnings, "unterminated")
}

func TestReadEventsDenseHistoryKeepsLargeNewestErrorRecord(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	olderLine := readerEventLine(t, Event{SchemaVersion: SchemaVersion, Time: now.Add(-time.Hour), ProcessID: "old", Level: "INFO", Name: "old"})
	for i := 0; i < 600; i++ {
		writeReaderEventFile(t, root, fmt.Sprintf("old%04d", i), olderLine, now.Add(-time.Hour))
	}
	newest := Event{SchemaVersion: SchemaVersion, Time: now, ProcessID: "new", Level: "ERROR", Name: "newest-error", Message: strings.Repeat("e", 8<<10)}
	writeReaderEventFile(t, root, "new", readerEventLine(t, newest), now)
	result, err := ReadEvents(root, Filter{Level: slog.LevelError})
	require.NoError(t, err)
	require.Equal(t, []Event{newest}, result.Events)
	require.Equal(t, 601, result.Coverage.Files)
	require.LessOrEqual(t, result.Coverage.Bytes, int64(4<<20))
	require.False(t, result.Coverage.Truncated)
	require.Empty(t, result.Coverage.Warnings)
}
