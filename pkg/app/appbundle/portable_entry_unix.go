//go:build !windows

package appbundle

import (
	"os"
	"os/exec"
	"path/filepath"
)

// EntryPath is the real account's stable link to the current immutable image.
func EntryPath() (string, error) {
	home, err := nativeAccountHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin", "rzm"), nil
}

func setEntrySearchPath(string, bool) error { return nil }

func runEntryChild(command *exec.Cmd) error { return command.Run() }

func removeEntryFile(path string) error { return os.Remove(path) }
