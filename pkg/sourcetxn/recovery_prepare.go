package sourcetxn

import (
	"fmt"
	"os"
	"path/filepath"
)

// No adjacent artifact or source write precedes manifest publication. An empty
// digest directory or its unpublished temporary manifest has no source authority.
func discardUnpublishedPrepare(run writerContext, dir string) (bool, error) {
	if !validRepairJournalDigest(filepath.Base(dir)) {
		return false, nil
	}
	if err := run.Lease.Validate(run.VaultPath); err != nil {
		return false, err
	}
	root, err := repairJournalRoot(run)
	if err != nil {
		return false, err
	}
	if filepath.Dir(dir) != root {
		return false, fmt.Errorf("unpublished prepare is outside journal root")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	if _, err := os.Lstat(filepath.Join(dir, "manifest.json")); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return false, err
		}
		if entry.Name() != "manifest.json.tmp" || !info.Mode().IsRegular() {
			return false, nil
		}
	}
	if err := run.Lease.Validate(run.VaultPath); err != nil {
		return false, err
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return false, err
		}
	}
	if err := syncDirectory(dir); err != nil {
		return false, err
	}
	if err := os.Remove(dir); err != nil {
		return false, err
	}
	return true, syncDirectory(root)
}

// A crash may leave a complete exclusive artifact before the ownership update
// reaches the manifest. Adopt only the exact expected file while all sources
// still match their original witnesses; later mutations require explicit owner.
func adoptUnownedPrepareArtifacts(run writerContext, dir string, manifest *Manifest) error {
	if err := releaseCaseLinkPairs(run, *manifest); err != nil {
		return err
	}
	expected := expectedRepairArtifacts(*manifest)
	var unowned []string
	for path, witness := range expected {
		if repairArtifactIsOwned(*manifest, path) || nativeGitArtifactReleased(*manifest, path) {
			continue
		}
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := validateRepairFileSnapshot(path, path, true, witness.hash, witness.mode); err != nil {
			return err
		}
		unowned = append(unowned, path)
	}
	if len(unowned) == 0 {
		return nil
	}
	if err := run.Lease.Validate(run.VaultPath); err != nil {
		return err
	}
	for _, entry := range manifest.Entries {
		path := entry.Path
		if entry.OriginalPath != "" {
			path = entry.OriginalPath
		}
		abs, err := repairAbsPath(run, path)
		if err != nil {
			return err
		}
		if err := validateRepairFileSnapshot(abs, path, entry.OriginalExists, entry.OriginalHash, entry.OriginalMode); err != nil {
			return err
		}
	}
	manifest.OwnedArtifacts = sortedUnique(append(manifest.OwnedArtifacts, unowned...))
	return writeRepairJournalManifest(dir, *manifest)
}

func reconcileQuarantine(run writerContext) error {
	if err := run.Lease.Validate(run.VaultPath); err != nil {
		return err
	}
	records, err := quarantineRecords(run.VaultPath)
	if err != nil {
		return err
	}
	for _, record := range records {
		// A surviving adjacent artifact still needs its evidence owner even when an
		// operator has removed the journal. Do not silently abandon that artifact.
		missing := true
		for _, path := range append([]string{record.Evidence}, record.Artifacts...) {
			if path == "" {
				continue
			}
			if err := verifyEditRecoveryPath(run.VaultPath, path, true); err != nil {
				return err
			}
			if _, err := os.Lstat(path); err == nil {
				missing = false
				break
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		if !missing {
			continue
		}
		target, err := quarantinePath(run.VaultPath, record.Evidence)
		if err != nil {
			return err
		}
		if err := run.Lease.Validate(run.VaultPath); err != nil {
			return err
		}
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := syncDirectory(filepath.Dir(target)); err != nil {
			return err
		}
	}
	return nil
}

func clearQuarantine(run writerContext, evidence string) error {
	target, err := quarantinePath(run.VaultPath, evidence)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(target); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := run.Lease.Validate(run.VaultPath); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(target))
}
