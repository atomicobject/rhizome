package sourcetxn

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrNamespaceGitAdmission = errors.New("native Git exclusion unavailable before publication")

// Static authority is checked even for settled journals: cleanup may remove
// only transaction-bound snapshots, regardless of today's Git directory state.
func validateNamespaceGitAuthority(runCtx writerContext, manifest Manifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	dir := filepath.Join(filepath.FromSlash(runCtx.VaultPath), ".git")
	if filepath.Clean(git.GitDir) != dir || git.IndexPath != filepath.Join(dir, "index") || git.LockPath != filepath.Join(dir, "index.lock") {
		return fmt.Errorf("native Git journal binding is outside canonical vault")
	}
	rollback, candidate := namespaceGitPaths(manifest)
	_, tokenErr := hex.DecodeString(git.Token)
	if git.Rollback.Path != rollback || git.Candidate.Path != candidate ||
		!validNamespaceHash(git.Rollback.Hash) || !validNamespaceHash(git.Candidate.Hash) ||
		git.Rollback.Mode != RepairModeBits(RepairFileMode(git.Rollback.Mode)) || git.Candidate.Mode != RepairModeBits(RepairFileMode(git.Candidate.Mode)) ||
		(git.RawOriginal.Exists && (!validNamespaceHash(git.RawOriginal.Hash) || git.RawOriginal.Mode != RepairModeBits(RepairFileMode(git.RawOriginal.Mode)))) ||
		(!git.RawOriginal.Exists && (git.RawOriginal.Hash != "" || git.RawOriginal.Mode != 0)) ||
		len(git.Token) != 32 || tokenErr != nil || git.LockHash != Hash(namespaceGitLockContent(git)) || git.LockMode != 0o600 {
		return fmt.Errorf("native Git journal witness is invalid")
	}
	return nil
}

func namespaceGitPaths(manifest Manifest) (string, string) {
	git := manifest.Namespace.Git
	short := strings.TrimPrefix(Hash([]byte(manifest.TransactionID)), "sha256:")[:12]
	return git.IndexPath + ".rzm-repair-" + short + ".rollback", git.IndexPath + ".rzm-repair-" + short + ".candidate"
}

// Bind again under live exclusion and before every replay action. Captured
// absolute paths alone do not authorize writes through a redirected .git.
func validateNamespaceGitBinding(runCtx writerContext, manifest Manifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := validateNamespaceGitAuthority(runCtx, manifest); err != nil {
		return err
	}
	dir := filepath.Join(filepath.FromSlash(runCtx.VaultPath), ".git")
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("native Git directory binding changed")
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(git.GitDir) || filepath.Clean(git.IndexPath) != filepath.Join(resolved, "index") || filepath.Clean(git.LockPath) != filepath.Join(resolved, "index.lock") {
		return fmt.Errorf("native Git index/lock binding changed")
	}
	return nil
}

func namespaceGitLockContent(git *GitJournal) []byte {
	return []byte("rhizome namespace " + git.Token + "\n")
}

func acquireNamespaceGitExclusion(runCtx writerContext, prepared *preparedTransaction) error {
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return err
	}

	git := prepared.Manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := validateNamespaceGitBinding(runCtx, prepared.Manifest); err != nil {
		return err
	}
	if _, err := os.Lstat(git.LockPath); err == nil {
		if !repairArtifactIsOwned(prepared.Manifest, git.LockPath) {
			return ErrNamespaceGitAdmission
		}
		if err := validateRepairFileSnapshot(git.LockPath, "native Git lock", true, git.LockHash, git.LockMode); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		// Fresh exclusion uses exclusive creation. Prepared discovery may first
		// adopt an exact-token lock after verifying every original witness.
		if err := createOwnedRepairArtifact(prepared.Directory, &prepared.Manifest, git.LockPath, namespaceGitLockContent(git), RepairFileMode(git.LockMode)); err != nil {
			if errors.Is(err, os.ErrExist) {
				return errors.Join(ErrNamespaceGitAdmission, err)
			}
			return err
		}
	}
	return validateNamespaceGitBinding(runCtx, prepared.Manifest)
}

// An exclusive-creation refusal proves no live Git effect. Discard only owned
// preparation evidence, then prepare the existing filesystem fallback anew.
func abandonNativeGitPreparation(runCtx writerContext, prepared *preparedTransaction) error {
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return err
	}

	git := prepared.Manifest.Namespace.Git
	if prepared.Installed != 0 || repairArtifactIsOwned(prepared.Manifest, git.LockPath) {
		return fmt.Errorf("cannot abandon published Git participation")
	}
	if err := verifyNamespaceOriginalFiles(runCtx, prepared.Manifest); err != nil {
		return err
	}
	if err := validateNamespaceGitBinding(runCtx, prepared.Manifest); err != nil {
		return err
	}
	for _, artifact := range []GitArtifact{git.Rollback, git.Candidate} {
		if !repairArtifactIsOwned(prepared.Manifest, artifact.Path) {
			return fmt.Errorf("native fallback snapshot ownership is absent")
		}
		if err := validateRepairFileSnapshot(artifact.Path, "native fallback snapshot", true, artifact.Hash, artifact.Mode); err != nil {
			return err
		}
		if err := removeJustCreatedRepairArtifact(artifact.Path); err != nil {
			return err
		}
	}
	var retained []string
	for _, path := range prepared.Manifest.OwnedArtifacts {
		if path != git.Rollback.Path && path != git.Candidate.Path {
			retained = append(retained, path)
		}
	}
	prepared.Manifest.OwnedArtifacts = retained
	prepared.Manifest.Namespace.Git, prepared.Manifest.Namespace.GitMoves = nil, nil
	if err := writeRepairJournalManifest(prepared.Directory, prepared.Manifest); err != nil {
		return err
	}
	return cleanupFailedPrepare(runCtx, prepared.Directory, prepared.Manifest, nil)
}

func verifyNamespaceGitLock(runCtx writerContext, manifest Manifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := validateNamespaceGitBinding(runCtx, manifest); err != nil {
		return err
	}
	if manifest.Namespace.SettledDecision != "" || !repairArtifactIsOwned(manifest, git.LockPath) {
		return fmt.Errorf("native Git exclusion is not held by this journal")
	}
	return validateRepairFileSnapshot(git.LockPath, "native Git lock", true, git.LockHash, git.LockMode)
}

func verifyNamespaceGitOriginal(runCtx writerContext, manifest Manifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := verifyNamespaceGitLock(runCtx, manifest); err != nil {
		return err
	}
	return validateRepairFileSnapshot(git.IndexPath, "native original Git index", git.RawOriginal.Exists, git.RawOriginal.Hash, git.RawOriginal.Mode)
}

func publishNamespaceGitIndex(runCtx writerContext, prepared *preparedTransaction) error {
	git := prepared.Manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := verifyNamespaceGitOriginal(runCtx, prepared.Manifest); err != nil {
		return err
	}
	if !repairArtifactIsOwned(prepared.Manifest, git.Candidate.Path) {
		return fmt.Errorf("native Git candidate ownership is absent")
	}
	if err := validateRepairFileSnapshot(git.Candidate.Path, "native Git candidate", true, git.Candidate.Hash, git.Candidate.Mode); err != nil {
		return err
	}
	return replaceFile(git.Candidate.Path, git.IndexPath)
}

func verifyNamespaceGitFinal(runCtx writerContext, manifest Manifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := verifyNamespaceGitLock(runCtx, manifest); err != nil {
		return err
	}
	return validateRepairFileSnapshot(git.IndexPath, "native final Git index", true, git.Candidate.Hash, git.Candidate.Mode)
}

func preflightNamespaceGitRollback(runCtx writerContext, manifest Manifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := validateNamespaceGitBinding(runCtx, manifest); err != nil {
		return err
	}
	if _, err := os.Lstat(git.LockPath); err == nil {
		if err := verifyNamespaceGitLock(runCtx, manifest); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if namespaceGitIndexMatches(git.IndexPath, git.RawOriginal.Exists, git.RawOriginal.Hash, git.RawOriginal.Mode) || namespaceGitIndexMatches(git.IndexPath, true, git.Rollback.Hash, git.Rollback.Mode) {
		return nil
	}
	if !namespaceGitIndexMatches(git.IndexPath, true, git.Candidate.Hash, git.Candidate.Mode) {
		return fmt.Errorf("native Git index changed after publication")
	}
	if !repairArtifactIsOwned(manifest, git.Rollback.Path) {
		return fmt.Errorf("native Git rollback snapshot ownership is absent")
	}
	return validateRepairFileSnapshot(git.Rollback.Path, "native Git rollback snapshot", true, git.Rollback.Hash, git.Rollback.Mode)
}

func namespaceGitIndexMatches(path string, exists bool, hash string, mode uint32) bool {
	return validateRepairFileSnapshot(path, "native Git index", exists, hash, mode) == nil
}

func restoreNamespaceGitIndex(runCtx writerContext, prepared *preparedTransaction) error {
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return err
	}

	git := prepared.Manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := verifyNamespaceGitLock(runCtx, prepared.Manifest); err != nil {
		return err
	}
	if err := preflightNamespaceGitRollback(runCtx, prepared.Manifest); err != nil {
		return err
	}
	if namespaceGitIndexMatches(git.IndexPath, git.RawOriginal.Exists, git.RawOriginal.Hash, git.RawOriginal.Mode) || namespaceGitIndexMatches(git.IndexPath, true, git.Rollback.Hash, git.Rollback.Mode) {
		return nil
	}
	return replaceFile(git.Rollback.Path, git.IndexPath)
}

func verifyNamespaceGitRestored(runCtx writerContext, manifest Manifest) error {
	git := manifest.Namespace.Git
	if git == nil {
		return nil
	}
	if err := verifyNamespaceGitLock(runCtx, manifest); err != nil {
		return err
	}
	if namespaceGitIndexMatches(git.IndexPath, git.RawOriginal.Exists, git.RawOriginal.Hash, git.RawOriginal.Mode) || namespaceGitIndexMatches(git.IndexPath, true, git.Rollback.Hash, git.Rollback.Mode) {
		return nil
	}
	return fmt.Errorf("native Git staging restoration is not verified")
}

func settleNamespaceGitRelease(runCtx writerContext, journal *journal) error {
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return err
	}

	git := journal.Manifest.Namespace.Git
	if journal.Manifest.Namespace.SettledDecision != "" {
		if journal.Manifest.Namespace.SettledDecision != journal.State {
			return fmt.Errorf("native settled decision disagrees with marker")
		}
		journal.DecisionSynced = true
		return nil
	}
	if journal.State != repairJournalCommitted && journal.State != repairJournalRestored {
		return fmt.Errorf("native Git release requires a durable decision")
	}
	if err := syncNamespaceDecision(journal.Directory, journal.State); err != nil {
		return err
	}
	journal.DecisionSynced = true
	if git != nil {
		if err := validateNamespaceGitBinding(runCtx, journal.Manifest); err != nil {
			return err
		}
		if _, err := os.Lstat(git.LockPath); err == nil {
			if err := verifyNamespaceGitLock(runCtx, journal.Manifest); err != nil {
				return err
			}
			if err := os.Remove(git.LockPath); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := syncDirectory(filepath.Dir(git.LockPath)); err != nil {
			return err
		}
	}
	journal.Manifest.Namespace.SettledDecision = journal.State
	if err := writeRepairJournalManifest(journal.Directory, journal.Manifest); err != nil {
		journal.Manifest.Namespace.SettledDecision = ""
		return fmt.Errorf("persist native Git release: %w", err)
	}
	return nil
}

func nativeGitArtifactReleased(manifest Manifest, path string) bool {
	return manifest.Namespace != nil && manifest.Namespace.Git != nil && manifest.Namespace.SettledDecision != "" && path == manifest.Namespace.Git.LockPath
}

type NamespaceDecisionSyncError struct{ Err error }

func (e NamespaceDecisionSyncError) Error() string {
	return "native decision synchronization failed: " + e.Err.Error()
}
func (e NamespaceDecisionSyncError) Unwrap() error { return e.Err }

func restoreNamespacePrepared(runCtx writerContext, prepared *preparedTransaction, hooks *Hooks) error {
	if err := runCtx.Lease.Validate(runCtx.VaultPath); err != nil {
		return err
	}

	if err := acquireNamespaceGitExclusion(runCtx, prepared); err != nil {
		return err
	}
	if err := preflightNamespaceGitRollback(runCtx, prepared.Manifest); err != nil {
		return err
	}
	plan, err := preflightRepairRollback(runCtx, prepared.Manifest)
	if err != nil {
		return err
	}
	if _, err := executeRepairRollbackPlan(runCtx, prepared.Manifest, plan, hooks); err != nil {
		return err
	}
	if err := restoreNamespaceGitIndex(runCtx, prepared); err != nil {
		return err
	}
	if err := verifyNamespaceOriginalFiles(runCtx, prepared.Manifest); err != nil {
		return err
	}
	if err := verifyNamespaceGitRestored(runCtx, prepared.Manifest); err != nil {
		return err
	}
	if err := markNamespaceRestored(prepared.Directory, hooks); err != nil {
		return NamespaceDecisionSyncError{Err: err}
	}
	prepared.DecisionSynced = true
	if hooks != nil && hooks.BeforeNamespaceGitRelease != nil {
		if err := hooks.BeforeNamespaceGitRelease(repairJournalRestored); err != nil {
			return err
		}
	}
	journal := journal{Directory: prepared.Directory, Manifest: prepared.Manifest, State: repairJournalRestored}
	err = settleNamespaceGitRelease(runCtx, &journal)
	prepared.Manifest = journal.Manifest
	return err
}
