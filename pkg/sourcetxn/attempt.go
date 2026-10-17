package sourcetxn

import (
	"crypto/rand"
	"encoding/hex"
	"os"
)

// Retained settled evidence has no source authority, but still owns its journal
// name and adjacent artifacts. A retry gets a separate publication attempt.
func repairAttemptID(run writerContext, requested string) (string, error) {
	dir, err := repairJournalDir(run, requested)
	if err != nil {
		return "", err
	}
	m, err := readRepairJournalManifest(dir)
	if err != nil {
		return requested, nil
	}
	settled, err := sourceIsSettled(dir, m)
	if err != nil || !settled {
		return requested, err
	}
	for {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return "", err
		}
		candidate := requested + ":attempt:" + hex.EncodeToString(nonce[:])
		dir, err := repairJournalDir(run, candidate)
		if err != nil {
			return "", err
		}
		if _, err := os.Lstat(dir); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
}
