package sourcetxn

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/paths"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type pendingWrite struct {
	notePath, temp, backup, oldFingerprint, newFingerprint string
	mode                                                   os.FileMode
}
type CommitCompletionArtifact struct {
	Path    string
	Content []byte
}

func recoverLegacyStrict(root string) error {
	vault, err := paths.NewVaultPaths(root)
	if err != nil {
		return err
	}
	return recoverEditWriteJournals(&vault)
}
func prepareEditWriteJournal(vaultPaths *paths.VaultPaths, transactionID string, items []pendingWrite, completion *editCompletionJournalEntry) (string, error) {
	if len(items) == 0 {
		return "", nil
	}
	if strings.TrimSpace(transactionID) == "" {
		buf := make([]byte, 12)
		if _, err := rand.Read(buf); err != nil {
			transactionID = fmt.Sprintf("edit-%d", time.Now().UnixNano())
		} else {
			transactionID = "edit-" + hex.EncodeToString(buf)
		}
	}
	journal := editWriteJournal{Version: editWriteJournalVersion, TransactionID: transactionID, Phase: "prepared", Completion: completion}
	for _, item := range items {
		journal.Entries = append(journal.Entries, editWriteJournalEntry{
			NotePath: item.notePath, TempPath: item.temp, BackupPath: item.backup,
			OldFingerprint: item.oldFingerprint, NewFingerprint: item.newFingerprint, Mode: uint32(item.mode),
		})
	}
	dir := filepath.Join(vaultPaths.Root(), ".rhizome", "edit-journal")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(transactionID))
	journalPath := filepath.Join(dir, hex.EncodeToString(digest[:])+".json")
	if _, err := os.Stat(journalPath); err == nil {
		return "", fmt.Errorf("edit transaction %q is already prepared", transactionID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return journalPath, writeEditWriteJournal(journalPath, journal)
}
func prepareEditCompletionArtifact(vaultPaths *paths.VaultPaths, artifact *CommitCompletionArtifact) (*editCompletionJournalEntry, error) {
	if artifact == nil {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Join(vaultPaths.Root(), ".rhizome", "edit-receipts"), 0o700); err != nil {
		return nil, err
	}
	target, err := verifiedEditCompletionTarget(vaultPaths, artifact.Path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(target); err == nil {
		return nil, fmt.Errorf("edit completion artifact already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".completion-*")
	if err != nil {
		return nil, err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		_ = temp.Close()
		if cleanup {
			_ = removeIfPresent(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return nil, err
	}
	if _, err := temp.Write(artifact.Content); err != nil {
		return nil, err
	}
	if err := temp.Sync(); err != nil {
		return nil, err
	}
	if err := temp.Close(); err != nil {
		return nil, err
	}
	if err := syncDirectory(filepath.Dir(target)); err != nil {
		return nil, err
	}
	cleanup = false
	return &editCompletionJournalEntry{TargetPath: target, TempPath: tempPath, Fingerprint: legacyFingerprint(string(artifact.Content))}, nil
}
func verifiedEditCompletionTarget(vaultPaths *paths.VaultPaths, target string) (string, error) {
	target = filepath.Clean(target)
	allowed, err := filepath.EvalSymlinks(filepath.Join(vaultPaths.Root(), ".rhizome", "edit-receipts"))
	if err != nil {
		return "", fmt.Errorf("resolve edit receipt directory: %w", err)
	}
	targetParent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return "", fmt.Errorf("resolve edit receipt target: %w", err)
	}
	target = filepath.Join(targetParent, filepath.Base(target))
	rel, err := filepath.Rel(allowed, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("edit completion artifact escaped the receipt directory")
	}
	return target, nil
}
