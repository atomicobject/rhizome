//go:build integration && !windows

package integration

import "os"

func holdManifestReader(path string) (*os.File, error) {
	return os.Open(path)
}
