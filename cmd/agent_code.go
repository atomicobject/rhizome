package cmd

import (
	"os"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/spf13/cobra"
)

func newAgentCodeCmd() *cobra.Command {
	command := &cobra.Command{Use: "code", Short: "Execute JavaScript with Rhizome operations or discover selected contracts"}
	surface := &cobra.Command{
		Use: "surface", Short: "List the bounded code-mode operations without loading schemas or runtime state", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := assertRuntimeFreeAgentOperation(cmd); err != nil {
				return agentCodeError(err)
			}
			return writeAgentPayload(cmd, agentcode.Surface())
		},
	}
	var describeOperations []string
	var describeSchema string
	describe := &cobra.Command{
		Use: "describe", Short: "Describe only explicitly selected code-mode operations", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := assertRuntimeFreeAgentOperation(cmd); err != nil {
				return agentCodeError(err)
			}
			result, err := agentcode.Describe(describeOperations)
			if err != nil {
				return agentCodeError(err)
			}
			result, err = result.SelectSchemas(describeSchema)
			if err != nil {
				return agentCodeError(err)
			}
			return writeAgentPayload(cmd, result)
		},
	}
	describe.Flags().StringArrayVar(&describeOperations, "operation", nil, "catalog operation name or JavaScript method to describe (repeatable; required)")
	describe.Flags().StringVar(&describeSchema, "schema", "both", "schemas to include: input, output, or both; shared call/outcome guidance is always included")
	var generateOperations []string
	var output string
	generate := &cobra.Command{
		Use: "generate", Short: "Write selected ESM and TypeScript client artifacts to an explicit task directory", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := assertRuntimeFreeAgentOperation(cmd); err != nil {
				return agentCodeError(err)
			}
			executable, err := os.Executable()
			if err != nil {
				return agentCodeError(err)
			}
			def, err := vaultDefOrDefaultContext(cmd.Context())
			if err != nil {
				return agentCodeError(err)
			}
			vaultPath, err := canonicalAgentCodeVaultPath(def.BasePath())
			if err != nil {
				return agentCodeError(err)
			}
			manifest, err := agentcode.Generate(output, generateOperations)
			if err != nil {
				return agentCodeError(err)
			}
			manifest.ExecutablePath = executable
			manifest.VaultPath = vaultPath
			return writeAgentPayload(cmd, manifest)
		},
	}
	generate.Flags().StringArrayVar(&generateOperations, "operation", nil, "catalog operation name or JavaScript method to include (repeatable; required)")
	generate.Flags().StringVar(&output, "output", "", "explicit writable task artifact directory (required)")
	command.AddCommand(surface, describe, newAgentCodeExecuteCmd(), generate, newAgentCodeServeCmd())
	return command
}

func canonicalAgentCodeVaultPath(root string) (string, error) {
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil {
		return "", err
	}
	return vaultPaths.Root(), nil
}

func agentCodeError(err error) error {
	writeAgentError(err)
	return silentExitError{code: 1}
}
