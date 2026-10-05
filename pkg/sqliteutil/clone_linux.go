//go:build linux

package sqliteutil

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func cloneFile(source, target string) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	err = unix.IoctlFileClone(int(dst.Fd()), int(src.Fd()))
	return errors.Join(err, dst.Close())
}
