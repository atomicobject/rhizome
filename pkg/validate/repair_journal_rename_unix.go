//go:build !windows

package validate

import "os"

func publishRepairJournalManifest(source, destination string) error {
	return os.Rename(source, destination)
}

func detachRepairJournal(source, destination string) error {
	return os.Rename(source, destination)
}
