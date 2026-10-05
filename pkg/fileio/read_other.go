//go:build !windows

package fileio

import "os"

// OpenRead opens a file without blocking cooperative replacement or removal.
func OpenRead(path string) (*os.File, error) {
	return os.Open(path)
}
