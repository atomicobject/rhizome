package cmd

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

func newBootstrapCommand(name string, run func(context.Context, bootstrap.CodexParams) error) *cobra.Command {
	var params bootstrap.CodexParams
	cmd := &cobra.Command{
		Use:   strings.ToLower(name) + " <task>",
		Short: "Launch " + name + " CLI with managed Rhizome guidance and vault-aware context",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			selectedVault := vaultName
			if selectedVault == "" {
				v := &obsidian.Vault{}
				name, err := v.DefaultName()
				if err != nil {
					return err
				}
				selectedVault = name
			}
			task := strings.TrimSpace(strings.Join(args, " "))
			if params.DebugAll {
				params.Debug = true
			}
			invocation := params
			invocation.Task = task
			invocation.VaultName = selectedVault
			return run(cmd.Context(), invocation)
		},
	}
	flags := cmd.Flags()
	flags.StringArrayVarP(&params.Queries, "query", "q", nil, "semantic query (repeatable)")
	flags.StringArrayVarP(&params.Files, "file", "f", nil, "file or directory seed (repeatable)")
	flags.StringVar(&params.Profile, "profile", "instant", "LLM profile: instant|thinking (fast is an alias)")
	flags.StringVar(&params.Model, "model", "", "model override (e.g., openai/gpt-5.2:high)")
	flags.BoolVar(&params.NoAuto, "no-auto", false, "disable LLM-driven retrieval planning")
	flags.BoolVar(&params.Debug, "debug", false, "print tool calls and response sizes without launching "+name)
	flags.BoolVar(&params.DebugAll, "debug-all", false, "print tool calls plus full responses without launching "+name)
	flags.IntVar(&params.BudgetChars, "budget-chars", 0, "max output size in characters (default from config)")
	flags.StringVarP(&vaultName, "vault", "v", "", "vault name (not required if default is set)")
	return cmd
}
