package sourcetxn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const sourceSettledMarker = "SOURCE_SETTLED"

type sourceSettlement struct {
	Version      int    `json:"version"`
	ManifestHash string `json:"manifestHash"`
}

func settlementHash(dir string) (string, error) {
	content, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return "", err
	}
	return Hash(content), nil
}

func sourceIsSettled(dir string, m Manifest) (bool, error) {
	path := filepath.Join(dir, sourceSettledMarker)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, nil
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false, nil
	}
	hash, err := settlementHash(dir)
	if err != nil {
		return false, nil
	}
	var proof sourceSettlement
	if json.Unmarshal(content, &proof) != nil || proof.Version != 1 || proof.ManifestHash != hash || m.Namespace != nil || filepath.Base(dir) != strings.TrimPrefix(Hash([]byte(m.TransactionID)), "sha256:") {
		return false, nil
	}
	return true, nil
}
func rejectSourceSettlement(run writerContext, m Manifest) error {
	dir, err := repairJournalDir(run, m.TransactionID)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(dir, sourceSettledMarker)); err == nil {
		return fmt.Errorf("source-settled journal has no publication, rollback or cleanup authority")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
func settledEvidence(root, evidence string) (bool, error) {
	journalRoot, err := repairJournalRoot(writerContext{VaultPath: root})
	if err != nil {
		return false, err
	}
	if filepath.Dir(evidence) != journalRoot {
		return false, nil
	}
	m, err := readRepairJournalManifest(evidence)
	if err != nil {
		return false, nil
	}
	return sourceIsSettled(evidence, m)
}

// Unknown artifact bytes stay untouched. Ordinary source can be released only
// after every original witness matches and a durable terminal proof removes all
// future rollback/cleanup authority. Native namespace/Git work remains blocked.
func settleUnpublishedSources(run writerContext, dir string, m Manifest, hooks *Hooks) (bool, error) {
	if m.Namespace != nil {
		return false, nil
	}
	if _, err := os.Lstat(filepath.Join(dir, sourceSettledMarker)); !os.IsNotExist(err) {
		// An invalid proof retains its barrier and zero rollback authority. Do
		// not replace unknown evidence with a newly inferred proof.
		return false, nil
	}
	unowned := false
	for _, entry := range m.Entries {
		if entry.CasePath != "" {
			return false, nil
		}
		for _, path := range []string{entry.BackupPath, entry.StagePath} {
			if path == "" || repairArtifactIsOwned(m, path) {
				continue
			}
			if _, err := os.Lstat(path); err == nil {
				unowned = true
			} else if !os.IsNotExist(err) {
				return false, err
			}
		}
	}
	if !unowned {
		return false, nil
	}
	for _, entry := range m.Entries {
		path := entry.Path
		if entry.OriginalPath != "" {
			path = entry.OriginalPath
		}
		abs, err := repairAbsPath(run, path)
		if err != nil {
			return false, nil
		}
		if err := validateRepairFileSnapshot(abs, path, entry.OriginalExists, entry.OriginalHash, entry.OriginalMode); err != nil {
			return false, nil
		}
	}
	if err := run.Lease.Validate(run.VaultPath); err != nil {
		return false, err
	}
	if hooks != nil && hooks.BeforeSourceSettlement != nil {
		if err := hooks.BeforeSourceSettlement(dir); err != nil {
			return false, err
		}
	}
	hash, err := settlementHash(dir)
	if err != nil {
		return false, err
	}
	proof, _ := json.Marshal(sourceSettlement{Version: 1, ManifestHash: hash})
	temp := filepath.Join(dir, sourceSettledMarker+".tmp")
	if err := writeSyncedFile(temp, append(proof, '\n'), 0600); err != nil {
		return false, err
	}
	if err := publishRepairJournalManifest(temp, filepath.Join(dir, sourceSettledMarker)); err != nil {
		return false, err
	}
	if err := syncDirectory(dir); err != nil {
		return true, err
	}
	if hooks != nil && hooks.AfterSourceSettlement != nil {
		if err := hooks.AfterSourceSettlement(dir); err != nil {
			return true, err
		}
	}
	return true, nil
}

func classifySourceSettlement(run writerContext, dir string, m Manifest, preflightErr error, hooks *Hooks) (bool, error) {
	settled, err := sourceIsSettled(dir, m)
	if err != nil {
		return false, err
	}
	if !settled && preflightErr != nil {
		settled, err = settleUnpublishedSources(run, dir, m, hooks)
		if err != nil {
			return settled, err
		}
	}
	if settled {
		reason := fmt.Errorf("source is settled; unknown artifacts remain preserved for review")
		if preflightErr != nil {
			reason = fmt.Errorf("source is settled; unknown artifacts remain preserved for review: %w", preflightErr)
		}
		if err := quarantineRepair(run, dir, reason); err != nil {
			return false, err
		}
	}
	return settled, nil
}
