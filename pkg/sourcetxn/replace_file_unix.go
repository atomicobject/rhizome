//go:build !windows

package sourcetxn

import (
	"os"
	"path/filepath"
)

func replaceFile(source, destination string) error {
	_, err := replaceFileOutcome(source, destination, nil)
	return err
}

func replaceFileOutcome(source, destination string, beforeSync func() error) (bool, error) {
	if err := os.Rename(source, destination); err != nil {
		return false, err
	}
	if beforeSync != nil {
		if err := beforeSync(); err != nil {
			return true, err
		}
	}
	return true, syncDirectory(filepath.Dir(destination))
}
