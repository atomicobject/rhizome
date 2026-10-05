package serve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

type stopWitnessWriter struct {
	out       io.Writer
	vaultPath string
	observed  chan error
}

func (w *stopWitnessWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("Rhizome serve stopped")) {
		if _, err := os.Stat(appruntime.ManifestPath(w.vaultPath)); !errors.Is(err, os.ErrNotExist) {
			w.observed <- fmt.Errorf("stopped line preceded manifest removal: %v", err)
		} else {
			release, acquired, err := indexlock.TryAcquire(appruntime.LockPath(w.vaultPath))
			if err != nil || !acquired {
				w.observed <- fmt.Errorf("stopped line preceded election release: acquired=%t err=%v", acquired, err)
			} else {
				w.observed <- release()
			}
		}
	}
	return w.out.Write(p)
}

func TestRunReportsStoppedOnlyAfterReleasingVault(t *testing.T) {
	vault := newTestVault(t)
	observed := make(chan error, 1)
	handle := startRun(t, vault, func(opts *Options) {
		opts.Stderr = &stopWitnessWriter{out: opts.Stderr, vaultPath: vault.path, observed: observed}
	})
	handle.waitForManifest(t, vault.path)
	result := handle.stop(t)
	require.NoError(t, result.err)
	require.Contains(t, result.stderr, "Rhizome serve stopped")
	select {
	case err := <-observed:
		require.NoError(t, err)
	default:
		t.Fatal("stopped output was not observed")
	}
}

func TestRunKeepsVaultUntilActiveRequestFinishes(t *testing.T) {
	vault := newTestVault(t)
	entered := make(chan struct{})
	requestCancelled := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	handle := startRun(t, vault, func(opts *Options) {
		opts.RegisterHooks = func(hooks *ControlHooks, _ *bootstrap.LiveRuntime) {
			hooks.SubmitIndex = func(ctx context.Context) (lane.Handle, bool, error) {
				close(entered)
				<-ctx.Done()
				close(requestCancelled)
				<-release
				return nil, false, context.Canceled
			}
		}
	})
	manifest := handle.waitForManifest(t, vault.path)
	client := &appruntime.Client{Manifest: manifest, HTTP: &http.Client{}}
	req, err := client.NewRequest(context.Background(), http.MethodPost, appruntime.IndexJobsPath, nil)
	require.NoError(t, err)
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		resp, err := client.HTTP.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("index request never reached the runtime")
	}

	handle.cancel()
	select {
	case <-requestCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("active request did not receive shutdown cancellation")
	}
	// Shutdown's grace can expire and close the client connection while the
	// handler remains inside a write-capable callback.
	select {
	case <-requestDone:
	case <-time.After(httpShutdownGrace + 2*time.Second):
		t.Fatal("shutdown did not close the active request connection")
	}
	select {
	case result := <-handle.done:
		handle.collected.Store(true)
		t.Fatalf("runtime exited while a request could still write: %v", result.err)
	default:
	}
	require.FileExists(t, appruntime.LockPath(vault.path))
	require.NotContains(t, handle.stderr.String(), "Rhizome serve stopped")
	unblock()
	result := handle.awaitExit(t, 20*time.Second)
	require.NoError(t, result.err)
}

func TestRunFinalizesDiagnosticsBeforeReleasingVault(t *testing.T) {
	vault := newTestVault(t)
	finalized := make(chan error, 1)
	handle := startRun(t, vault, func(opts *Options) {
		opts.FinalizeDiagnostics = func(err error) {
			if err != nil {
				finalized <- err
				return
			}
			if _, err := os.Stat(appruntime.LockPath(vault.path)); err != nil {
				finalized <- err
				return
			}
			reports, err := diagnostics.ReadReports(vault.path, diagnostics.Filter{Kind: "runtime.serve"})
			if err == nil && len(reports.Reports) != 1 {
				err = fmt.Errorf("expected terminal runtime report before release, got %d", len(reports.Reports))
			}
			finalized <- err
		}
	})
	handle.waitForManifest(t, vault.path)
	result := handle.stop(t)
	require.NoError(t, result.err)
	select {
	case err := <-finalized:
		require.NoError(t, err)
	default:
		t.Fatal("diagnostics finalizer did not run")
	}
	reports, err := diagnostics.ReadReports(vault.path, diagnostics.Filter{Kind: "runtime.serve"})
	require.NoError(t, err)
	require.Len(t, reports.Reports, 1)
}

func TestRunDrainsBrowserDiagnosticBeforeReleasingVault(t *testing.T) {
	vault := newTestVault(t)
	opened, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	handle := startRun(t, vault, func(opts *Options) {
		opts.Open = true
		opts.OpenBrowser = func(string) error {
			close(opened)
			<-release
			return errors.New("synthetic browser failure")
		}
	})
	t.Cleanup(unblock)
	select {
	case <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("browser did not open")
	}
	handle.cancel()
	select {
	case <-handle.done:
		handle.collected.Store(true)
		t.Fatal("runtime returned before its browser callback drained")
	case <-time.After(20 * time.Millisecond):
	}
	_, err := os.Stat(appruntime.LockPath(vault.path))
	require.NoError(t, err, "runtime retains ownership while its callback can still write diagnostics")
	unblock()
	require.NoError(t, handle.stop(t).err)
	events, err := diagnostics.ReadEvents(vault.path, diagnostics.Filter{Subsystem: "runtime"})
	require.NoError(t, err)
	var browserFailures int
	for _, event := range events.Events {
		if event.Name == "browser.open_failed" {
			browserFailures++
		}
	}
	require.Equal(t, 1, browserFailures)
}
