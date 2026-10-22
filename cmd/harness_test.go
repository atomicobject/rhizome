package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/cli/harnesscheck"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

// runHarnessCLI executes a fresh root tree built from env, with the harness
// family bound to registry, through the invocation wrapper.
func runHarnessCLI(t *testing.T, env vaultconfig.Env, registry harnessRegistry, args ...string) (string, string, error) {
	t.Helper()
	app := newRootCommand(commandEnv{Config: env})
	harnessCmd, _, err := app.Find([]string{"harness"})
	require.NoError(t, err)
	app.RemoveCommand(harnessCmd)
	app.AddCommand(newAgentHarnessCmdWithRegistry(registry))
	return runCommandInvocation(t, app, t.Context(), args, true)
}

func unprobedHarnesses(t *testing.T) harnessRegistry {
	return func() map[harness.Kind]harness.Harness {
		t.Fatal("configured status must not construct or probe providers")
		return nil
	}
}

func TestAgentHarnessConfiguredStatus(t *testing.T) {
	for _, tc := range []struct {
		name, config, want string
	}{
		{name: "missing", want: ""},
		{name: "unset", config: "agent: {}\n", want: ""},
		{name: "claude", config: "agent:\n  harness: claude\n  harnesses:\n    claude:\n      model: private-model\n", want: "claude"},
		{name: "codex", config: "agent:\n  harness: codex\n", want: "codex"},
		{name: "unknown", config: "agent:\n  harness: other\n", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := testkit.Env(t)
			env.CLIConfigFile = filepath.Join(env.WorkingDir, "config.yml")
			if tc.config != "" {
				require.NoError(t, os.WriteFile(env.CLIConfigFile, []byte(tc.config), 0o600))
			}
			stdout, stderr, err := runHarnessCLI(t, env, unprobedHarnesses(t), "harness", "status", "--configured")
			require.NoError(t, err, stderr)
			require.JSONEq(t, `{"harness":"`+tc.want+`"}`, stdout)
			if tc.config != "" {
				after, err := os.ReadFile(env.CLIConfigFile)
				require.NoError(t, err)
				require.Equal(t, tc.config, string(after))
			} else {
				require.NoFileExists(t, env.CLIConfigFile)
			}
		})
	}
}

func TestAgentHarnessConfiguredStatusRejectsHarnessFlag(t *testing.T) {
	_, _, err := runHarnessCLI(t, testkit.Env(t), unprobedHarnesses(t), "harness", "status", "--configured", "--harness", "claude")
	require.ErrorContains(t, err, "[configured harness]")
}

func TestAgentHarnessConfiguredStatusReturnsConfigError(t *testing.T) {
	env := testkit.Env(t)
	env.CLIConfigFile = filepath.Join(env.WorkingDir, "config.yml")
	require.NoError(t, os.WriteFile(env.CLIConfigFile, []byte("agent: ["), 0o600))
	_, _, err := runHarnessCLI(t, env, unprobedHarnesses(t), "harness", "status", "--configured")
	require.IsType(t, silentExitError{}, err)
	require.Equal(t, 1, err.(silentExitError).ExitCode())
}

func TestAgentHarnessStatusOutput(t *testing.T) {
	codexFake := &harnesstest.Harness{StatusResult: harness.Status{
		Kind: harness.KindCodex, Installed: true, LoggedIn: true, Version: "0.154.0",
	}}
	claudeFake := &harnesstest.Harness{StatusResult: harness.Status{
		Kind: harness.KindClaude, Installed: false, LoginHint: "claude login",
	}}
	registry := func() map[harness.Kind]harness.Harness {
		return map[harness.Kind]harness.Harness{harness.KindCodex: codexFake, harness.KindClaude: claudeFake}
	}

	t.Run("selected harness", func(t *testing.T) {
		var stdout bytes.Buffer
		cmd := newAgentHarnessStatusCmd(registry)
		cmd.SetOut(&stdout)
		cmd.SetArgs([]string{"--harness", "codex"})
		require.NoError(t, cmd.Execute())
		require.JSONEq(t, `{"kind":"codex","installed":true,"version":"0.154.0","loggedIn":true,"capabilities":{"supportsAllowedTools":false,"supportsAllowForSession":false,"permissionModes":null}}`, stdout.String())
	})

	t.Run("all harnesses are sorted", func(t *testing.T) {
		var stdout bytes.Buffer
		cmd := newAgentHarnessStatusCmd(registry)
		cmd.SetOut(&stdout)
		require.NoError(t, cmd.Execute())
		var statuses []harness.Status
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &statuses))
		require.Equal(t, []harness.Kind{harness.KindClaude, harness.KindCodex}, []harness.Kind{statuses[0].Kind, statuses[1].Kind})
	})
}

func TestAgentHarnessStatusReturnsErrorExit(t *testing.T) {
	cmd := newAgentHarnessStatusCmd(func() map[harness.Kind]harness.Harness { return nil })
	cmd.SetArgs([]string{"--harness", "codex"})
	err := cmd.Execute()
	require.IsType(t, silentExitError{}, err)
	require.Equal(t, 1, err.(silentExitError).ExitCode())
}

func TestAgentHarnessSmokeOutputAndOptions(t *testing.T) {
	session := harnesstest.NewSession()
	session.Emit(harness.Event{Kind: harness.EventTurnStarted, TurnID: "turn_1"})
	session.Emit(harness.Event{Kind: harness.EventApprovalRequested, RequestID: "approve_1"})
	session.Emit(harness.Event{Kind: harness.EventAssistantTextDelta, Text: "pong"})
	session.Emit(harness.Event{Kind: harness.EventTokenUsage, Usage: harness.TokenUsage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}})
	session.Emit(harness.Event{Kind: harness.EventTurnCompleted, TurnID: "turn_1", Status: "completed"})
	fake := &harnesstest.Harness{
		StatusResult:   harness.Status{Kind: harness.KindCodex, Installed: true, LoggedIn: true, Version: "0.154.0"},
		GenerateResult: harness.GenerateResult{Output: json.RawMessage(`{"answer":"pong"}`)},
		Session:        session,
	}
	registry := func() map[harness.Kind]harness.Harness {
		return map[harness.Kind]harness.Harness{harness.KindCodex: fake}
	}

	var stdout bytes.Buffer
	cmd := newAgentHarnessSmokeCmd(registry)
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--harness", "codex", "--model", "gpt-test", "--effort", "high", "--cwd", "/repo", "--prompt", "ping"})
	require.NoError(t, cmd.Execute())

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	require.Len(t, lines, 6)
	for _, line := range lines {
		require.True(t, json.Valid([]byte(line)), line)
	}
	var summary harnesscheck.Summary
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &summary))
	require.Equal(t, "pong", summary.Turn.Text)
	require.Equal(t, 5, summary.Turn.Events)
	require.Equal(t, int64(3), summary.Turn.Usage.TotalTokens)
	require.JSONEq(t, `{"answer":"pong"}`, string(summary.Generate))

	require.Equal(t, "ping", session.Turns[0].Prompt)
	require.Equal(t, harness.DecisionDeny, session.Responses[0].Decision)
	require.Equal(t, harness.PermissionApprovalRequired, fake.StartedWith[0].PermissionMode)
	require.Equal(t, "/repo", fake.StartedWith[0].Cwd)
	require.Equal(t, "gpt-test", fake.GeneratedWith[0].Model)
	require.JSONEq(t, `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`, string(fake.GeneratedWith[0].Schema))
}

func TestAgentHarnessSmokeReturnsPhaseFailureAsErrorExit(t *testing.T) {
	fake := &harnesstest.Harness{
		StatusResult:  harness.Status{Kind: harness.KindCodex, Installed: true, LoggedIn: true},
		GenerateError: harness.Phase(harness.ErrSchemaMismatch, errors.New("bad output")),
	}
	cmd := newAgentHarnessSmokeCmd(func() map[harness.Kind]harness.Harness {
		return map[harness.Kind]harness.Harness{harness.KindCodex: fake}
	})
	cmd.SetArgs([]string{"--harness", "codex"})
	err := cmd.Execute()
	require.IsType(t, silentExitError{}, err)
	require.Equal(t, 1, err.(silentExitError).ExitCode())
}

func TestAgentHarnessSmokeSkipsUnsupportedGeneration(t *testing.T) {
	session := harnesstest.NewSession()
	session.Emit(harness.Event{Kind: harness.EventAssistantTextDelta, Text: "pong"})
	fake := &harnesstest.Harness{
		StatusResult:  harness.Status{Kind: harness.KindCodex, Installed: true, LoggedIn: true},
		GenerateError: harness.Phase(harness.ErrUnsupported, errors.New("no tool-free mode")),
		Session:       session,
	}
	var stdout bytes.Buffer
	cmd := newAgentHarnessSmokeCmd(func() map[harness.Kind]harness.Harness {
		return map[harness.Kind]harness.Harness{harness.KindCodex: fake}
	})
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--harness", "codex"})
	require.NoError(t, cmd.Execute())
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var summary harnesscheck.Summary
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &summary))
	require.Equal(t, "harness unsupported: no tool-free mode", summary.GenerateSkipped)
	require.Equal(t, "pong", summary.Turn.Text)
}
