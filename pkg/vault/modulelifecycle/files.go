package modulelifecycle

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ReadRegularFile reads bounded runtime input without leaving the repository.
func ReadRegularFile(root, name string, limit int64) ([]byte, error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return readRegularFile(dir, name, limit)
}

func readRegularFile(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	return readCheckedFile(root, name, limit, info)
}

func readLinkedInputFile(ctx context.Context, root *os.Root, name string, limit int64) ([]byte, error) {
	for links := 0; links < 40; links++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !safePath(name) {
			return nil, fmt.Errorf("unsafe linked input target %q", name)
		}
		if err := noSymlinks(root.Name(), filepath.ToSlash(filepath.Dir(name))); err != nil {
			return nil, err
		}
		info, err := root.Lstat(name)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return readCheckedFile(root, name, limit, info)
		}
		target, err := root.Readlink(name)
		if err != nil {
			return nil, err
		}
		if filepath.IsAbs(target) {
			return nil, fmt.Errorf("linked input target must be relative: %s", name)
		}
		raw := filepath.ToSlash(filepath.Dir(name)) + "/" + filepath.ToSlash(target)
		if err := checkLinkTargetParents(root, raw); err != nil {
			return nil, err
		}
		name = filepath.ToSlash(filepath.Clean(raw))
	}
	return nil, fmt.Errorf("too many symbolic links in readiness input")
}

func checkLinkTargetParents(root *os.Root, target string) error {
	parts := strings.Split(target, "/")
	current := ""
	for _, part := range parts[:len(parts)-1] {
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		clean := filepath.ToSlash(filepath.Clean(current))
		if clean != "." && !safePath(clean) {
			return fmt.Errorf("unsafe linked input target %q", target)
		}
		info, err := root.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("linked input parent must be a directory, not a symlink or file: %s", current)
		}
	}
	return nil
}

func readCheckedFile(root *os.Root, name string, limit int64, info os.FileInfo) ([]byte, error) {
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file, not a symlink or special file", name)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", name, limit)
	}
	file, err := root.OpenFile(name, os.O_RDONLY|safeReadFlags, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("%s changed while opening", name)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf("%s exceeds %d bytes", name, limit)
	}
	return data, err
}
