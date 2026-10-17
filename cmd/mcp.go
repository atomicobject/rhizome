package cmd

import (
	"context"
	"errors"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/mcpserve"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/spf13/cobra"
)

func (commandOptions *vaultCommandOptions) makeMcpCmd() *cobra.Command {
	return commandOptions.newMCPCommand()
}

func (commandOptions *vaultCommandOptions) newMCPCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "mcp",
		Short: "Serve Rhizome tools over MCP",
	}
	command.AddCommand(commandOptions.newMCPServeCommand())
	return command
}

func (commandOptions *vaultCommandOptions) newMCPServeCommand() *cobra.Command {
	var allowlist []string
	var readWrite bool
	command := &cobra.Command{
		Use:   "serve",
		Short: "Serve selected tools in one MCP stdio process",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := mcpserve.ValidateAllowlist(allowlist); err != nil {
				return err
			}
			cwd, err := commandEnvFromContext(command.Context()).Getwd()
			if err != nil {
				return err
			}
			dispatch, err := mcpserve.NewInvoker(command.Context(), readWrite, mcpserve.Dependencies{
				WorkingDirectory: cwd,
				VaultDefinition:  commandOptions.vaultDefOrDefaultContext,
				RuntimeFree: func() (agentapi.Config, error) {
					return commandOptions.buildRuntimeFreeAgentConfig(command.Context(), 0)
				},
				Prepare: func(ctx context.Context, name string, operationID oneshotruntime.OperationID, input map[string]any) (agentapi.Config, func(), error) {
					cfg, runtime, err := commandOptions.prepareAgentJSONTool(ctx, 0, name, operationID, input)
					if err != nil || runtime == nil {
						return cfg, nil, err
					}
					return cfg, func() { _ = runtime.Close() }, nil
				},
				PrepareBound: func(ctx context.Context, name string, operationID oneshotruntime.OperationID, _ map[string]any) (agentapi.Config, func(), error) {
					cfg, runtime, err := commandOptions.buildAgentConfigWithOperationRequirements(ctx, 0, name, operationID, bootstrap.RequireRuntimeCapabilities())
					if err != nil || runtime == nil {
						return cfg, nil, err
					}
					return cfg, func() { _ = runtime.Close() }, nil
				},
			})
			if err != nil {
				return err
			}
			// The connection is admitted before starting its background runtime.
			startVaultRuntimeInBackground(command.Context(), commandOptions.vaultDefOrDefaultContext)
			observedDispatch := func(ctx context.Context, name string, input map[string]any) (payload []byte, callErr error) {
				ctx, finish := observeAgentRequest(ctx, name, "mcp_stdio")
				defer func() {
					if panicked := recover(); panicked != nil {
						finish(false, errors.New("diagnostic boundary panicked"))
						panic(panicked)
					}
					finish(callErr == nil, callErr)
				}()
				return dispatch(ctx, name, input)
			}
			return mcpserve.Serve(command.Context(), command.InOrStdin(), command.OutOrStdout(), allowlist, observedDispatch)
		},
	}
	command.Flags().StringSliceVar(&allowlist, "tools", nil, "comma-separated catalog tool names to expose (required)")
	command.Flags().BoolVar(&readWrite, "read-write", false, "allow supported explicit write/apply operations for this connection")
	command.Flags().StringVarP(&commandOptions.vaultName, "vault", "v", "", "vault name (uses default if unset)")
	_ = command.MarkFlagRequired("tools")
	return command
}
