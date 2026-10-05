package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type validationProductRunnerSpy struct {
	requests []validationProductRequest
	result   actions.ValidationResult
	err      error
}

func (s *validationProductRunnerSpy) Run(_ context.Context, request validationProductRequest) (actions.ValidationResult, error) {
	s.requests = append(s.requests, request)
	return s.result, s.err
}

func TestValidateProductCommandPropagatesSelectorAndReadOnlyFlags(t *testing.T) {
	spy := &validationProductRunnerSpy{result: cleanValidationProductResult("all")}
	cmd := newValidateProductCmdWithRunner(spy)
	stdout, stderr := executeValidationProductCommand(t, cmd,
		"all", "--vault", "docs", "--max-issues", "31", "--skip-anchors", "--skip-embeds",
		"--include-images", "--scope-note", "a.md", "--scope-target", "b.md", "--scope-ref", "node:1",
	)

	require.Empty(t, stderr)
	require.Contains(t, stdout, "Validation selector: all")
	require.Len(t, spy.requests, 1)
	require.Equal(t, validationProductRequest{
		Selectors:     []string{"all"},
		Surface:       validate.SurfaceLocal,
		VaultName:     "docs",
		ApplyCommand:  "rzm validate fix all --apply --vault docs --skip-anchors --skip-embeds --include-images --scope-note a.md --scope-target b.md --scope-ref node:1",
		MaxIssues:     31,
		SkipAnchors:   true,
		SkipEmbeds:    true,
		IncludeImages: true,
		ScopeNote:     "a.md",
		ScopeTarget:   "b.md",
		ScopeRef:      "node:1",
	}, spy.requests[0])
	require.Nil(t, cmd.Flags().Lookup("check"))
	require.Nil(t, cmd.Flags().Lookup("fix"))
}

func TestAgentValidateProductCarriesExactAgentApplyCommand(t *testing.T) {
	spy := &validationProductRunnerSpy{result: cleanValidationProductResult("broken-links")}
	cmd := newAgentValidateProductCmdWithRunner(spy)
	_, stderr := executeValidationProductCommand(t, cmd,
		"broken-links", "--scope-note", "Notes/A B.md", "--skip-anchors",
	)

	require.Empty(t, stderr)
	require.Len(t, spy.requests, 1)
	require.Equal(t, "rzm agent validate fix broken-links --apply --skip-anchors --scope-note 'Notes/A B.md'", spy.requests[0].ApplyCommand)
}

func TestAgentValidateProductCarriesInheritedVaultIntoRequestAndApplyCommand(t *testing.T) {
	originalVault := vaultName
	vaultName = ""
	t.Cleanup(func() { vaultName = originalVault })

	spy := &validationProductRunnerSpy{result: cleanValidationProductResult("broken-links")}
	root := &cobra.Command{Use: "rzm", SilenceUsage: true, SilenceErrors: true}
	agent := &cobra.Command{Use: "agent", SilenceUsage: true, SilenceErrors: true}
	agent.PersistentFlags().StringVarP(&vaultName, "vault", "v", "", "vault")
	agent.AddCommand(newAgentValidateProductCmdWithRunner(spy))
	root.AddCommand(agent)
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"agent", "--vault", "non default", "validate", "broken-links", "--scope-note", "Notes/A.md"})

	require.NoError(t, root.Execute())
	require.Len(t, spy.requests, 1)
	require.Equal(t, "non default", spy.requests[0].VaultName)
	require.Equal(t, "rzm agent validate fix broken-links --apply --vault 'non default' --scope-note Notes/A.md", spy.requests[0].ApplyCommand)
}

func TestValidateProductCommandAcceptsAtMostOneSelector(t *testing.T) {
	spy := &validationProductRunnerSpy{}
	cmd := newValidateProductCmdWithRunner(spy)
	cmd.SetArgs([]string{"default", "ontology"})
	err := cmd.Execute()

	require.Error(t, err)
	require.Empty(t, spy.requests)
}

func TestValidateProductListNeverCallsRunner(t *testing.T) {
	spy := &validationProductRunnerSpy{err: errors.New("runner must not be called")}
	cmd := newValidateProductCmdWithRunner(spy)
	stdout, stderr := executeValidationProductCommand(t, cmd, "list")

	require.Empty(t, stderr)
	require.Contains(t, stdout, "Validation checks")
	require.Contains(t, stdout, "broken-links")
	require.Empty(t, spy.requests)
}

func TestValidateProductFixPropagatesPlanAndApplyOptions(t *testing.T) {
	spy := &validationProductRunnerSpy{result: cleanValidationProductResult("identifiers")}
	cmd := newValidateProductCmdWithRunner(spy)
	_, stderr := executeValidationProductCommand(t, cmd,
		"fix", "identifiers", "--apply", "--allow-historical", "--max-issues", "12",
	)

	require.Empty(t, stderr)
	require.Len(t, spy.requests, 1)
	require.Equal(t, []string{"identifiers"}, spy.requests[0].Selectors)
	require.True(t, spy.requests[0].Repair)
	require.True(t, spy.requests[0].Apply)
	require.True(t, spy.requests[0].AllowHistorical)
	require.Equal(t, 12, spy.requests[0].MaxIssues)
}

func TestValidateProductFixCarriesReviewedApplySelection(t *testing.T) {
	planFile := filepath.Join(t.TempDir(), "approved.txt")
	require.NoError(t, os.WriteFile(planFile, []byte("# reviewed\n\nrepair:b\n  issue:v1:c  \n"), 0o644))
	for _, agent := range []bool{false, true} {
		spy := &validationProductRunnerSpy{result: cleanValidationProductResult("broken-links")}
		cmd := newValidateProductCmdWithRunner(spy)
		if agent {
			cmd = newAgentValidateProductCmdWithRunner(spy)
		}
		_, stderr := executeValidationProductCommand(t, cmd,
			"fix", "broken-links", "--apply", "--action", "repair:a", "--action", "issue:v1:x", "--from-plan", planFile,
		)

		require.Empty(t, stderr)
		require.Len(t, spy.requests, 1)
		require.Equal(t, []string{"repair:a", "issue:v1:x", "repair:b", "issue:v1:c"}, spy.requests[0].ApplySelection)
		require.NotContains(t, spy.requests[0].ApplyCommand, "--action")
	}
}

func TestValidateProductFixSelectionFlagsRequireApplyAndValidFile(t *testing.T) {
	dir := t.TempDir()
	emptyPlan := filepath.Join(dir, "empty.txt")
	require.NoError(t, os.WriteFile(emptyPlan, []byte("# nothing approved\n"), 0o644))
	badJSON := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(badJSON, []byte(`["a", 1]`), 0o644))
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"action without apply", []string{"fix", "--action", "repair:a"}, "require --apply"},
		{"from-plan without apply", []string{"fix", "--from-plan", emptyPlan}, "require --apply"},
		{"missing file", []string{"fix", "--apply", "--from-plan", filepath.Join(dir, "missing.txt")}, "read --from-plan"},
		{"empty file", []string{"fix", "--apply", "--from-plan", emptyPlan}, "lists no action IDs"},
		{"malformed JSON", []string{"fix", "--apply", "--from-plan", badJSON}, "parse selection JSON array"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &validationProductRunnerSpy{result: cleanValidationProductResult("default")}
			cmd := newValidateProductCmdWithRunner(spy)
			cmd.SetOut(&bytes.Buffer{})
			stderr := &bytes.Buffer{}
			cmd.SetErr(stderr)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()

			var coded interface{ ExitCode() int }
			require.ErrorAs(t, err, &coded)
			require.Equal(t, actions.ValidationExitFailure, coded.ExitCode())
			require.Contains(t, stderr.String(), tt.want)
			require.Empty(t, spy.requests)
		})
	}
}

func TestValidateProductHumanApplyCarriesReusableConfirmation(t *testing.T) {
	spy := &validationProductRunnerSpy{result: cleanValidationProductResult("identifiers")}
	cmd := newValidateProductCmdWithRunner(spy)
	stdout := &bytes.Buffer{}
	cmd.SetIn(strings.NewReader("yes\ny\n"))
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"fix", "identifiers", "--apply"})
	require.NoError(t, cmd.Execute())

	require.Len(t, spy.requests, 1)
	request := spy.requests[0]
	require.False(t, request.NonInteractive)
	require.NotNil(t, request.Confirm)
	first, err := request.Confirm("Apply first repair?")
	require.NoError(t, err)
	second, err := request.Confirm("Apply second repair?")
	require.NoError(t, err)
	require.True(t, first)
	require.True(t, second)
	require.Contains(t, stdout.String(), "Apply first repair? [y/N]:")
	require.Contains(t, stdout.String(), "Apply second repair? [y/N]:")
}

func TestValidateProductPlanDoesNotCarryConfirmation(t *testing.T) {
	spy := &validationProductRunnerSpy{result: cleanValidationProductResult("identifiers")}
	cmd := newValidateProductCmdWithRunner(spy)
	_, stderr := executeValidationProductCommand(t, cmd, "fix", "identifiers")

	require.Empty(t, stderr)
	require.Len(t, spy.requests, 1)
	require.False(t, spy.requests[0].Apply)
	require.Nil(t, spy.requests[0].Confirm)
}

func TestValidationProductCommandsReturnFrozenExitCodes(t *testing.T) {
	tests := []struct {
		name   string
		result actions.ValidationResult
		want   int
	}{
		{name: "clean", result: cleanValidationProductResult("default"), want: actions.ValidationExitClean},
		{name: "findings", result: findingsValidationProductResult(), want: actions.ValidationExitFindings},
		{name: "blocked", result: blockedValidationProductResult(), want: actions.ValidationExitFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newValidateProductCmdWithRunner(&validationProductRunnerSpy{result: tt.result})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(nil)
			err := cmd.Execute()
			if tt.want == 0 {
				require.NoError(t, err)
				return
			}
			var coded interface{ ExitCode() int }
			require.ErrorAs(t, err, &coded)
			require.Equal(t, tt.want, coded.ExitCode())
		})
	}
}

func TestValidationProductExecutionErrorsRenderAndExitTwo(t *testing.T) {
	cmd := newValidateProductCmdWithRunner(&validationProductRunnerSpy{err: errors.New("bad validation config")})
	cmd.SetOut(&bytes.Buffer{})
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)
	cmd.SetArgs(nil)
	err := cmd.Execute()

	var coded interface{ ExitCode() int }
	require.ErrorAs(t, err, &coded)
	require.Equal(t, actions.ValidationExitFailure, coded.ExitCode())
	require.Contains(t, stderr.String(), "bad validation config")
}

func TestAgentValidationProductErrorsUseStableJSONEnvelopeOnStderr(t *testing.T) {
	cmd := newAgentValidateProductCmdWithRunner(&validationProductRunnerSpy{err: errors.New("bad validation config")})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(nil)
	err := cmd.Execute()

	var coded interface{ ExitCode() int }
	require.ErrorAs(t, err, &coded)
	require.Equal(t, actions.ValidationExitFailure, coded.ExitCode())
	require.Empty(t, stdout.String())
	require.JSONEq(t, `{"ok":false,"error":"bad validation config","exitCode":2}`, stderr.String())
}

func TestAgentValidateProductMirrorsMenuAsJSONWithoutPromptState(t *testing.T) {
	spy := &validationProductRunnerSpy{result: cleanValidationProductResult("audit")}
	cmd := newAgentValidateProductCmdWithRunner(spy)
	stdout, stderr := executeValidationProductCommand(t, cmd, "fix", "audit", "--apply", "--allow-historical")

	require.Empty(t, stderr)
	require.Len(t, spy.requests, 1)
	require.Equal(t, validate.SurfaceAgent, spy.requests[0].Surface)
	require.True(t, spy.requests[0].Repair)
	require.True(t, spy.requests[0].Apply)
	require.True(t, spy.requests[0].AllowHistorical)
	require.True(t, spy.requests[0].NonInteractive)
	require.Nil(t, spy.requests[0].Confirm)
	require.NotContains(t, stdout, "Continue?")
	var payload actions.ValidationResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	require.Equal(t, "audit", payload.Selector)
	require.Nil(t, cmd.Flags().Lookup("check"))
	require.Nil(t, cmd.Flags().Lookup("fix"))
}

func TestAgentValidateProductListIsJSONAndNeverCallsRunner(t *testing.T) {
	spy := &validationProductRunnerSpy{err: errors.New("runner must not be called")}
	cmd := newAgentValidateProductCmdWithRunner(spy)
	stdout, stderr := executeValidationProductCommand(t, cmd, "list")

	require.Empty(t, stderr)
	require.JSONEq(t, string(mustValidationCatalogJSON(t)), stdout)
	require.Empty(t, spy.requests)
}

func TestCIProductUsesExclusiveJSONAndGitHubRenderers(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		spy := &validationProductRunnerSpy{result: findingsValidationProductResult()}
		cmd := newCIProductCmdWithRunner(spy)
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetArgs([]string{"all", "--format", "json"})
		err := cmd.Execute()

		var coded interface{ ExitCode() int }
		require.ErrorAs(t, err, &coded)
		require.Equal(t, actions.ValidationExitFindings, coded.ExitCode())
		require.Empty(t, stderr.String())
		require.NotContains(t, stdout.String(), "::error")
		var payload actions.ValidationResult
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
		require.Equal(t, validate.SurfaceCI, spy.requests[0].Surface)
		require.Equal(t, []string{"all"}, spy.requests[0].Selectors)
	})

	t.Run("github", func(t *testing.T) {
		spy := &validationProductRunnerSpy{result: findingsValidationProductResult()}
		cmd := newCIProductCmdWithRunner(spy)
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetArgs([]string{"--format", "github"})
		err := cmd.Execute()

		require.Error(t, err)
		require.Empty(t, stderr.String())
		require.Contains(t, stdout.String(), "::error")
		require.Contains(t, stdout.String(), "Rhizome validation:")
		require.NotContains(t, stdout.String(), `"effectiveChecks"`)
	})
}

func TestCIProductRejectsMixedOrUnknownFormatBeforeRunning(t *testing.T) {
	for _, args := range [][]string{
		{"--format", "github,json"},
		{"--format", "github", "--format", "json"},
	} {
		spy := &validationProductRunnerSpy{}
		cmd := newCIProductCmdWithRunner(spy)
		cmd.SetOut(&bytes.Buffer{})
		stderr := &bytes.Buffer{}
		cmd.SetErr(stderr)
		cmd.SetArgs(args)
		err := cmd.Execute()

		var coded interface{ ExitCode() int }
		require.ErrorAs(t, err, &coded)
		require.Equal(t, actions.ValidationExitFailure, coded.ExitCode())
		require.Empty(t, spy.requests)
		require.Contains(t, stderr.String(), "expected exactly one")
	}
}

func TestCIProductJSONErrorsKeepStdoutMachineValid(t *testing.T) {
	cmd := newCIProductCmdWithRunner(&validationProductRunnerSpy{err: errors.New("projection failed")})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--format", "json"})
	err := cmd.Execute()

	var coded interface{ ExitCode() int }
	require.ErrorAs(t, err, &coded)
	require.Equal(t, actions.ValidationExitFailure, coded.ExitCode())
	require.Empty(t, stderr.String())
	var payload map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
	require.Equal(t, false, payload["ok"])
	require.Equal(t, "projection failed", payload["error"])
}

func executeValidationProductCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, string) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute())
	return stdout.String(), stderr.String()
}

func cleanValidationProductResult(selector string) actions.ValidationResult {
	return actions.ValidationResult{
		Result:          validate.Result{OK: true},
		Selector:        selector,
		EffectiveChecks: []string{"broken-links"},
		Outcomes: []actions.ValidationCheckOutcome{{
			Check: "broken-links", Outcome: validate.CheckOutcomeCompleted,
		}},
	}
}

func findingsValidationProductResult() actions.ValidationResult {
	return actions.ValidationResult{
		Result: validate.Result{
			IssueCount: 1,
			Checks: []validate.CheckResult{{
				Name: "broken-links", IssueCount: 1,
				Issues: []validate.Issue{{Code: "broken_note_link", Path: "a.md", Line: 3, Message: "missing note"}},
			}},
		},
		Selector:        "default",
		EffectiveChecks: []string{"broken-links"},
		Outcomes: []actions.ValidationCheckOutcome{{
			Check: "broken-links", Outcome: validate.CheckOutcomeCompleted,
		}},
	}
}

func blockedValidationProductResult() actions.ValidationResult {
	return actions.ValidationResult{
		Result:          validate.Result{OK: false},
		Selector:        "code-anchors",
		EffectiveChecks: []string{"code-anchors"},
		Outcomes: []actions.ValidationCheckOutcome{{
			Check: "code-anchors", Outcome: validate.CheckOutcomeBlocked,
			PreparationCommand: "rzm index",
		}},
	}
}

func mustValidationCatalogJSON(t *testing.T) []byte {
	t.Helper()
	payload, err := actions.RenderValidationCatalogJSON(actions.BuildValidationCatalog())
	require.NoError(t, err)
	return payload
}
