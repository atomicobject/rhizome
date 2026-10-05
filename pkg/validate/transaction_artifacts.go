package validate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type repairArtifactExpectation struct {
	hash string
	mode uint32
}

func createOwnedRepairArtifact(
	dir string,
	manifest *repairJournalManifest,
	path string,
	content []byte,
	mode os.FileMode,
) error {
	created, err := writeExclusiveSyncedFile(path, content, mode)
	if err != nil {
		if !created {
			return err
		}
		return errors.Join(err, removeJustCreatedRepairArtifact(path))
	}
	if err := persistRepairArtifactOwnership(dir, manifest, path); err != nil {
		removeErr := removeJustCreatedRepairArtifact(path)
		if removeErr != nil {
			return errors.Join(err, removeErr)
		}
		return err
	}
	return nil
}

func linkOwnedRepairArtifact(dir string, manifest *repairJournalManifest, source, path string) error {
	if err := os.Link(source, path); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return errors.Join(err, removeJustCreatedRepairArtifact(path))
	}
	if err := persistRepairArtifactOwnership(dir, manifest, path); err != nil {
		return errors.Join(err, removeJustCreatedRepairArtifact(path))
	}
	return nil
}

func persistRepairArtifactOwnership(dir string, manifest *repairJournalManifest, path string) error {
	if repairArtifactIsOwned(*manifest, path) {
		return nil
	}
	previous := append([]string(nil), manifest.OwnedArtifacts...)
	manifest.OwnedArtifacts = append(manifest.OwnedArtifacts, path)
	if err := writeRepairJournalManifest(dir, *manifest); err != nil {
		manifest.OwnedArtifacts = previous
		return fmt.Errorf("persist repair artifact ownership: %w", err)
	}
	return nil
}

func removeJustCreatedRepairArtifact(path string) error {
	var cleanupErr error
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove newly created repair artifact %s: %w", path, err))
	}
	if cleanupErr == nil {
		cleanupErr = errors.Join(cleanupErr, syncDirectory(filepath.Dir(path)))
	}
	return cleanupErr
}

func validateRepairArtifactManifest(manifest repairJournalManifest) error {
	expected := expectedRepairArtifacts(manifest)
	owned := make(map[string]struct{}, len(manifest.OwnedArtifacts))
	for _, path := range manifest.OwnedArtifacts {
		if _, ok := expected[path]; !ok {
			return fmt.Errorf("journal claims ownership of unexpected repair artifact %s", path)
		}
		if _, duplicate := owned[path]; duplicate {
			return fmt.Errorf("journal claims duplicate repair artifact ownership %s", path)
		}
		owned[path] = struct{}{}
	}
	return nil
}

func validateRepairArtifactOwnership(manifest repairJournalManifest) error {
	expected := expectedRepairArtifacts(manifest)
	owned := make(map[string]struct{}, len(manifest.OwnedArtifacts))
	for _, path := range manifest.OwnedArtifacts {
		owned[path] = struct{}{}
	}
	for path, expectation := range expected {
		if nativeGitArtifactReleased(manifest, path) {
			continue
		}
		_, statErr := os.Lstat(path)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if _, ok := owned[path]; !ok {
			return fmt.Errorf("repair artifact ownership is absent for existing path %s", path)
		}
		if err := validateRepairFileSnapshot(path, path, true, expectation.hash, expectation.mode); err != nil {
			return fmt.Errorf("repair artifact ownership changed: %w", err)
		}
	}
	return nil
}

func expectedRepairArtifacts(manifest repairJournalManifest) map[string]repairArtifactExpectation {
	expected := make(map[string]repairArtifactExpectation, len(manifest.Entries)*3)
	for _, entry := range manifest.Entries {
		if entry.BackupPath != "" {
			expected[entry.BackupPath] = repairArtifactExpectation{hash: entry.OriginalHash, mode: entry.OriginalMode}
		}
		if entry.StagePath != "" {
			expected[entry.StagePath] = repairArtifactExpectation{hash: entry.FinalHash, mode: entry.FinalMode}
		}
		if entry.CasePath != "" {
			expected[entry.CasePath] = repairArtifactExpectation{hash: entry.OriginalHash, mode: entry.OriginalMode}
		}
	}
	if manifest.Namespace != nil && manifest.Namespace.Git != nil {
		git := manifest.Namespace.Git
		for _, artifact := range []namespaceGitArtifact{git.Rollback, git.Candidate} {
			expected[artifact.Path] = repairArtifactExpectation{hash: artifact.Hash, mode: artifact.Mode}
		}
		expected[git.LockPath] = repairArtifactExpectation{hash: git.LockHash, mode: git.LockMode}
	}
	return expected
}

func repairArtifactIsOwned(manifest repairJournalManifest, path string) bool {
	for _, owned := range manifest.OwnedArtifacts {
		if owned == path {
			return true
		}
	}
	return false
}
