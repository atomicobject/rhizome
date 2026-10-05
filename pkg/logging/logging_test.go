package logging

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/stretchr/testify/require"
)

func TestStandardBridgeRestoresOutOfOrderAndKeepsPayloadPrivate(t *testing.T) {
	original, flags := log.Writer(), log.Flags()
	var console bytes.Buffer
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &console})
	require.NoError(t, err)
	ctx := diagnostics.WithRecorder(context.Background(), recorder)
	first := InstallStandard(ctx)
	second := InstallStandard(ctx)
	first()
	require.True(t, StandardInstalled())
	log.Print("live: search ready query=SECRET_QUERY")
	require.Empty(t, console.String(), "info must stay off the terminal")
	log.Print("Warning: database is locked provider=SECRET_TOKEN")
	log.Print("Error: operation failed input=SECRET_BODY")
	second()
	require.Equal(t, original, log.Writer())
	require.Equal(t, flags, log.Flags())
	require.NoError(t, recorder.Close())
	require.Contains(t, console.String(), "WARN")
	require.Contains(t, console.String(), "ERROR")
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, events.Events, 3)
	require.Equal(t, "database_locked", events.Events[1].Attributes["reason_code"])
	data, err := os.ReadFile(filepath.Join(root, ".rhizome", "diagnostics", "events", eventsFile(t, root)))
	require.NoError(t, err)
	require.NotContains(t, string(data), "SECRET")
}

func eventsFile(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".rhizome", "diagnostics", "events"))
	require.NoError(t, err)
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".jsonl" {
			return entry.Name()
		}
	}
	t.Fatal("event segment absent")
	return ""
}

func TestStandardBridgePreservesLegacyLogs(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, ".rhizome", "logs.d", "rhizome-old.log")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacy), 0700))
	require.NoError(t, os.WriteFile(legacy, []byte("old content"), 0600))
	recorder, err := diagnostics.Open(root, diagnostics.Options{})
	require.NoError(t, err)
	restore := InstallStandard(diagnostics.WithRecorder(context.Background(), recorder))
	log.Print("live: startup ready")
	require.True(t, StandardInstalled())
	restore()
	require.NoError(t, recorder.Close())
	data, err := os.ReadFile(legacy)
	require.NoError(t, err)
	require.Equal(t, "old content", string(data))
	require.NoFileExists(t, filepath.Join(root, ".rhizome", "logs"))
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, events.Events, 1)
}

func TestStandardWithoutRecorderPreservesOriginalWriter(t *testing.T) {
	writer, flags := log.Writer(), log.Flags()
	restore := InstallStandard(context.Background())
	defer restore()
	require.Equal(t, writer, log.Writer())
	require.Equal(t, flags, log.Flags())
	require.False(t, StandardInstalled())
}
