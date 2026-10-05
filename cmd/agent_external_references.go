package cmd

import "github.com/spf13/cobra"

func newAgentExternalReferencesCmd() *cobra.Command {
	var handle, ecosystem, module, symbolPrefix string
	var limit int
	cmd := &cobra.Command{
		Use:   "external-references",
		Short: "Return bounded local uses of an external code target as JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			maybeSetString(payload, "sessionId", agentSessionID)
			maybeSetString(payload, "handle", handle)
			maybeSetString(payload, "ecosystem", ecosystem)
			maybeSetString(payload, "module", module)
			maybeSetString(payload, "symbolPrefix", symbolPrefix)
			maybeSetInt(payload, "limit", limit)
			return runAgentJSONTool(cmd, 0, "external_references", payload)
		},
	}
	cmd.Flags().StringVar(&handle, "handle", "", "exact canonical external target handle")
	cmd.Flags().StringVar(&ecosystem, "ecosystem", "", "exact ecosystem for structured lookup")
	cmd.Flags().StringVar(&module, "module", "", "exact public module for structured lookup")
	cmd.Flags().StringVar(&symbolPrefix, "symbol-prefix", "", "deterministic canonical symbol-path prefix")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum results per relation group")
	return cmd
}
