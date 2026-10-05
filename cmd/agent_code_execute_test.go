package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/stretchr/testify/require"
)

func TestAgentCodeExecuteAcceptsInlineAndStdinPrograms(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for JavaScript execution")
	}
	vault := setupAgentTestVault(t, map[string]string{"README.md": "# Fixture\n"})
	for _, stdin := range []bool{false, true} {
		t.Run(map[bool]string{true: "stdin", false: "inline"}[stdin], func(t *testing.T) {
			code := `console.log("diagnostic"); return {answer: await Promise.resolve(42), close: typeof rzm.close};`
			args := []string{"agent", "code", "execute", "--vault", vault.name}
			if stdin {
				original := rootCmd.InOrStdin()
				rootCmd.SetIn(strings.NewReader(code))
				t.Cleanup(func() { rootCmd.SetIn(original) })
			} else {
				args = append(args, "--code", code)
			}
			stdout, stderr, err := runRootCLI(t, context.Background(), args)
			require.NoError(t, err, stderr)
			var result agentcode.ExecuteResult
			require.NoError(t, json.Unmarshal([]byte(stdout), &result), stdout)
			require.True(t, result.OK)
			require.JSONEq(t, `{"answer":42,"close":"undefined"}`, string(result.Result))
			require.NotEmpty(t, result.Diagnostics)
		})
	}
	stdout, _, err := runRootCLI(t, context.Background(), []string{"agent", "code", "execute", "--vault", vault.name, "--code", `throw new Error("script failed");`})
	require.Error(t, err)
	var result agentcode.ExecuteResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	require.False(t, result.OK)
	require.NotNil(t, result.Error)
}

func TestAgentCodeInputIsExplicitAndBounded(t *testing.T) {
	for _, test := range []struct {
		name, code, input, wantError string
		explicit                     bool
	}{
		{name: "empty", wantError: "provide JavaScript"},
		{name: "explicit empty ignores stdin", explicit: true, input: "return 42", wantError: "provide JavaScript"},
		{name: "stdin oversized", input: strings.Repeat(" ", agentcode.MaxExecuteCodeBytes+1), wantError: "input limit"},
		{name: "inline overrides stdin", explicit: true, code: "return 42", input: "ignored"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := newAgentCodeExecuteCmd()
			command.SetIn(strings.NewReader(test.input))
			if test.explicit {
				require.NoError(t, command.Flags().Set("code", test.code))
			}
			code, err := readAgentCodeInput(command, test.code)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.code, code)
			}
		})
	}
}

func TestAgentCodeExecutionIsDiscoverable(t *testing.T) {
	selected, err := selectAgentSurface("code execute")
	require.NoError(t, err)
	payload, err := json.Marshal(selected)
	require.NoError(t, err)
	for _, flag := range []string{"code", "timeout", "read-write", "session-id"} {
		require.Contains(t, string(payload), `"name":"`+flag+`"`)
	}
}

func TestAgentCodeScriptInputBoundsJSONBeforeTrimming(t *testing.T) {
	for _, test := range []struct {
		name, content string
		wantError     bool
	}{
		{name: "at limit", content: `"` + strings.Repeat("a", agentcode.MaxExecuteCodeBytes-2) + `"`},
		{name: "oversized JSON", content: `"` + strings.Repeat("a", agentcode.MaxExecuteCodeBytes-1) + `"`, wantError: true},
		{name: "oversized whitespace", content: strings.Repeat(" ", agentcode.MaxExecuteCodeBytes) + "null", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.json")
			require.NoError(t, os.WriteFile(path, []byte(test.content), 0600))
			for _, flag := range []string{"input", "input-file"} {
				t.Run(flag, func(t *testing.T) {
					command := newAgentCodeExecuteCmd()
					value := test.content
					if flag == "input-file" {
						value = path
					}
					require.NoError(t, command.Flags().Set(flag, value))
					input, err := readAgentCodeScriptInput(command, test.content, path)
					if test.wantError {
						require.ErrorContains(t, err, "byte limit")
						require.Empty(t, input)
					} else {
						require.NoError(t, err)
						require.Equal(t, test.content, string(input))
					}
				})
			}
		})
	}
}
