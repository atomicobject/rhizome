package claude

import (
	"context"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/stretchr/testify/require"
)

func TestGenerateStructuredArgsAndPrompt(t *testing.T) {
	runner := &fakeRunner{}
	runner.run = func(_ context.Context, spec command.Spec) (command.Result, error) {
		if len(spec.Args) == 1 && spec.Args[0] == "--version" {
			return command.Result{Stdout: "2.1.269 (Claude Code)"}, nil
		}
		require.Equal(t, "summarize", commandInput(t, spec))
		require.Equal(t, "opus", argValue(t, spec.Args, "--model"))
		require.Equal(t, "high", argValue(t, spec.Args, "--effort"))
		require.Equal(t, "", argValue(t, spec.Args, "--tools"))
		require.JSONEq(t, `{"type":"object"}`, argValue(t, spec.Args, "--json-schema"))
		return command.Result{Stdout: `{"result":"ignored","structured_output":{"answer":"yes"}}`}, nil
	}
	result, err := (&Driver{binary: "claude", runner: runner}).Generate(context.Background(), harness.GenerateRequest{Prompt: "summarize", Model: "opus", Effort: "high", Schema: []byte(`{"type":"object"}`)})
	require.NoError(t, err)
	require.JSONEq(t, `{"answer":"yes"}`, string(result))
}

func TestGeneratePlainResultIsJSONString(t *testing.T) {
	runner := &fakeRunner{results: []command.Result{{Stdout: "2.1.269 (Claude Code)"}, {Stdout: `{"result":"plain text"}`}}}
	result, err := (&Driver{binary: "claude", runner: runner}).Generate(context.Background(), harness.GenerateRequest{})
	require.NoError(t, err)
	require.JSONEq(t, `"plain text"`, string(result))
}

func TestGenerateFailureClasses(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		_, err := (&Driver{binary: "claude", runner: notInstalledRunner()}).Generate(context.Background(), harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrNotInstalled)
	})
	t.Run("spawn", func(t *testing.T) {
		_, err := (&Driver{binary: "claude", runner: &fakeRunner{errors: []error{errors.New("spawn")}}}).Generate(context.Background(), harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrSpawn)
	})
	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := (&Driver{binary: "claude", runner: &fakeRunner{errors: []error{context.Canceled}}}).Generate(ctx, harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrTimeout)
	})
	t.Run("not logged in", func(t *testing.T) {
		runner := &fakeRunner{results: []command.Result{{Stdout: "2.1.269 (Claude Code)"}, {ExitCode: 1, Stderr: "Please log in; run claude auth login"}}}
		_, err := (&Driver{binary: "claude", runner: runner}).Generate(context.Background(), harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrNotLoggedIn)
	})
	t.Run("nonzero", func(t *testing.T) {
		runner := &fakeRunner{results: []command.Result{{Stdout: "2.1.269 (Claude Code)"}, {ExitCode: 2, Stderr: "bad option"}}}
		_, err := (&Driver{binary: "claude", runner: runner}).Generate(context.Background(), harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrCommandFailed)
		require.Contains(t, err.Error(), "bad option")
	})
	t.Run("schema mismatch", func(t *testing.T) {
		runner := &fakeRunner{results: []command.Result{{Stdout: "2.1.269 (Claude Code)"}, {Stdout: `{"result":"no structured result"}`}}}
		_, err := (&Driver{binary: "claude", runner: runner}).Generate(context.Background(), harness.GenerateRequest{Schema: []byte(`{"type":"object"}`)})
		require.ErrorIs(t, err, harness.ErrSchemaMismatch)
	})
	t.Run("decode", func(t *testing.T) {
		runner := &fakeRunner{results: []command.Result{{Stdout: "2.1.269 (Claude Code)"}, {Stdout: `not json`}}}
		_, err := (&Driver{binary: "claude", runner: runner}).Generate(context.Background(), harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrDecode)
	})
}

func TestGenerateRejectsOldVersionAndCachesCheck(t *testing.T) {
	t.Run("old version", func(t *testing.T) {
		runner := &fakeRunner{results: []command.Result{{Stdout: "2.0.9 (Claude Code)"}}}
		_, err := (&Driver{binary: "claude", runner: runner}).Generate(context.Background(), harness.GenerateRequest{})
		require.ErrorIs(t, err, harness.ErrInitialize)
		require.Len(t, runner.commands, 1)
	})
	t.Run("cache", func(t *testing.T) {
		versionCalls := 0
		runner := &fakeRunner{}
		runner.run = func(_ context.Context, spec command.Spec) (command.Result, error) {
			if len(spec.Args) == 1 && spec.Args[0] == "--version" {
				versionCalls++
				return command.Result{Stdout: "2.1.269 (Claude Code)"}, nil
			}
			return command.Result{Stdout: `{"result":"ok"}`}, nil
		}
		driver := &Driver{binary: "claude", runner: runner}
		_, err := driver.Generate(context.Background(), harness.GenerateRequest{})
		require.NoError(t, err)
		_, err = driver.Generate(context.Background(), harness.GenerateRequest{})
		require.NoError(t, err)
		require.Equal(t, 1, versionCalls)
	})
}

func TestGenerateChecksClaudeStructuredOutputShape(t *testing.T) {
	for _, test := range []struct {
		name, schema, output string
	}{
		{"object type", `{"type":"object"}`, `[]`},
		{"array type", `{"type":"array"}`, `{}`},
		{"required key", `{"type":"object","required":["answer"]}`, `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{results: []command.Result{{Stdout: "2.1.269 (Claude Code)"}, {Stdout: `{"structured_output":` + test.output + `}`}}}
			_, err := (&Driver{binary: "claude", runner: runner}).Generate(context.Background(), harness.GenerateRequest{Schema: []byte(test.schema)})
			require.ErrorIs(t, err, harness.ErrSchemaMismatch)
		})
	}
}

func TestGenerateCanceledCommandPreservesTimeoutPhase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &fakeRunner{run: func(ctx context.Context, spec command.Spec) (command.Result, error) {
		if len(spec.Args) == 1 && spec.Args[0] == "--version" {
			return command.Result{Stdout: "2.1.269"}, nil
		}
		cancel()
		return command.Result{}, ctx.Err()
	}}
	_, err := (&Driver{binary: "claude", runner: runner}).Generate(ctx, harness.GenerateRequest{})
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
			return command.Result{Stdout: "2.1.269"}, nil
		}
		cancel()
		return command.Result{ExitCode: 7, Stderr: "authentication required"}, nil
	}}
	_, err := (&Driver{binary: "claude", runner: runner}).Generate(ctx, harness.GenerateRequest{})
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.ErrorIs(t, err, harness.ErrNotLoggedIn)
	require.Contains(t, err.Error(), "authentication required")
	require.NotErrorIs(t, err, harness.ErrTimeout)
	require.Len(t, runner.commands, 2, "authentication failure must come from generation")
}
