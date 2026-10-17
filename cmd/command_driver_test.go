package cmd

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/stretchr/testify/require"
)

func runRootCLI(t *testing.T, ctx context.Context, args []string) (string, string, error) {
	return runRootCLIWithRootSilence(t, ctx, args, true)
}

func runRootCLIWithInput(t *testing.T, ctx context.Context, args []string, input string) (string, string, error) {
	t.Helper()
	app := newRootCommand(commandEnvFromContext(ctx))
	app.SetIn(strings.NewReader(input))
	stdout, stderr, err := runCommandInvocation(t, app, ctx, args, true)
	return strings.TrimSpace(stdout), strings.TrimSpace(stderr), err
}

func runRootCLIWithRootSilence(t *testing.T, ctx context.Context, args []string, silence bool) (string, string, error) {
	t.Helper()
	app := newRootCommand(commandEnvFromContext(ctx))
	stdout, stderr, err := runCommandInvocation(t, app, ctx, args, silence)
	return strings.TrimSpace(stdout), strings.TrimSpace(stderr), err
}

func runCLI(t *testing.T, args []string, stdin string) (string, string, error) {
	return runCLIWithVault(t, args, stdin, "")
}

func runCLIWithVault(t *testing.T, args []string, stdin, vault string) (string, string, error) {
	t.Helper()
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv
	env.WorkingDir, _ = os.Getwd()
	if vault != "" {
		env.WorkingDir = vault
	}
	app := newRootCommand(commandEnv{Config: env})
	app.SetIn(strings.NewReader(stdin))
	return runCommandInvocation(t, app, context.Background(), args, true)
}

// Capture legacy terminal writes and Cobra writers through the same invocation.
func runCommandInvocation(t *testing.T, app *commandInvocation, ctx context.Context, args []string, silence bool) (string, string, error) {
	t.Helper()
	originalOut, originalErr := os.Stdout, os.Stderr
	defer func() { os.Stdout, os.Stderr = originalOut, originalErr }()
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	errR, errW, err := os.Pipe()
	require.NoError(t, err)
	var stdout, stderr []byte
	var readers sync.WaitGroup
	readErrors := make(chan error, 2)
	read := func(dst *[]byte, src *os.File) {
		defer readers.Done()
		data, err := io.ReadAll(src)
		if err != nil {
			readErrors <- err
			return
		}
		*dst = data
	}
	readers.Add(2)
	go read(&stdout, outR)
	go read(&stderr, errR)
	os.Stdout, os.Stderr = outW, errW
	app.SetOut(outW)
	app.SetErr(errW)
	app.SilenceUsage, app.SilenceErrors = silence, silence
	app.SetArgs(args)
	_, runErr := app.ExecuteContextC(ctx)
	require.NoError(t, outW.Close())
	require.NoError(t, errW.Close())
	readers.Wait()
	close(readErrors)
	for err := range readErrors {
		require.NoError(t, err)
	}
	require.NoError(t, outR.Close())
	require.NoError(t, errR.Close())
	return string(stdout), string(stderr), runErr
}
