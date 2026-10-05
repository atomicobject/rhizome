//go:build !windows

package cmd

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDelegatedProcessOwnsLauncherPIDAndReceivesTermination(t *testing.T) {
	const helper = "RZM_TEST_EXEC_DELEGATE"
	if phase := os.Getenv(helper); phase != "" {
		if phase == "launcher" {
			_ = os.Setenv(helper, "target")
			_ = os.Setenv(repoDelegatedEnv, "")
			os.Exit(executeRepoBinary(os.Args[0], []string{"-test.run=^TestDelegatedProcessOwnsLauncherPIDAndReceivesTermination$"}))
		}
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		if os.Getenv(repoDelegatedEnv) != "1" {
			os.Exit(4)
		}
		if err := os.WriteFile(os.Getenv("RZM_TEST_EXEC_READY"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			os.Exit(5)
		}
		<-signals
		os.Exit(23)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	command := exec.Command(os.Args[0], "-test.run=^TestDelegatedProcessOwnsLauncherPIDAndReceivesTermination$")
	command.Env = append(os.Environ(), helper+"=launcher", "RZM_TEST_EXEC_READY="+ready)
	require.NoError(t, command.Start())
	t.Cleanup(func() { _ = command.Process.Kill() })
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(ready)
		return err == nil && string(data) == strconv.Itoa(command.Process.Pid)
	}, 5*time.Second, 10*time.Millisecond, "the delegated command must retain its supervisor's PID")
	require.NoError(t, command.Process.Signal(syscall.SIGTERM))
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit)
		require.Equal(t, 23, exit.ExitCode())
	case <-time.After(5 * time.Second):
		t.Fatal("delegated command did not handle termination")
	}
}
