//go:build !windows

package fileio

import "os"

// Replace atomically renames src over dst, retaining the old target on failure.
func Replace(src, dst string) error {
	return os.Rename(src, dst)
}
