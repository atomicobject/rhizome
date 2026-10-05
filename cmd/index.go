package cmd

// Docs:
// - [Indexing pipeline (Hub)](docs/hubs/Indexing pipeline (Hub).md)
// - [Indexing pipeline - rzm index orchestration](docs/reference/analysis/Indexing pipeline - rzm index orchestration.md)
// - [Code Index (Hub)](docs/hubs/Code Index (Hub).md)
// - [Code Index - Unified SQLite DB](docs/reference/analysis/Code Index - Unified SQLite DB.md)

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
)

var (
	indexRebuild     bool
	indexStatus      bool
	indexVacuum      bool
	indexTimings     bool
	indexInProcess   bool
	indexExplainPath string
)

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Manage indexes (semantic + code)",
	Long: `Manage indexes for the vault.

Indexes configured notes and code, refreshing changed content and enabled
embeddings as needed. Use --status to print current index status. An explicit
--rebuild clobbers the unified SQLite database and its WAL/SHM sidecars, then
recreates every index from scratch.

Examples:
  rzm index
  rzm index --status
  rzm index --rebuild`,
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		vaultDef, err := vaultDefOrDefaultContext(cmd.Context())
		if err != nil {
			return err
		}
		return runResolvedIndexCommand(cmd, vaultDef, newNoteMetadataIndexer)
	},
}

func runResolvedIndexCommand(
	cmd *cobra.Command,
	vaultDef obsidian.VaultDefinition,
	buildNoteMetadataIndexer func() (notemeta.Indexer, error),
) (err error) {
	vaultPath := vaultDef.BasePath()

	if indexExplainPath != "" {
		return runIndexExplain(cmd.OutOrStdout(), vaultPath, vaultDef.Excludes, indexExplainPath)
	}

	if indexStatus {
		return printIndexStatus(cmd, vaultPath)
	}
	ctx := cmd.Context()
	var finishTimings func(error)
	startAttempt := func(trigger string) {
		ctx, finishTimings = withIndexTimings(ctx, cmd.ErrOrStderr(), indexTimings, indexDiagnosticOptions{Trigger: trigger, Rebuild: indexRebuild, Vacuum: indexVacuum})
		cmd.SetContext(ctx)
	}
	finishAttempt := func() {
		if finishTimings != nil {
			finishTimings(err)
			finishTimings = nil
		}
	}
	defer finishAttempt()

	var spawnLeaseRelease func()
	switch {
	case indexRebuild:
		startAttempt("rebuild")
		// The rebuild clobbers the database, so no runtime may hold it open.
		restart, releaseSpawnLease, err := stopRuntimeForRebuild(cmd.Context(), vaultPath, cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		// Keep both leases through the full in-process rebuild so no runtime
		// opens the database while it is only partly rebuilt.
		spawnLeaseRelease = releaseSpawnLease
		defer func() {
			finishAttempt()
			if spawnLeaseRelease != nil {
				spawnLeaseRelease()
			}
			if restart {
				restartRuntimeAfterRebuild(cmd.Context(), vaultDef, cmd.ErrOrStderr())
			}
		}()
	case indexInProcess:
		startAttempt("in-process")
		fmt.Fprintln(cmd.ErrOrStderr(), "Indexing in this process: --in-process was requested.")
	case indexVacuum:
		startAttempt("vacuum")
		// Runtime index jobs take no per-request options because requests
		// coalesce, so --vacuum has to run here.
		fmt.Fprintln(cmd.ErrOrStderr(), "Indexing in this process: --vacuum is not a runtime job option.")
	default:
		handled, restart, err := runIndexThroughRuntime(cmd, vaultDef)
		if handled {
			return err
		}
		if restart {
			defer func() {
				finishAttempt()
				restartRuntimeAfterRebuild(cmd.Context(), vaultDef, cmd.ErrOrStderr())
			}()
		}
	}
	if finishTimings == nil {
		startAttempt("runtime-fallback")
	}

	origErr := cmd.ErrOrStderr()
	bar := newCLIProgressBar(origErr)
	defer bar.Close()
	cmd.SetErr(bar.WrapWriter(origErr))
	defer cmd.SetErr(origErr)
	restoreLogs := withProgressLogOutput(bar, origErr, indexTimings)
	defer restoreLogs()

	noteMetadata, err := buildNoteMetadataIndexer()
	if err != nil {
		return err
	}

	lockPath := obsidian.IndexLockPath(vaultPath)
	lockStarted := time.Now()
	release, err := indexing.TryAcquireIndexLock(ctx, lockPath, true, debug, cmd.ErrOrStderr())
	indexingperf.ObserveLatency(ctx, "index.lock_wait", time.Since(lockStarted))
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := release(); releaseErr != nil && debug {
			fmt.Fprintf(os.Stderr, "Warning: failed to release index lock: %v\n", releaseErr)
		}
	}()

	if err := obsidian.ValidateEmbeddingsProvider(vaultPath); err != nil {
		return err
	}
	if err := ensureEmbeddingCredentials(vaultPath, os.Stdin, cmd.ErrOrStderr()); err != nil {
		return err
	}
	if indexRebuild {
		if err := indexing.PrepareFreshRebuild(vaultPath, lockPath); err != nil {
			return err
		}
	}
	if err := runIndexCommandWithRebuildGuidance(cmd, noteMetadata, vaultPath, vaultDef, bar); err != nil {
		return err
	}

	cleanupMCPSessions(ctx, vaultPath, debug)
	return nil
}

func cleanupMCPSessions(ctx context.Context, vaultPath string, verbose bool) {
	indexing.CleanupSessions(ctx, vaultPath, verbose, os.Stderr)
}

func printIndexStatus(cmd *cobra.Command, vaultPath string) error {
	status, err := indexing.PrintStatus(cmd.Context(), vaultPath)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.ErrOrStderr(), status)
	return nil
}

func init() {
	indexCmd.SilenceUsage = true
	indexCmd.Flags().BoolVar(&indexRebuild, "rebuild", false, "Clobber and recreate indexes from scratch")
	indexCmd.Flags().BoolVar(&indexStatus, "status", false, "Print index status and exit")
	indexCmd.Flags().BoolVar(&indexVacuum, "vacuum", false, "Run VACUUM after indexing (reclaims space; can be slow)")
	indexCmd.Flags().StringVar(&indexExplainPath, "explain", "", "Explain whether a path would be indexed under the ignore rules (no indexing is performed)")
	indexCmd.Flags().BoolVar(&indexTimings, "timings", false, "Print rolling indexing performance snapshots and a final timing summary")
	indexCmd.Flags().BoolVar(&indexInProcess, "in-process", false, "Index in this process under the index lock instead of delegating to the vault runtime")
	indexCmd.PersistentFlags().StringVarP(&vaultName, "vault", "v", "", "vault name (uses default if unset)")

	rootCmd.AddCommand(indexCmd)
}
