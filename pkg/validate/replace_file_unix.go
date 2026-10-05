//go:build !windows

package validate

import (
	"os"
	"path/filepath"
)

func replaceFile(source, destination string) error {
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(destination))
}
