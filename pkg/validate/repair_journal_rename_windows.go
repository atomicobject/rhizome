//go:build windows

package validate

import "golang.org/x/sys/windows"

func publishRepairJournalManifest(source, destination string) error {
	return moveRepairJournalMetadata(source, destination, true)
}

func detachRepairJournal(source, destination string) error {
	return moveRepairJournalMetadata(source, destination, false)
}

func moveRepairJournalMetadata(source, destination string, replace bool) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPtr, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	return windows.MoveFileEx(sourcePtr, destinationPtr, flags)
}
