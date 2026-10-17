//go:build !darwin && !linux

package sqliteutil

import (
	"errors"
	"fmt"
)

func cloneFile(string, string) error {
	return fmt.Errorf("copy-on-write clone: %w", errors.ErrUnsupported)
}
