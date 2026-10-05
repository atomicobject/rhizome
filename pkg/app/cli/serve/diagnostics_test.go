package serve

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/stretchr/testify/require"
)

func TestHeadlessRunSuppressesInheritedDiagnosticStderrAndRestoresIt(t *testing.T) {
	vault := newTestVault(t)
	var inherited syncBuffer
	recorder, err := diagnostics.Open(vault.path, diagnostics.Options{Stderr: &inherited})
	require.NoError(t, err)
	defer recorder.Close()
	ctx, cancel := context.WithCancel(diagnostics.WithRecorder(context.Background(), recorder))
	defer cancel()
	handle := &runHandle{cancel: cancel, done: make(chan runResult, 1), stderr: &syncBuffer{}}
	go func() {
		err := Run(ctx, Options{
			VaultName: vault.name, Host: "127.0.0.1", Headless: true,
			Stderr: handle.stderr, Listen: net.Listen,
			RegisterHooks: func(_ *ControlHooks, _ *bootstrap.LiveRuntime) {
				recorder.Event(ctx, slog.LevelError, "test", "inherited.error", "synthetic failure")
			},
		})
		handle.done <- runResult{err: err, stderr: handle.stderr.String()}
	}()
	t.Cleanup(func() {
		cancel()
		if !handle.collected.Load() {
			select {
			case <-handle.done:
			case <-time.After(20 * time.Second):
				t.Error("headless runtime did not stop")
			}
		}
	})
	handle.waitForManifest(t, vault.path)
	require.NoError(t, handle.stop(t).err)
	require.Empty(t, inherited.String(), "structured diagnostic mirrors must not bypass process output capture")
	recorder.Event(context.Background(), slog.LevelError, "test", "after.run", "restored writer")
	require.Contains(t, inherited.String(), "restored writer", "restore must target the original writer after capture closes")
	require.NoError(t, recorder.Close())
	events, err := diagnostics.ReadEvents(vault.path, diagnostics.Filter{Subsystem: "test"})
	require.NoError(t, err)
	require.Len(t, events.Events, 2)
	require.Equal(t, "ERROR", events.Events[0].Level)
	require.Equal(t, "inherited.error", events.Events[0].Name)
}
