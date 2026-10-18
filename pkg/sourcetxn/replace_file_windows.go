//go:build windows

package sourcetxn

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func replaceFile(source, destination string) error {
	_, err := replaceFileOutcome(source, destination, nil)
	return err
}

func replaceFileOutcome(source, destination string, beforeSync func() error) (bool, error) {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return false, err
	}
	destinationPtr, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return false, err
	}
	if err := windows.MoveFileEx(
		sourcePtr,
		destinationPtr,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	); err != nil {
		return false, err
	}
	if beforeSync != nil {
		if err := beforeSync(); err != nil {
			return true, err
		}
	}
	return true, syncDirectory(filepath.Dir(destination))
}
