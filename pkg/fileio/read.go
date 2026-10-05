// Package fileio owns cooperative file reads and atomic namespace replacement.
package fileio

import "io"

// ReadFile reads the complete file through OpenRead, so concurrent readers do
// not prevent its publisher from replacing or removing it on Windows.
func ReadFile(path string) ([]byte, error) {
	file, err := OpenRead(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}
