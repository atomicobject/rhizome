package ontology

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

type editRecoveryEntry struct {
	editWriteJournalEntry
	abs         string
	fingerprint string
	exists      bool
}

type editRecoveryPlan struct {
	entries    []editRecoveryEntry
	completion *editCompletionJournalEntry
	artifacts  []string
	allNew     bool
}

func recoverEditWriteJournal(vaultPaths *paths.VaultPaths, journalPath string, journal editWriteJournal) error {
	plan, err := preflightEditRecovery(vaultPaths, journalPath, journal)
	if err != nil {
		return err
	}
	if journal.Phase == "committed" || plan.allNew {
		if journal.Phase != "committed" {
			// Keep the terminal decision durable if artifact cleanup is interrupted.
			if err := markEditWriteJournalCommitted(journalPath); err != nil {
				return err
			}
		}
		return cleanupRecoveredEdit(journalPath, plan.artifacts)
	}
	// Every rollback witness was checked before changing any source or artifact.
	for i := len(plan.entries) - 1; i >= 0; i-- {
		entry := plan.entries[i]
		if !entry.exists || entry.fingerprint != entry.OldFingerprint {
			if err := removeIfPresent(entry.abs); err != nil {
				return err
			}
			if err := os.Rename(entry.BackupPath, entry.abs); err != nil {
				return err
			}
			if err := syncEditDirectory(filepath.Dir(entry.abs)); err != nil {
				return err
			}
		}
	}
	if plan.completion != nil {
		if err := removeIfPresent(plan.completion.TargetPath); err != nil {
			return err
		}
	}
	return cleanupRecoveredEdit(journalPath, plan.artifacts)
}

func cleanupRecoveredEdit(journalPath string, artifacts []string) error {
	var errs []error
	for _, artifact := range artifacts {
		if err := removeIfPresent(artifact); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return removeEditWriteJournal(journalPath)
}

func preflightEditRecovery(vaultPaths *paths.VaultPaths, journalPath string, journal editWriteJournal) (editRecoveryPlan, error) {
	plan := editRecoveryPlan{allNew: true, completion: journal.Completion}
	if journal.Phase != "prepared" && journal.Phase != "committed" {
		return plan, fmt.Errorf("unsupported edit journal phase %q", journal.Phase)
	}
	committed := journal.Phase == "committed"
	root := filepath.Clean(vaultPaths.Root())
	owned := make(map[string]bool)
	claim := func(path string, allowMissingParent bool) error {
		if err := verifyEditRecoveryPath(root, path, allowMissingParent); err != nil {
			return err
		}
		key := paths.CaseKey(path)
		if owned[key] {
			return fmt.Errorf("edit journal paths overlap: %s", path)
		}
		owned[key] = true
		return nil
	}
	if filepath.Dir(journalPath) != filepath.Join(root, ".rhizome", "edit-journal") {
		return plan, fmt.Errorf("edit journal escaped its journal directory")
	}
	if err := claim(journalPath, false); err != nil {
		return plan, err
	}
	if _, _, err := editRecoveryFingerprint(journalPath); err != nil {
		return plan, err
	}
	for _, entry := range journal.Entries {
		clean, err := paths.CleanNotePath(entry.NotePath)
		if err != nil || clean.String() != entry.NotePath || clean == "" {
			return plan, fmt.Errorf("edit journal note path is not canonical: %s", entry.NotePath)
		}
		// The current note must never redirect authority to its replacement target.
		abs := filepath.Join(root, filepath.FromSlash(clean.String()))
		if err := claim(abs, committed); err != nil {
			return plan, err
		}
		verified := editRecoveryEntry{editWriteJournalEntry: entry, abs: abs}
		if !committed {
			verified.fingerprint, verified.exists, err = editRecoveryFingerprint(abs)
			if err != nil {
				return plan, err
			}
			if !verified.exists || verified.fingerprint != entry.NewFingerprint {
				plan.allNew = false
			}
			if verified.exists && verified.fingerprint != entry.OldFingerprint && verified.fingerprint != entry.NewFingerprint {
				return plan, fmt.Errorf("edit recovery stopped because %s changed outside the interrupted transaction", entry.NotePath)
			}
		}
		base := filepath.Base(abs)
		for _, artifact := range []struct{ path, prefix, fingerprint string }{
			{entry.TempPath, base + ".rhizome-write-", entry.NewFingerprint},
			{entry.BackupPath, "." + base + ".rhizome-backup-", entry.OldFingerprint},
		} {
			if filepath.Dir(artifact.path) != filepath.Dir(abs) || !strings.HasPrefix(filepath.Base(artifact.path), artifact.prefix) {
				return plan, fmt.Errorf("edit journal artifact for %s escaped its source directory or has an invalid name", entry.NotePath)
			}
			if err := claim(artifact.path, committed); err != nil {
				return plan, err
			}
			fingerprint, exists, err := editRecoveryFingerprint(artifact.path)
			if err != nil {
				return plan, err
			}
			// Unpublished prepared entries still own their empty backup reservation.
			placeholder := !committed && artifact.path == entry.BackupPath && verified.exists && verified.fingerprint == entry.OldFingerprint && fingerprint == hashText("")
			if exists && fingerprint != artifact.fingerprint && !placeholder {
				return plan, fmt.Errorf("edit recovery cannot verify artifact %s", artifact.path)
			}
			if placeholder && fingerprint != artifact.fingerprint {
				plan.allNew = false
			}
			plan.artifacts = append(plan.artifacts, artifact.path)
		}
		plan.entries = append(plan.entries, verified)
	}
	if completion := journal.Completion; completion != nil {
		if err := validateEditCompletionArtifactPaths(completion); err != nil {
			return plan, err
		}
		receiptRoot := filepath.Join(root, ".rhizome", "edit-receipts")
		rel, err := filepath.Rel(receiptRoot, completion.TargetPath)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return plan, fmt.Errorf("edit completion artifact escaped the receipt directory")
		}
		for _, artifact := range []string{completion.TargetPath, completion.TempPath} {
			if artifact == "" {
				continue
			}
			if err := claim(artifact, true); err != nil {
				return plan, fmt.Errorf("edit completion artifact: %w", err)
			}
			fingerprint, exists, err := editRecoveryFingerprint(artifact)
			if err != nil {
				return plan, err
			}
			if exists && fingerprint != completion.Fingerprint {
				return plan, fmt.Errorf("edit recovery stopped because its completion artifact changed")
			}
			if artifact == completion.TargetPath && !exists {
				if committed {
					return plan, fmt.Errorf("committed edit transaction %q is missing its completion artifact", journal.TransactionID)
				}
				plan.allNew = false
			}
		}
		if completion.TempPath != "" {
			plan.artifacts = append(plan.artifacts, completion.TempPath)
		}
	}
	if !committed && !plan.allNew {
		for _, entry := range plan.entries {
			if entry.exists && entry.fingerprint == entry.OldFingerprint {
				continue
			}
			fingerprint, exists, err := editRecoveryFingerprint(entry.BackupPath)
			if err != nil || !exists || fingerprint != entry.OldFingerprint {
				return plan, fmt.Errorf("edit recovery cannot verify the backup for %s", entry.NotePath)
			}
		}
	}
	return plan, nil
}

// Walk direct parents from the canonical root. A missing suffix is safe only
// for terminal cleanup; replaced or symlink parents never redirect artifact work.
func verifyEditRecoveryPath(root, path string, allowMissingParent bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("edit recovery path is not canonical: %s", path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if clean, err := paths.CleanRelPath(filepath.ToSlash(rel)); err != nil || clean == "" {
		return fmt.Errorf("edit recovery path escaped vault: %s", path)
	}
	parent := root
	for _, part := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
		parent = filepath.Join(parent, part)
		info, err := os.Lstat(parent)
		if errors.Is(err, os.ErrNotExist) && allowMissingParent {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("edit recovery parent is not a direct directory: %s", parent)
		}
	}
	return nil
}

func editRecoveryFingerprint(path string) (string, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() || editFileHasMultipleLinks(info) {
		return "", false, fmt.Errorf("edit recovery artifact is not a direct regular file: %s", path)
	}
	return editFileFingerprint(path)
}
