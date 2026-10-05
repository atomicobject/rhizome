//go:build windows

package validate

import "os"

// syncDirectory is intentionally a no-op on Windows. Windows does not support
// flushing directory handles opened by os.Open; regular repair files are
// flushed before publication, and replaceFile uses MOVEFILE_WRITE_THROUGH.
func syncDirectory(path string) error {
	_, err := os.Lstat(path)
	return err
}
