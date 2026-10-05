//go:build !darwin && !linux

package sqliteutil

func filesystemType(string) (string, error) {
	return "", nil
}
