package logging

import (
	"fmt"
	"os"
	"path/filepath"
)

// TrimOutputTail repairs oversized inherited startup/crash output on the next
// open. It never follows a symlink and leaves absent files absent.
func TrimOutputTail(path string, maxBytes int64) error {
	if maxBytes <= 0 {
		return fmt.Errorf("raw output byte limit must be positive")
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("raw output directory is not a regular directory")
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("raw output is not a regular file")
	}
	if info.Size() <= maxBytes {
		return nil
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, current) {
		return fmt.Errorf("raw output changed while opening")
	}
	tail := make([]byte, int(maxBytes))
	if _, err := file.ReadAt(tail, current.Size()-maxBytes); err != nil {
		return err
	}
	marker := []byte("[earlier process output truncated]\n")
	if len(marker) < len(tail) {
		copy(tail, marker)
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	_, err = file.WriteAt(tail, 0)
	return err
}
