//go:build !windows

package sourcetxn

import (
	"fmt"
	"os"
	"syscall"
)

func fileHasMultipleLinks(_ string, info os.FileInfo) (bool, error) {
	count, err := fileLinkCount("", info)
	return count > 1, err
}

func fileLinkCount(_ string, info os.FileInfo) (uint64, error) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("file link count is unavailable")
	}
	return uint64(s.Nlink), nil
}
