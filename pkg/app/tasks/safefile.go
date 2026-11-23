package tasks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// openRegular opens a file an agent could have tampered with, such as a
// run's result file or a task's handoff note: it must be a regular file,
// not a symlink, FIFO, device, or socket, in a real folder rather than one
// reached through a symlink. On Unix the open neither follows a final
// symlink nor blocks on a FIFO, so a swap after the check is refused too.
func openRegular(path string, flag int, perm os.FileMode) (*os.File, error) {
	if err := realDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil && !info.Mode().IsRegular():
		return nil, notRegular(path)
	case err != nil && !(flag&os.O_CREATE != 0 && errors.Is(err, os.ErrNotExist)):
		return nil, err
	}
	f, err := os.OpenFile(path, flag|safeOpenFlags, perm)
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		if err == nil {
			err = notRegular(path)
		}
		return nil, err
	}
	return f, nil
}

// realDir fails unless dir is a folder itself, not a symlink to one.
func realDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a folder", dir)
	}
	return nil
}

func notRegular(path string) error {
	return fmt.Errorf("%s is not a regular file", filepath.Base(path))
}
