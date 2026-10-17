package cmd

import (
	"context"

	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"github.com/spf13/cobra"
)

func (commandOptions *vaultCommandOptions) newAgentCurrentUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "current-user",
		Short: "Show, set, or validate the ignored per-vault current Person",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(commandOptions.newAgentCurrentUserShowCmd())
	cmd.AddCommand(commandOptions.newAgentCurrentUserSetCmd())
	cmd.AddCommand(commandOptions.newAgentCurrentUserValidateCmd())
	return cmd
}

func (commandOptions *vaultCommandOptions) newAgentCurrentUserShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show configured current-user identity",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := assertRuntimeFreeAgentOperation(cmd); err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			result, err := commandOptions.showCurrentUser(cmd.Context())
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			return writeAgentPayload(cmd, result)
		},
	}
}

func (commandOptions *vaultCommandOptions) newAgentCurrentUserSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <person-title-or-ref>",
		Short: "Set current-user identity to a Person title or ref",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := assertRuntimeFreeAgentOperation(cmd); err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			result, err := commandOptions.setCurrentUser(cmd.Context(), args[0])
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			return writeAgentPayload(cmd, result)
		},
	}
}

func (commandOptions *vaultCommandOptions) currentUserService() actions.CurrentUserService {
	return actions.CurrentUserService{Lookup: commandOptions.currentUserLookup}
}

func (commandOptions *vaultCommandOptions) showCurrentUser(ctx context.Context) (identity.Resolution, error) {
	vaultDef, err := commandOptions.vaultDefOrDefaultContext(ctx)
	if err != nil {
		return identity.Resolution{}, err
	}
	return commandOptions.currentUserService().Show(vaultDef.BasePath())
}

func (commandOptions *vaultCommandOptions) setCurrentUser(ctx context.Context, ref string) (identity.Resolution, error) {
	vaultDef, err := commandOptions.vaultDefOrDefaultContext(ctx)
	if err != nil {
		return identity.Resolution{}, err
	}
	return commandOptions.currentUserService().Set(vaultDef.BasePath(), ref)
}

func (commandOptions *vaultCommandOptions) newAgentCurrentUserValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate current-user identity resolves to Person",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := commandOptions.validateCurrentUser(cmd.Context())
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			return writeAgentPayload(cmd, result)
		},
	}
}

func (commandOptions *vaultCommandOptions) validateCurrentUser(ctx context.Context) (identity.Resolution, error) {
	vaultDef, err := commandOptions.vaultDefOrDefaultContext(ctx)
	if err != nil {
		return identity.Resolution{}, err
	}
	return commandOptions.currentUserService().Validate(ctx, vaultDef.BasePath())
}

func (commandOptions *vaultCommandOptions) currentUserLookup(ctx context.Context, ref string) (actions.CurrentUserLookup, error) {
	runtime, prepared, cleanup, err := commandOptions.prepareOntologyQuery(ctx, actions.CurrentUserLookupQuery, map[string]any{"ref": ref})
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return actions.CurrentUserLookup{}, err
	}
	result, err := runtime.run(ctx, prepared)
	if err != nil {
		return actions.CurrentUserLookup{}, err
	}
	return actions.CurrentUserLookupFromQueryResult(result)
}
