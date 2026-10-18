package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/app/userstate"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/spf13/cobra"
)

func (commandOptions *indexInvocationOptions) makeNewWorktreeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new-worktree <source-worktree-path>",
		Short: "Prepare a new worktree with the Rhizome index and personal settings",
		Long: `Prepare the current worktree by copying the unified Rhizome database from
another worktree, then running indexing in the current worktree to bring it up
to date. Personal view settings are copied when the source has them and the
current worktree has no user state yet. Later preference changes are independent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourcePath := args[0]
			targetDef, err := commandOptions.vaultDefOrDefaultContext(cmd.Context())
			if err != nil {
				return err
			}
			source, err := resolveNewWorktreeSource(sourcePath)
			if err != nil {
				return err
			}
			target, err := resolveNewWorktreeTarget(targetDef)
			if err != nil {
				return err
			}
			return commandOptions.runNewWorktree(cmd, source, target)
		},
	}
}

type newWorktreeIndexSource struct {
	Root   string
	DBPath string
}

type newWorktreeIndexTarget struct {
	Def    obsidian.VaultDefinition
	Root   string
	DBPath string
}

func resolveNewWorktreeSource(path string) (newWorktreeIndexSource, error) {
	cfgDir, cfg, err := repoconfig.FindLocalConfig(path)
	if err != nil {
		return newWorktreeIndexSource{}, fmt.Errorf("resolve source worktree: %w", err)
	}
	def := repoconfig.LocalConfigToDefinition(cfgDir, cfg)
	root := def.BasePath()
	return newWorktreeIndexSource{
		Root:   root,
		DBPath: repoconfig.UnifiedIndexPath(root, cfg.IndexPath),
	}, nil
}

func resolveNewWorktreeTarget(def obsidian.VaultDefinition) (newWorktreeIndexTarget, error) {
	root := def.BasePath()
	cfg, err := repoconfig.LoadLocalConfig(root)
	if err != nil {
		return newWorktreeIndexTarget{}, fmt.Errorf("resolve target worktree: %w", err)
	}
	return newWorktreeIndexTarget{
		Def:    def,
		Root:   root,
		DBPath: repoconfig.UnifiedIndexPath(root, cfg.IndexPath),
	}, nil
}

func (commandOptions *indexInvocationOptions) runNewWorktree(cmd *cobra.Command, source newWorktreeIndexSource, target newWorktreeIndexTarget) error {
	if sameCleanPath(source.DBPath, target.DBPath) {
		return fmt.Errorf("source and target resolve to the same Rhizome database: %s", source.DBPath)
	}
	// The target may already have an auto-started runtime holding its
	// database open; replacing the file under it would split readers across
	// inodes. Stop a headless one first (delegated indexing restarts it) and
	// refuse an attached one, exactly as `rzm index --rebuild` does. The
	// source needs no such care: it is read as one SQLite snapshot, so any
	// writer there (runtime, one-shot indexer, note edit) leaves the copy
	// consistent.
	_, releaseSpawnLease, err := stopRuntimeForRebuild(cmd.Context(), target.Root, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	// A one-shot target writer may hold index.lock without a runtime manifest.
	// Keep the spawn lease while waiting and copying so neither kind of writer
	// can hold the target database open when its files are replaced.
	var cloned bool
	copyErr := func() error {
		defer releaseSpawnLease()
		releaseIndexLock, err := indexing.TryAcquireIndexLock(cmd.Context(), target.Root, indexing.LockWaitOptions{RequestPriority: true, Debug: commandOptions.debug, Out: cmd.ErrOrStderr()})
		if err != nil {
			return err
		}
		cloned, err = copyRhizomeIndex(cmd.Context(), source.DBPath, target.DBPath)
		if err == nil {
			var seeded bool
			seeded, err = userstate.SeedWorktree(cmd.Context(), source.Root, target.Root)
			if seeded {
				fmt.Fprintln(cmd.ErrOrStderr(), "Copied personal view settings from source worktree.")
			}
		}
		return errors.Join(err, releaseIndexLock.Release())
	}()
	if copyErr != nil {
		return copyErr
	}
	verb := "Copied"
	if cloned {
		verb = "Cloned"
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s Rhizome index from %s\n", verb, source.DBPath)
	if err := commandOptions.newWorktreeRunIndex(cmd, target.Root, target.Def); err != nil {
		return err
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "Rhizome index is up to date.")
	return nil
}

var cloneRhizomeIndex = sqliteutil.CloneInto

// copyRhizomeIndex replaces the target database with a consistent snapshot of
// the source and reports whether it was a copy-on-write clone. Copying db,
// WAL, and SHM files one at a time can mix a concurrent writer's states, so
// the clone holds the source's write lock, and the fallback VACUUM INTO reads
// one transaction into a single checkpointed file. Stale target sidecars are
// removed rather than overwritten.
func copyRhizomeIndex(ctx context.Context, sourceDB, targetDB string) (bool, error) {
	if _, err := os.Stat(sourceDB); err != nil {
		if os.IsNotExist(err) {
			return false, fmt.Errorf("source Rhizome database not found at %s; run `rzm index` in the source worktree first", sourceDB)
		}
		return false, fmt.Errorf("stat source Rhizome database: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(targetDB), 0o755); err != nil {
		return false, fmt.Errorf("create target Rhizome directory: %w", err)
	}
	for _, path := range sqliteDatabaseFiles(targetDB) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("remove stale target database file %s: %w", path, err)
		}
	}
	if err := cloneRhizomeIndex(ctx, sourceDB, targetDB); err == nil {
		return true, nil
	} else if ctx.Err() != nil {
		return false, err
	}
	return false, sqliteutil.SnapshotInto(ctx, sourceDB, targetDB)
}

func sqliteDatabaseFiles(dbPath string) []string {
	return []string{dbPath, dbPath + "-wal", dbPath + "-shm"}
}

// runNewWorktreeIndexThroughRuntime brings the copied database up to date the
// same way `rzm index` does: in the vault runtime when one can be used, in this
// process otherwise.
func (commandOptions *indexInvocationOptions) runNewWorktreeIndexThroughRuntime(cmd *cobra.Command, vaultPath string, vaultDef obsidian.VaultDefinition) error {
	handled, restart, err := commandOptions.runIndexThroughRuntime(cmd, vaultDef)
	if handled {
		return err
	}
	if restart {
		defer restartRuntimeAfterRebuild(cmd.Context(), vaultDef, cmd.ErrOrStderr())
	}
	return commandOptions.runNewWorktreeIndex(cmd, vaultPath, vaultDef)
}

func (commandOptions *indexInvocationOptions) runNewWorktreeIndex(cmd *cobra.Command, vaultPath string, vaultDef obsidian.VaultDefinition) error {
	resolved, err := repoconfig.Load(vaultPath)
	if err != nil {
		return err
	}
	if err := resolved.ValidateEmbeddingsProvider(); err != nil {
		return err
	}
	lease, err := indexing.TryAcquireIndexLock(cmd.Context(), vaultPath, indexing.LockWaitOptions{RequestPriority: true, Debug: commandOptions.debug, Out: cmd.ErrOrStderr()})
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := lease.Release(); releaseErr != nil && commandOptions.debug {
			fmt.Fprintf(os.Stderr, "Warning: failed to release index lock: %v\n", releaseErr)
		}
	}()

	bar := newCLIProgressBar(cmd.ErrOrStderr())
	defer bar.Close()
	noteMetadata, err := newNoteMetadataIndexer()
	if err != nil {
		return err
	}
	if err := commandOptions.runIndexCommandWithRebuildGuidance(cmd, lease, noteMetadata, vaultPath, vaultDef, bar, &resolved); err != nil {
		return err
	}
	cleanupMCPSessions(cmd.Context(), vaultPath, commandOptions.debug)
	return nil
}

func (commandOptions *indexInvocationOptions) registerNewWorktreeCommands(rootCmd, newWorktreeCmd *cobra.Command) {
	rootCmd.AddCommand(newWorktreeCmd)
}
