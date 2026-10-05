package cmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

func runIndexCommandWithRebuildGuidance(cmd *cobra.Command, noteMetadata notemeta.Indexer, vaultPath string, vaultDef obsidian.VaultDefinition, bar *progressBar) error {
	run := func() error {
		return runUnifiedIndex(cmd, noteMetadata, vaultPath, vaultDef, bar)
	}

	return runIndexWithRebuildGuidance(cmd.ErrOrStderr(), indexRebuild, run)
}

func runIndexWithRebuildGuidance(out io.Writer, explicitRebuild bool, run func() error) error {
	err := run()
	if err == nil || explicitRebuild || !shouldRetryIndexWithFreshRebuild(err) {
		return err
	}

	fmt.Fprintln(out, "Index replacement requires explicit operator authorization.")
	return fmt.Errorf("%w; rerun with --rebuild to clobber and recreate the indexes", err)
}

func shouldRetryIndexWithFreshRebuild(err error) bool {
	if err == nil {
		return false
	}
	if sqliteutil.IsCorruptError(err) || semdb.IsSchemaIncompatibleError(err) {
		return true
	}

	var future *migration.ErrFutureSchema
	if errors.As(err, &future) {
		return true
	}
	var missing *migration.ErrMissingStep
	if errors.As(err, &missing) {
		return true
	}
	var drift *migration.ErrSchemaDrift
	if errors.As(err, &drift) {
		return true
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database busy") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") {
		return false
	}

	return strings.Contains(msg, "sql logic error") ||
		strings.Contains(msg, "sql error") ||
		isRebuildableUniqueConstraintFailure(msg) ||
		strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "no such column") ||
		strings.Contains(msg, "has no column named") ||
		strings.Contains(msg, "no such index") ||
		strings.Contains(msg, "malformed database schema") ||
		strings.Contains(msg, "file is not a database") ||
		strings.Contains(msg, "database disk image is malformed") ||
		strings.Contains(msg, "schema version downgrade") ||
		strings.Contains(msg, "missing migration for version") ||
		strings.Contains(msg, "error applying migration") ||
		(strings.Contains(msg, "migration") && strings.Contains(msg, "failed"))
}

func isRebuildableUniqueConstraintFailure(msg string) bool {
	if !strings.Contains(msg, "unique constraint failed") {
		return false
	}
	rebuildableTables := []string{
		"intel_chunks",
		"intel_embeddings",
		"ontology_nodes",
		"ontology_node_embedding_state",
	}
	for _, table := range rebuildableTables {
		if strings.Contains(msg, table+".") {
			return true
		}
	}
	return false
}
