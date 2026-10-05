package cmd

import (
	"context"
	"errors"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	appmcpserve "github.com/atomicobject/rhizome/pkg/app/cli/mcpserve"
	"github.com/atomicobject/rhizome/pkg/app/mcpserve"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/spf13/cobra"
)

var mcpCmd = newMCPCommand()

func newMCPCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "mcp",
		Short: "Serve Rhizome tools over MCP",
	}
	command.AddCommand(newMCPServeCommand())
	return command
}

func newMCPServeCommand() *cobra.Command {
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
			// Fire-and-forget: MCP reads the shared index in-process and only
			// wants the vault to have a runtime keeping it fresh.
			startVaultRuntimeInBackground(command.Context(), vaultDefOrDefaultContext)
			dispatch, err := appmcpserve.NewInvoker(command.Context(), readWrite, appmcpserve.Dependencies{
				VaultDefinition: vaultDefOrDefaultContext,
				RuntimeFree: func() (agentapi.Config, error) {
					return buildRuntimeFreeAgentConfig(0)
				},
				Prepare: func(ctx context.Context, name string, operationID oneshotruntime.OperationID, input map[string]any) (agentapi.Config, func(), error) {
					cfg, runtime, err := prepareAgentJSONTool(ctx, 0, name, operationID, input)
					if err != nil || runtime == nil {
						return cfg, nil, err
					}
					return cfg, func() { _ = runtime.Close() }, nil
				},
				PrepareBound: func(ctx context.Context, name string, operationID oneshotruntime.OperationID, _ map[string]any) (agentapi.Config, func(), error) {
					cfg, runtime, err := buildAgentConfigWithOperationRequirements(ctx, 0, name, operationID, bootstrap.RequireRuntimeCapabilities())
					if err != nil || runtime == nil {
						return cfg, nil, err
					}
					return cfg, func() { _ = runtime.Close() }, nil
				},
			})
			if err != nil {
				return err
			}
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
	command.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name (uses default if unset)")
	_ = command.MarkFlagRequired("tools")
	return command
}
