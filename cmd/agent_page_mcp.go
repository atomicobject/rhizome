package cmd

import (
	"os"

	"github.com/atomicobject/rhizome/pkg/app/agentchat/pagemcp"
	"github.com/spf13/cobra"
)

func newAgentPageMCPCmd() *cobra.Command {
	return &cobra.Command{Use: "page-mcp", Hidden: true, Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		return pagemcp.Serve(command.Context(), command.InOrStdin(), command.OutOrStdout(), os.Getenv("RHIZOME_PAGE_RELAY_URL"), os.Getenv("RHIZOME_PAGE_RELAY_TOKEN"))
	}}
}
