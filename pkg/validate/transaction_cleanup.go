package validate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/validate/namespaceadmission"
)

const (
	cleanupAfterArtifactRemoved  = "after_artifact_removed"
	cleanupBeforeDetach          = "before_detach"
	cleanupAfterDetach           = "after_detach"
	cleanupAfterCommittedRemoved = "after_committed_removed"
	cleanupAfterManifestRemoved  = "after_manifest_removed"
	cleanupAfterDirectoryRemoved = "after_directory_removed"
)

var repairJournalMetadataNames = map[string]struct{}{
	"COMMITTED":         {},
	"RESTORED":          {},
	"manifest.json":     {},
	"manifest.json.tmp": {},
}

func cleanupRepairJournal(
	runCtx RunContext,
	dir string,
	manifest repairJournalManifest,
	hookOptions ...*repairExecutionHooks,
) error {
	if err := validateRepairJournalDir(runCtx, dir, manifest); err != nil {
		return err
	}
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := validateRepairJournalPaths(runCtx, manifest); err != nil {
		return err
	}
	if err := validateRepairArtifactOwnership(manifest); err != nil {
		return err
	}
	if err := validateRepairJournalMetadata(dir); err != nil {
		return err
	}
	hooks := firstRepairCleanupHooks(hookOptions)
	var cleanupErr error
	for _, artifact := range manifest.OwnedArtifacts {
		if nativeGitArtifactReleased(manifest, artifact) {
			continue
		}
		if err := os.Remove(artifact); err != nil && !os.IsNotExist(err) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove repair artifact %s: %w", artifact, err))
			continue
		}
		if err := syncDirectory(filepath.Dir(artifact)); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
			continue
		}
		if hookErr := runRepairCleanupHook(hooks, cleanupAfterArtifactRemoved, artifact); hookErr != nil {
			return errors.Join(cleanupErr, hookErr)
		}
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	if err := runRepairCleanupHook(hooks, cleanupBeforeDetach, dir); err != nil {
		return err
	}
	root := filepath.Dir(dir)
	digest := filepath.Base(dir)
	tombstone := filepath.Join(root, repairJournalCleanupPrefix+digest)
	if manifest.Namespace != nil {
		decision, err := nativeJournalDecision(dir)
		if err != nil || manifest.Namespace.SettledDecision != decision || (decision != repairJournalCommitted && decision != repairJournalRestored) {
			return fmt.Errorf("native cleanup requires a settled terminal decision")
		}
		tombstone = filepath.Join(root, namespaceadmission.NativeCleanupPrefix+decision+"-"+digest)
	}
	if _, err := os.Lstat(tombstone); err == nil {
		return fmt.Errorf("repair cleanup tombstone already exists: %s", tombstone)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := detachRepairJournal(dir, tombstone); err != nil {
		return fmt.Errorf("detach repair journal for cleanup: %w", err)
	}
	if err := syncDirectory(root); err != nil {
		return fmt.Errorf("sync detached repair journal: %w", err)
	}
	if err := runRepairCleanupHook(hooks, cleanupAfterDetach, tombstone); err != nil {
		return err
	}
	return cleanupDetachedRepairJournal(runCtx, tombstone, hooks)
}

func cleanupDetachedRepairJournal(runCtx RunContext, dir string, hooks *repairExecutionHooks) error {
	root, err := repairJournalRoot(runCtx)
	if err != nil {
		return err
	}
	name := filepath.Base(dir)
	_, cleanup, parseErr := namespaceadmission.ParseCleanupName(name)
	if filepath.Clean(filepath.Dir(dir)) != filepath.Clean(root) ||
		!cleanup || parseErr != nil {
		return fmt.Errorf("invalid repair cleanup tombstone: %s", dir)
	}
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("repair cleanup tombstone is not a direct directory: %s", dir)
	}
	if err := validateRepairJournalMetadata(dir); err != nil {
		return fmt.Errorf("repair journal cleanup_pending: %w", err)
	}
	if err := removeRepairCleanupMetadata(dir, "COMMITTED"); err != nil {
		return err
	}
	if err := removeRepairCleanupMetadata(dir, "RESTORED"); err != nil {
		return err
	}
	if err := syncDirectory(dir); err != nil {
		return err
	}
	if err := runRepairCleanupHook(hooks, cleanupAfterCommittedRemoved, dir); err != nil {
		return err
	}
	for _, name := range []string{"manifest.json.tmp", "manifest.json"} {
		if err := removeRepairCleanupMetadata(dir, name); err != nil {
			return err
		}
	}
	if err := syncDirectory(dir); err != nil {
		return err
	}
	if err := runRepairCleanupHook(hooks, cleanupAfterManifestRemoved, dir); err != nil {
		return err
	}
	if err := os.Remove(dir); err != nil {
		return fmt.Errorf("remove repair cleanup tombstone %s: %w", dir, err)
	}
	if err := syncDirectory(root); err != nil {
		return err
	}
	return runRepairCleanupHook(hooks, cleanupAfterDirectoryRemoved, dir)
}

func validateRepairJournalMetadata(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read repair journal metadata %s: %w", dir, err)
	}
	for _, entry := range entries {
		if _, ok := repairJournalMetadataNames[entry.Name()]; !ok || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected repair journal entry %s", filepath.Join(dir, entry.Name()))
		}
	}
	return nil
}

func removeRepairCleanupMetadata(dir, name string) error {
	path := filepath.Join(dir, name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove repair journal file %s: %w", name, err)
	}
	return nil
}

func firstRepairCleanupHooks(options []*repairExecutionHooks) *repairExecutionHooks {
	if len(options) == 0 {
		return nil
	}
	return options[0]
}

func runRepairCleanupHook(hooks *repairExecutionHooks, boundary, path string) error {
	if hooks == nil || hooks.AfterCleanupBoundary == nil {
		return nil
	}
	return hooks.AfterCleanupBoundary(boundary, path)
}
