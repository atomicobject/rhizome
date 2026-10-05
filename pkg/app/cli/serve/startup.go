package serve

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// RecoverInterruptedWrites establishes the synchronous write-stability barrier
// required before the live runtime starts readers and background workers.
func RecoverInterruptedWrites(ctx context.Context, vaultDef obsidian.VaultDefinition) error {
	runCtx := validate.RunContext{
		VaultDef: vaultDef, VaultPath: vaultDef.BasePath(),
	}
	if err := validate.StabilizePendingRepairJournalsContext(ctx, runCtx); err != nil {
		return fmt.Errorf("recover interrupted repair writes: %w", err)
	}
	if err := ontology.RecoverInterruptedEditsContext(ctx, vaultDef.BasePath()); err != nil {
		return fmt.Errorf("recover interrupted note edits: %w", err)
	}
	journals, err := validate.DetectPendingRepairJournals(runCtx)
	if err != nil {
		return fmt.Errorf("inspect pending repair recovery: %w", err)
	}
	if len(journals) > 0 {
		message := fmt.Sprintf("%d pending repair journal(s) need recovery review; startup will continue. Later edits to repaired notes are preserved. Validation and repair writes remain blocked. Run `rzm validate` for recovery guidance.", len(journals))
		if recorder := diagnostics.FromContext(ctx); recorder != nil {
			recorder.Event(ctx, slog.LevelWarn, "runtime", "repair.recovery_review_required", message, slog.Int("pending_journals", len(journals)))
		} else {
			fmt.Fprintf(os.Stderr, "Warning: %s\n", message)
		}
	}
	return nil
}
