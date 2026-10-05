//go:build darwin

package sqliteutil

import (
	"os"
	"path/filepath"
	"syscall"
)

func filesystemType(path string) (string, error) {
	probe := path
	for {
		if _, err := os.Stat(probe); err == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", os.ErrNotExist
		}
		probe = parent
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(probe, &stat); err != nil {
		return "", err
	}
	b := make([]byte, 0, len(stat.Fstypename))
	for _, c := range stat.Fstypename {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b), nil
}
