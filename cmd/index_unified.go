package cmd

// Docs:
// - [Indexing pipeline (Hub)](docs/hubs/Indexing pipeline (Hub).md)
// - [Indexing pipeline - rzm index orchestration](docs/reference/analysis/Indexing pipeline - rzm index orchestration.md)
// - [Code Index - Unified SQLite DB](docs/reference/analysis/Code Index - Unified SQLite DB.md)

import (
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

// runUnifiedIndex performs code ingest, code embeddings, and note embeddings in one pass with a single progress bar.
func runUnifiedIndex(cmd *cobra.Command, noteMetadata notemeta.Indexer, vaultPath string, vaultDef obsidian.VaultDefinition, bar *progressBar) error {
	if bar == nil {
		bar = newCLIProgressBar(cmd.ErrOrStderr())
		defer bar.Close()
	}

	return indexing.RunUnifiedCore(cmd.Context(), indexing.UnifiedOptions{
		VaultPath:    vaultPath,
		VaultDef:     vaultDef,
		NoteMetadata: noteMetadata,
		ProgressBar:  bar,
		Verbose:      indexTimings,
		Vacuum:       indexVacuum,
		TxLockMode:   sqliteutil.TxLockImmediate,
		APIKey:       "",
	})
}
