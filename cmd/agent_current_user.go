package cmd

import (
	"context"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"github.com/spf13/cobra"
)

func newAgentCurrentUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "current-user",
		Short: "Show, set, or validate the ignored per-vault current Person",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newAgentCurrentUserShowCmd())
	cmd.AddCommand(newAgentCurrentUserSetCmd())
	cmd.AddCommand(newAgentCurrentUserValidateCmd())
	return cmd
}

func newAgentCurrentUserShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show configured current-user identity",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := assertRuntimeFreeAgentOperation(cmd); err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			result, err := showCurrentUser(cmd.Context())
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			return writeAgentPayload(cmd, result)
		},
	}
}

func newAgentCurrentUserSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <person-title-or-ref>",
		Short: "Set current-user identity to a Person title or ref",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := assertRuntimeFreeAgentOperation(cmd); err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			result, err := setCurrentUser(cmd.Context(), args[0])
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			return writeAgentPayload(cmd, result)
		},
	}
}

func currentUserService() actions.CurrentUserService {
	return actions.CurrentUserService{Lookup: currentUserLookup}
}

func showCurrentUser(ctx context.Context) (identity.Resolution, error) {
	vaultDef, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return identity.Resolution{}, err
	}
	return currentUserService().Show(vaultDef.BasePath())
}

func setCurrentUser(ctx context.Context, ref string) (identity.Resolution, error) {
	vaultDef, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return identity.Resolution{}, err
	}
	return currentUserService().Set(vaultDef.BasePath(), ref)
}

func newAgentCurrentUserValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate current-user identity resolves to Person",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := validateCurrentUser(cmd.Context())
			if err != nil {
				writeAgentError(err)
				return silentExitError{code: 1}
			}
			return writeAgentPayload(cmd, result)
		},
	}
}

func validateCurrentUser(ctx context.Context) (identity.Resolution, error) {
	vaultDef, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return identity.Resolution{}, err
	}
	return currentUserService().Validate(ctx, vaultDef.BasePath())
}

func currentUserLookup(ctx context.Context, ref string) (actions.CurrentUserLookup, error) {
	runtime, prepared, cleanup, err := prepareOntologyQuery(ctx, actions.CurrentUserLookupQuery, map[string]any{"ref": ref})
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return actions.CurrentUserLookup{}, err
	}
	return actions.CurrentUserLookupFromQueryResult(query.ExecutePrepared(ctx, runtime.deps(), runtime.schema, runtime.execSchema, prepared))
}
