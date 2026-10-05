package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/cli/harnesscheck"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/stretchr/testify/require"
)

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
		GenerateResult: json.RawMessage(`{"answer":"pong"}`),
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
