package cmd

// Docs:
// - [Indexing pipeline (Hub)](docs/hubs/Indexing pipeline (Hub).md)
// - [Indexing pipeline - rzm index orchestration](docs/reference/analysis/Indexing pipeline - rzm index orchestration.md)
// - [Code Index - Unified SQLite DB](docs/reference/analysis/Code Index - Unified SQLite DB.md)

import (
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

// runUnifiedIndex performs code ingest, code embeddings, and note embeddings in one pass with a single progress bar.
func (commandOptions *indexInvocationOptions) runUnifiedIndex(cmd *cobra.Command, lease *indexlock.Lease, noteMetadata notemeta.Indexer, vaultPath string, vaultDef obsidian.VaultDefinition, bar *progressBar, resolved *repoconfig.Resolved) error {
	if bar == nil {
		bar = newCLIProgressBar(cmd.ErrOrStderr())
		defer bar.Close()
	}

	return indexing.RunUnifiedCore(cmd.Context(), lease, indexing.UnifiedOptions{
		Config:       resolved,
		VaultPath:    vaultPath,
		VaultDef:     vaultDef,
		NoteMetadata: noteMetadata,
		ProgressBar:  bar,
		Verbose:      commandOptions.indexTimings,
		Vacuum:       commandOptions.indexVacuum,
		TxLockMode:   sqliteutil.TxLockImmediate,
		APIKey:       "",
	})
}
