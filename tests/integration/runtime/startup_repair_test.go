//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestStartPreservesRestoredNoteWithStaleCommittedRepairJournal(t *testing.T) {
	t.Parallel()
	vault := newRuntimeVault(t, "")
	const note = "notes/guide.md"
	notePath := filepath.Join(vault.root, filepath.FromSlash(note))
	notePath, err := filepath.EvalSymlinks(notePath)
	require.NoError(t, err)
	restored, err := os.ReadFile(notePath)
	require.NoError(t, err)
	const transactionID = "transaction:startup-restored-note"
	digest := strings.TrimPrefix(validate.SourceHash([]byte(transactionID)), "sha256:")
	journalDir := filepath.Join(vault.root, ".rhizome", "repair-journal", "v1", digest)
	require.NoError(t, os.MkdirAll(journalDir, 0o700))
	// The committed edit was restored through Git, including removal of its
	// backup. Preserve that state while leaving valid recovery evidence behind.
	manifest, err := json.Marshal(map[string]any{
		"version": 1, "transactionId": transactionID,
		"planFingerprint": validate.SourceHash([]byte("startup repair plan")),
		"createdAt":       "2026-09-28T12:00:00Z", "changed": []string{note},
		"checks": []string{"ontology"},
		"entries": []map[string]any{{
			"path": note, "originalPath": note,
			"originalExists": true, "originalMode": uint32(0o644),
			"originalHash": validate.SourceHash(restored),
			"finalExists":  true, "finalMode": uint32(0o644),
			"finalHash":  validate.SourceHash([]byte("# Repaired guide\n")),
			"backupPath": notePath + ".rzm-repair-" + digest[:12] + "-000.backup",
			"stagePath":  notePath + ".rzm-repair-" + digest[:12] + "-000.stage",
		}},
	})
	require.NoError(t, err)
	manifestPath := filepath.Join(journalDir, "manifest.json")
	markerPath := filepath.Join(journalDir, "COMMITTED")
	require.NoError(t, os.WriteFile(manifestPath, manifest, 0o600))
	require.NoError(t, os.WriteFile(markerPath, []byte("committed\n"), 0o600))

	logPath := filepath.Join(t.TempDir(), "startup.log")
	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = logFile.Close() })
	command := exec.Command(vault.binary, "start", "--open=false", "--port", "0")
	command.Dir, command.Env = vault.root, vault.environment(nil)
	command.Stdout, command.Stderr = logFile, logFile
	require.NoError(t, command.Start())
	vault.rememberRuntimePID(command.Process.Pid)
	exited := make(chan struct{})
	go func() { _ = command.Wait(); close(exited) }()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-exited
	})
	require.Eventually(t, func() bool {
		_, health, err := appruntime.LiveManifest(context.Background(), vault.root)
		return err == nil && health.PID == command.Process.Pid && health.Ready
	}, 60*time.Second, 200*time.Millisecond, "startup must become ready with a stale committed repair")
	output, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(output), "repair journal(s) need recovery review")
	require.Contains(t, string(output), "Validation and repair writes remain blocked")
	require.Contains(t, string(output), "rzm validate")

	blocked := vault.run(nil, "agent", "validate")
	require.Error(t, blocked.err, blocked.stdout+blocked.stderr)
	require.Contains(t, blocked.stdout+blocked.stderr, "pending_repair_journal")
	require.Contains(t, blocked.stdout+blocked.stderr, note)
	for path, want := range map[string][]byte{
		notePath: restored, manifestPath: manifest, markerPath: []byte("committed\n"),
	} {
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, want, got, "startup and validation must preserve %s", path)
	}
}
