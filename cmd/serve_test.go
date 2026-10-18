package cmd

import "os"

import "github.com/atomicobject/rhizome/internal/testkit"

import (
	"testing"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/stretchr/testify/require"
)

// The serve command is a thin Cobra adapter over pkg/app/cli/serve; the URL,
// port, and manifest helpers are tested beside their implementation there.

func TestServeCommandExposesRuntimeFlags(t *testing.T) {
	commandOptions := newRuntimeOptionsForTest(t)

	t.Parallel()

	for _, use := range []string{"serve", "start"} {
		cmd := commandOptions.newServeCommand(use, "test", use == "start")
		require.NotNil(t, cmd.Flags().Lookup("headless"), "%s must offer --headless", use)
		require.Equal(t, "false", cmd.Flags().Lookup("headless").DefValue)
		require.Nil(t, cmd.Flags().Lookup("leader-follower"), "%s must not keep follower flags", use)
		require.Nil(t, cmd.Flags().Lookup("leader-follower-log-max-bytes"), "%s must not keep follower flags", use)
		require.Nil(t, cmd.Flags().Lookup("leader-follower-poll-interval"), "%s must not keep follower flags", use)
	}
}

func TestStopCommandExposesAllFlag(t *testing.T) {
	rootCmd := newRootCommand(commandEnv{Getwd: os.Getwd, Config: testkit.Env(t)})
	stopCmd, _, _ := rootCmd.Find([]string{"stop"})

	t.Parallel()

	require.NotNil(t, stopCmd.Flags().Lookup("all"))
	require.NotNil(t, stopCmd.Flags().Lookup("vault"))
}

func TestElectionLossExitCodeIsDistinctFromOutcomeClasses(t *testing.T) {
	t.Parallel()

	// 0/1/2 are the index outcome classes and 130 is interrupt, so the
	// "already running" signal must not collide with any of them (SPEC-0104).
	err := silentExitError{code: appruntime.ExitCodeAlreadyRunning}
	require.Equal(t, 3, err.ExitCode())
}
