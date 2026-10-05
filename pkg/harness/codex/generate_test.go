package codex

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/stretchr/testify/require"
)

func TestGenerateArgsPromptAndStructuredResult(t *testing.T) {
	runner := &fakeRunner{}
	runner.run = func(_ context.Context, spec command.Spec) (command.Result, error) {
		if len(spec.Args) == 1 && spec.Args[0] == "--version" {
			return command.Result{Stdout: "codex-cli 0.154.0"}, nil
		}
		require.Equal(t, "/fake/codex", spec.Path)
		require.Equal(t, "summarize", commandInput(t, spec))
		require.Equal(t, []string{"exec", "--ephemeral", "--skip-git-repo-check", "-s", "read-only", "--model", "gpt-test", "--config", `model_reasoning_effort="high"`}, spec.Args[:9])
		outputPath := argValue(t, spec.Args, "--output-last-message")
		require.NotEmpty(t, argValue(t, spec.Args, "--output-schema"))
		require.Equal(t, "-", spec.Args[len(spec.Args)-1])
		require.NoError(t, os.WriteFile(outputPath, []byte(`{"answer":"yes"}`), 0o600))
		return command.Result{}, nil
	}
	driver := &Driver{binary: "codex", runner: runner}
	result, err := driver.Generate(context.Background(), harness.GenerateRequest{
		Prompt: "summarize", Model: "gpt-test", Effort: "high",
		Schema: []byte(`{"type":"object","required":["answer"],"properties":{"answer":{"type":"string"}}}`),
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"answer":"yes"}`, string(result))
}

func TestGenerateFailureClasses(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		_, err := (&Driver{binary: "codex", runner: notInstalledRunner()}).Generate(context.Background(), harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrNotInstalled)
	})
	t.Run("spawn", func(t *testing.T) {
		runner := &fakeRunner{errors: []error{errors.New("spawn failed")}}
		_, err := (&Driver{binary: "codex", runner: runner}).Generate(context.Background(), harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrSpawn)
	})
	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		runner := &fakeRunner{errors: []error{context.Canceled}}
		_, err := (&Driver{binary: "codex", runner: runner}).Generate(ctx, harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrTimeout)
	})
	t.Run("not logged in", func(t *testing.T) {
		runner := &fakeRunner{results: []command.Result{{Stdout: "codex-cli 0.154.0"}, {ExitCode: 1, Stderr: "authentication required; run codex login"}}}
		_, err := (&Driver{binary: "codex", runner: runner}).Generate(context.Background(), harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrNotLoggedIn)
	})
	t.Run("non-zero", func(t *testing.T) {
		runner := &fakeRunner{results: []command.Result{{Stdout: "codex-cli 0.154.0"}, {ExitCode: 2, Stderr: "bad option"}}}
		_, err := (&Driver{binary: "codex", runner: runner}).Generate(context.Background(), harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrCommandFailed)
		require.Contains(t, err.Error(), "bad option")
	})
	t.Run("schema mismatch", func(t *testing.T) {
		runner := &fakeRunner{}
		runner.run = func(_ context.Context, spec command.Spec) (command.Result, error) {
			if len(spec.Args) == 1 && spec.Args[0] == "--version" {
				return command.Result{Stdout: "codex-cli 0.154.0"}, nil
			}
			return command.Result{}, os.WriteFile(argValue(t, spec.Args, "--output-last-message"), []byte(`{"wrong":true}`), 0o600)
		}
		_, err := (&Driver{binary: "codex", runner: runner}).Generate(context.Background(), harness.GenerateRequest{
			Schema: []byte(`{"type":"object","required":["answer"]}`),
		})
		require.ErrorIs(t, err, harness.ErrSchemaMismatch)
	})
}

func TestGenerateRejectsOldVersionAndCachesCheck(t *testing.T) {
	t.Run("old version", func(t *testing.T) {
		runner := &fakeRunner{results: []command.Result{{Stdout: "codex-cli 0.149.0"}}}
		_, err := (&Driver{binary: "codex", runner: runner}).Generate(context.Background(), harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrInitialize)
		require.Len(t, runner.commands, 1)
	})
	t.Run("cache", func(t *testing.T) {
		versionCalls := 0
		runner := &fakeRunner{}
		runner.run = func(_ context.Context, spec command.Spec) (command.Result, error) {
			if len(spec.Args) == 1 && spec.Args[0] == "--version" {
				versionCalls++
				return command.Result{Stdout: "codex-cli 0.154.0"}, nil
			}
			return command.Result{}, os.WriteFile(argValue(t, spec.Args, "--output-last-message"), []byte(`"ok"`), 0o600)
		}
		driver := &Driver{binary: "codex", runner: runner}
		_, err := driver.Generate(context.Background(), harness.GenerateRequest{})
		require.NoError(t, err)
		_, err = driver.Generate(context.Background(), harness.GenerateRequest{})
		require.NoError(t, err)
		require.Equal(t, 1, versionCalls)
	})
}

func argValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("flag %s not found in %v", flag, args)
	return ""
}

func TestGenerateCanceledCommandPreservesTimeoutPhase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &fakeRunner{run: func(ctx context.Context, spec command.Spec) (command.Result, error) {
		if len(spec.Args) == 1 && spec.Args[0] == "--version" {
			return command.Result{Stdout: "0.154.0"}, nil
		}
		cancel()
		return command.Result{}, ctx.Err()
	}}
	_, err := (&Driver{binary: "codex", runner: runner}).Generate(ctx, harness.GenerateRequest{})
	require.ErrorIs(t, err, harness.ErrTimeout)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, harness.ErrCommandFailed)
	require.Len(t, runner.commands, 2, "cancellation must reach generation after version succeeds")
}

func TestGenerateCompletedAuthFailureSurvivesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &fakeRunner{run: func(_ context.Context, spec command.Spec) (command.Result, error) {
		if len(spec.Args) == 1 && spec.Args[0] == "--version" {
			return command.Result{Stdout: "0.154.0"}, nil
		}
		cancel()
		return command.Result{ExitCode: 7, Stderr: "authentication required"}, nil
	}}
	_, err := (&Driver{binary: "codex", runner: runner}).Generate(ctx, harness.GenerateRequest{})
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.ErrorIs(t, err, harness.ErrNotLoggedIn)
	require.Contains(t, err.Error(), "authentication required")
	require.NotErrorIs(t, err, harness.ErrTimeout)
	require.Len(t, runner.commands, 2, "authentication failure must come from generation")
}
