//go:build !windows

package sourcetxn

import "os"

func publishRepairJournalManifest(source, destination string) error {
	return os.Rename(source, destination)
}

func detachRepairJournal(source, destination string) error {
	return os.Rename(source, destination)
}
