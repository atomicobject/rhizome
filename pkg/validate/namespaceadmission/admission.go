// Package namespaceadmission fences legacy writers while the validate owner
// has unfinished namespace publication or Git exclusion to recover.
package namespaceadmission

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

const (
	RepairVersion       = 1
	NamespaceVersion    = 2
	PurposeVersion      = 1
	CleanupPrefix       = ".cleanup-"
	NativeCleanupPrefix = ".namespace-cleanup-"
	CommittedMarker     = "COMMITTED"
	RestoredMarker      = "RESTORED"
)

// Identity and purpose headers are shared with the publishing owner. Admission
// reads only this envelope; operation/artifact validation belongs to recovery.
type Identity struct {
	Version       int    `json:"version"`
	TransactionID string `json:"transactionId"`
}

type PurposeHeader struct {
	Version         int    `json:"version"`
	SettledDecision string `json:"settledDecision,omitempty"`
}

// CleanupName is terminal namespace authority only. Optional manifest data
// cannot turn a detached directory back into rollback or refresh authority.
type CleanupName struct{ Digest, Decision string }

func ParseCleanupName(name string) (CleanupName, bool, error) {
	if strings.HasPrefix(name, CleanupPrefix) {
		digest := strings.TrimPrefix(name, CleanupPrefix)
		if !validDigest(digest) {
			return CleanupName{}, true, fmt.Errorf("invalid repair cleanup tombstone %s", name)
		}
		return CleanupName{Digest: digest}, true, nil
	}
	if strings.HasPrefix(name, NativeCleanupPrefix) {
		for _, decision := range []string{"committed", "restored"} {
			prefix := NativeCleanupPrefix + decision + "-"
			if strings.HasPrefix(name, prefix) && validDigest(strings.TrimPrefix(name, prefix)) {
				return CleanupName{Digest: strings.TrimPrefix(name, prefix), Decision: decision}, true, nil
			}
		}
		return CleanupName{}, true, fmt.Errorf("invalid native cleanup tombstone %s", name)
	}
	return CleanupName{}, false, nil
}

type envelope struct {
	Identity
	Namespace *struct {
		PurposeHeader
	} `json:"namespace,omitempty"`
}

// Check is read-only. Its caller already holds the canonical vault writer lease.
func Check(vaultPath string) error {
	vault, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vault.Root() == "" {
		return fmt.Errorf("namespace writer admission requires a canonical vault: %w", err)
	}
	root := filepath.FromSlash(vault.Root())
	for _, part := range []string{".rhizome", "repair-journal", "v1"} {
		root = filepath.Join(root, part)
		info, err := os.Lstat(root)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("namespace journal root is not a direct directory: %s", root)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid namespace journal discovery entry %s", entry.Name())
		}
		if _, cleanup, err := ParseCleanupName(entry.Name()); cleanup || err != nil {
			if err != nil {
				return err
			}
			continue // Detached cleanup names are terminal authority.
		}
		var header envelope
		data, err := readRegular(filepath.Join(dir, "manifest.json"))
		if err != nil || json.Unmarshal(data, &header) != nil || header.TransactionID == "" ||
			(header.Version != RepairVersion && header.Version != NamespaceVersion) {
			return fmt.Errorf("malformed or unsupported namespace journal %s; run rzm validate fix --apply", entry.Name())
		}
		digest := sha256.Sum256([]byte(header.TransactionID))
		if entry.Name() != hex.EncodeToString(digest[:]) {
			return fmt.Errorf("namespace journal directory does not match transaction %s", header.TransactionID)
		}
		if header.Namespace == nil {
			if header.Version != RepairVersion {
				return refuse(header.TransactionID, "missing native purpose")
			}
			continue
		}
		if header.Version != NamespaceVersion || header.Namespace.Version != PurposeVersion {
			return refuse(header.TransactionID, "unsupported purpose")
		}
		committed, err := markerPresent(dir, CommittedMarker, "committed\n")
		if err != nil {
			return refuse(header.TransactionID, "invalid committed decision")
		}
		restored, err := markerPresent(dir, RestoredMarker, "restored\n")
		if err != nil || (committed && restored) {
			return refuse(header.TransactionID, "invalid restored decision")
		}
		if !committed && !restored {
			return refuse(header.TransactionID, "prepared")
		}
		decision := "restored"
		if committed {
			decision = "committed"
		}
		if header.Namespace.SettledDecision != decision {
			return refuse(header.TransactionID, "decision or Git release pending")
		}
	}
	return nil
}

func refuse(id, state string) error {
	return fmt.Errorf("namespace transaction %s is %s; run rzm validate fix --apply before writing", id, state)
}

func markerPresent(dir, name, expected string) (bool, error) {
	data, err := readRegular(filepath.Join(dir, name))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if string(data) != expected {
		return false, fmt.Errorf("invalid %s marker", name)
	}
	return true, nil
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("journal metadata is not a regular file")
	}
	return os.ReadFile(path)
}

func validDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
