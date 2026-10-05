package command

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommandRunnerPreservesCancellation(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()
		_, err := (OSRunner{Label: "fixture command"}).Run(ctx, commandHelperSpec(t, "block", ""))
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
	t.Run("cancel running command", func(t *testing.T) {
		result, err := cancelCommandAfterReady(t, func(ctx context.Context, spec Spec) (Result, error) {
			return (OSRunner{Label: "fixture command"}).Run(ctx, spec)
		})
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, "command stdout", result.Stdout)
		require.Equal(t, "command stderr", result.Stderr)
	})
}

func TestCommandRunnerCompletedExitResults(t *testing.T) {
	for _, test := range []struct {
		mode string
		code int
	}{{"success", 0}, {"nonzero", 7}} {
		t.Run(test.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancelOnCompletedCommand(t, cancel)
			result, err := (OSRunner{Label: "fixture command"}).Run(ctx, commandHelperSpec(t, test.mode, ""))
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.NoError(t, err)
			require.Equal(t, test.code, result.ExitCode)
			require.Equal(t, "command stdout", result.Stdout)
			require.Equal(t, "command stderr", result.Stderr)
		})
	}
}

func TestCommandRunnerRetainsStderrTail(t *testing.T) {
	result, err := (OSRunner{Label: "fixture command"}).Run(context.Background(), commandHelperSpec(t, "stderr-tail", ""))
	require.NoError(t, err)
	require.Zero(t, result.ExitCode)
	stderr := strings.Repeat("0123456789", 2000) + "terminal diagnostic"
	require.Equal(t, stderr[len(stderr)-16*1024:], result.Stderr)
	require.Equal(t, "command stdout", result.Stdout)
}

// The exit log runs after Wait, placing cancellation inside the runner's
// completed-exit classification without relying on timing.
func cancelOnCompletedCommand(t *testing.T, cancel context.CancelFunc) {
	t.Helper()
	previous := log.Writer()
	log.SetOutput(commandExitCancelWriter{cancel: cancel})
	t.Cleanup(func() { log.SetOutput(previous) })
}

type commandExitCancelWriter struct {
	cancel context.CancelFunc
}

func (w commandExitCancelWriter) Write(data []byte) (int, error) {
	if strings.Contains(string(data), "harness fixture command exit ") {
		w.cancel()
	}
	return len(data), nil
}

func cancelCommandAfterReady(t *testing.T, invoke func(context.Context, Spec) (Result, error)) (Result, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := filepath.Join(t.TempDir(), "ready")
	childReady := make(chan bool, 1)
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(ready); err == nil {
				childReady <- true
				cancel()
				return
			}
			select {
			case <-ctx.Done():
				childReady <- false
				return
			case <-ticker.C:
			}
		}
	}()
	result, err := invoke(ctx, commandHelperSpec(t, "block", ready))
	cancel()
	require.True(t, <-childReady, "helper must be running before cancellation")
	return result, err
}

func commandHelperSpec(t *testing.T, mode, ready string) Spec {
	t.Helper()
	binary, err := os.Executable()
	require.NoError(t, err)
	return Spec{Path: binary, Args: []string{"-test.run=^TestCommandHelperProcess$", "--", "harness-command-helper", mode, ready}}
}

func TestCommandHelperProcess(t *testing.T) {
	marker := slices.Index(os.Args, "harness-command-helper")
	if marker < 0 {
		return
	}
	_, _ = os.Stdout.WriteString("command stdout")
	_, _ = os.Stderr.WriteString("command stderr")
	switch os.Args[marker+1] {
	case "stderr-tail":
		_, _ = os.Stderr.WriteString(strings.Repeat("0123456789", 2000) + "terminal diagnostic")
		os.Exit(0)
	case "block":
		if ready := os.Args[marker+2]; ready != "" {
			if err := os.WriteFile(ready, nil, 0o600); err != nil {
				os.Exit(2)
			}
		}
		for {
			time.Sleep(time.Hour)
		}
	case "nonzero":
		os.Exit(7)
	default:
		os.Exit(0)
	}
}
