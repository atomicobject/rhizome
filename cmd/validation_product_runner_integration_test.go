package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateFixLinkHygieneApplyRunsProductionSessionAndReturnsClearedPostcheck(t *testing.T) {
	root := writeValidationProductIntegrationVault(t, map[string]string{
		"target.md": "# Target\n",
		"source.md": "bad extension: [[target.md]]\n",
	})
	runner := &capturingValidationProductRunner{delegate: newProductionValidationRunner()}
	cmd := newValidateProductCmdWithRunner(runner)
	cmd.SetContext(validationProductIntegrationContext(root))

	stdout, stderr, err := executeValidationProductIntegrationCommand(cmd, "fix", "link-hygiene", "--apply")
	require.NoError(t, err, stderr)
	require.Len(t, runner.results, 1)
	result := runner.results[0]
	require.NotNil(t, result.FixExecution)

	assert.True(t, result.OK)
	assert.Zero(t, result.IssueCount)
	require.Len(t, result.Checks, 1)
	assert.Equal(t, "link-hygiene", result.Checks[0].Name)
	assert.True(t, result.Checks[0].OK)
	assert.Zero(t, result.Checks[0].IssueCount)
	assert.NotEmpty(t, result.FixExecution.Applied)
	require.NotNil(t, result.FixExecution.Refresh)
	assert.Equal(t, []string{"links", "markdown_targets", "metadata"}, result.FixExecution.Refresh.Domains)
	assert.Equal(t, []string{"source.md"}, result.FixExecution.Refresh.Paths)
	assert.Equal(t, "bad extension: [[target]]\n", string(readValidationProductIntegrationFile(t, root, "source.md")))
	assertNoValidationProductRepairJournals(t, root)
	assert.Contains(t, stdout, "Repair apply: applied=1")
	assert.Contains(t, stdout, "Validation summary: 0 issues; 0 errors; exit=0")
}

func TestValidationProductReadOnlySurfacesLeaveNotesAndRepairJournalUntouched(t *testing.T) {
	originalVaultName := vaultName
	vaultName = ""
	t.Cleanup(func() { vaultName = originalVaultName })

	tests := []struct {
		name       string
		newCommand func(validationProductRunner) *cobra.Command
		args       []string
		wantOutput string
	}{
		{
			name:       "human fix plan",
			newCommand: newValidateProductCmdWithRunner,
			args:       []string{"fix", "link-hygiene"},
			wantOutput: "Repair plan:",
		},
		{
			name:       "agent fix plan",
			newCommand: newAgentValidateProductCmdWithRunner,
			args:       []string{"fix", "link-hygiene"},
			wantOutput: `"fixPlan":`,
		},
		{
			name:       "ci validation",
			newCommand: newCIProductCmdWithRunner,
			args:       []string{"link-hygiene", "--format", "json"},
			wantOutput: `"fixPlan":`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeValidationProductIntegrationVault(t, map[string]string{
				"target.md": "# Target\n",
				"source.md": "bad extension: [[target.md]]\n",
			})
			before := readValidationProductIntegrationFile(t, root, "source.md")
			runner := &capturingValidationProductRunner{delegate: newProductionValidationRunner()}
			cmd := test.newCommand(runner)
			cmd.SetContext(validationProductIntegrationContext(root))

			stdout, _, err := executeValidationProductIntegrationCommand(cmd, test.args...)
			assertValidationProductIntegrationExit(t, err, actions.ValidationExitFindings)
			require.Len(t, runner.results, 1)
			result := runner.results[0]

			require.NotNil(t, result.FixPlan)
			assert.NotEmpty(t, result.FixPlan.Actions)
			assert.Nil(t, result.FixExecution)
			assert.Equal(t, before, readValidationProductIntegrationFile(t, root, "source.md"))
			assertNoValidationProductRepairJournals(t, root)
			assert.Contains(t, stdout, test.wantOutput)
		})
	}
}

func TestValidateFixIdentifiersApplyPreservesPrivateRepairAuthority(t *testing.T) {
	root := writeValidationProductIntegrationVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Spec @node(paths: ["specs/*.md"], propertyCase: AS_DEFINED) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 4)
  aliases: [String!] @field
}
`,
		"specs/a.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n# A\n",
		"specs/b.md": "---\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n# B\n",
	})
	runner := &capturingValidationProductRunner{delegate: newProductionValidationRunner()}
	cmd := newValidateProductCmdWithRunner(runner)
	cmd.SetContext(validationProductIntegrationContext(root))
	cmd.SetIn(bytes.NewBufferString("yes\n"))

	stdout, stderr, err := executeValidationProductIntegrationCommand(cmd, "fix", "identifiers", "--apply")
	require.NoError(t, err, stderr)
	require.Len(t, runner.results, 1)
	result := runner.results[0]
	require.NotNil(t, result.FixExecution)
	require.NotNil(t, result.IdentifierReconciliation)
	require.NotNil(t, result.IdentifierReconciliation.Plan)

	assert.True(t, result.OK)
	assert.Zero(t, result.IssueCount)
	require.Len(t, result.Checks, 1)
	assert.Equal(t, validate.CheckIdentifiers, result.Checks[0].Name)
	assert.Zero(t, result.Checks[0].IssueCount)
	assert.Contains(t, string(readValidationProductIntegrationFile(t, root, "specs/a.md")), "id: SPEC-0001")
	assert.Contains(t, string(readValidationProductIntegrationFile(t, root, "specs/b.md")), "id: SPEC-0002")
	assert.Contains(t, string(readValidationProductIntegrationFile(t, root, "specs/b.md")), "aliases: [SPEC-0002]")
	assert.Empty(t, result.IdentifierReconciliation.Plan.Collisions, "returned prepared postcheck must be clean")
	assert.Positive(t, result.IdentifierReconciliation.Diagnostics.Timings.Apply)
	assert.Positive(t, result.IdentifierReconciliation.Diagnostics.Timings.PostValidation)
	assertNoValidationProductRepairJournals(t, root)
	assert.Contains(t, stdout, "Identifier reconciliation:")
	assert.Contains(t, stdout, "Validation summary: 0 issues; 0 errors; exit=0")
}

type capturingValidationProductRunner struct {
	delegate validationProductRunner
	results  []actions.ValidationResult
}

func (runner *capturingValidationProductRunner) Run(ctx context.Context, request validationProductRequest) (actions.ValidationResult, error) {
	result, err := runner.delegate.Run(ctx, request)
	runner.results = append(runner.results, result)
	return result, err
}

func writeValidationProductIntegrationVault(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{}))
	for path, content := range files {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(absolute), 0o755))
		require.NoError(t, os.WriteFile(absolute, []byte(content), 0o644))
	}
	return root
}

func validationProductIntegrationContext(root string) context.Context {
	return contextWithCommandEnv(context.Background(), commandEnv{
		Getwd: func() (string, error) { return root, nil },
	})
}

func executeValidationProductIntegrationCommand(cmd *cobra.Command, args ...string) (string, string, error) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func assertValidationProductIntegrationExit(t *testing.T, err error, want int) {
	t.Helper()
	var coded interface{ ExitCode() int }
	require.ErrorAs(t, err, &coded)
	assert.Equal(t, want, coded.ExitCode())
}

func readValidationProductIntegrationFile(t *testing.T, root, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	require.NoError(t, err)
	return data
}

func assertNoValidationProductRepairJournals(t *testing.T, root string) {
	t.Helper()
	evidence, err := validate.DetectPendingRepairJournals(validate.RunContext{
		VaultPath: root,
		VaultDef:  obsidian.VaultDefinition{Root: root},
	})
	require.NoError(t, err)
	assert.Empty(t, evidence)
}
