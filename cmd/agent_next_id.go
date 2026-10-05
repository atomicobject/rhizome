package cmd

import (
	"context"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	"github.com/spf13/cobra"
)

func allocateAgentNextID(ctx context.Context, typeName string, count int, paths []string) (*idalloc.Result, error) {
	vaultDef, err := vaultDefOrDefaultContext(ctx)
	if err != nil {
		return nil, err
	}
	// Preserve schema admission errors before starting the live runtime.
	if _, err := ontology.LoadSchema(vaultDef.BasePath()); err != nil {
		return nil, err
	}
	live, err := buildOntologyQueryLiveRuntime(ctx, oneshotruntime.OntologyQueryPlan(false, false))
	if err != nil {
		return nil, err
	}
	defer live.Close()
	return actions.AllocateNextID(ctx, live.VaultDef, live.NoteMetadataIndexer(), live.IntelStore(), idalloc.Request{Type: typeName, Count: count, Paths: paths})
}

func newAgentNextIDCmd() *cobra.Command {
	var typeName string
	var count int
	var paths []string
	cmd := &cobra.Command{
		Use:   "next-id",
		Short: "Allocate the next identifier for a typed note family",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := allocateAgentNextID(cmd.Context(), typeName, count, paths)
			if err != nil {
				writeNextIDError(err, result)
				return silentExitError{code: 1}
			}
			if err := writeAgentPayload(cmd, result); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&typeName, "type", "", "ontology type to allocate the next id for (required)")
	cmd.Flags().IntVar(&count, "count", 1, "number of sequential ids to allocate in one batch")
	cmd.Flags().StringArrayVar(&paths, "path", nil, "prospective vault-relative note path (repeatable for filename-derived ids)")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

// writeNextIDError emits a stable JSON error envelope so skills can branch on
// the typed code. Falls back to the generic agent error shape for unexpected
// runtime/IO failures.
func writeNextIDError(err error, result *idalloc.Result) {
	code := idalloc.ErrorCode(err)
	if code == "" || code == "internal_error" {
		writeAgentError(err)
		return
	}
	payload := map[string]any{
		"error": err.Error(),
		"code":  code,
	}
	if result != nil {
		payload["type"] = result.Type
		payload["strategy"] = result.Strategy
		if len(result.Paths) > 0 {
			payload["paths"] = result.Paths
		}
	}
	writeAgentJSONError(payload)
}
