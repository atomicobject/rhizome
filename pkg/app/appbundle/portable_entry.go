package appbundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/atomicobject/rhizome/pkg/fileio"
)

var entrySearchPath = setEntrySearchPath

// ImageRoot recognizes the lexical immutable image layout, including a missing image.
func ImageRoot(path string) string {
	root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	if portableRoot(root) && filepath.Base(filepath.Dir(filepath.Dir(path))) == "images" &&
		validHash(filepath.Base(filepath.Dir(path))) && filepath.Base(path) == executableName() {
		return root
	}
	return ""
}

// EntryState never claims a foreign executable or symlink.
func EntryState(path string, bundle Info) (string, string, error) {
	stat, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "absent", "", nil
	}
	if err != nil {
		return "", "", err
	}
	if runtime.GOOS == "windows" {
		if !stat.Mode().IsRegular() {
			return "foreign", "", nil
		}
		hash, err := hashFile(path)
		if err != nil {
			return "", "", err
		}
		if _, err := os.Stat(ImagePath(bundle.Root, hash)); err != nil {
			return "foreign", "", nil
		}
		record, err := ReadInstallation(bundle.Root)
		if err != nil {
			return "", "", err
		}
		return "installed", ImagePath(bundle.Root, record.ImageHash), nil
	}
	if stat.Mode()&os.ModeSymlink == 0 {
		return "foreign", "", nil
	}
	target, err := os.Readlink(path)
	if err != nil {
		return "", "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	if ImageRoot(target) == "" {
		return "foreign", "", nil
	}
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return "dangling", target, nil
	} else if err != nil {
		return "", "", err
	}
	record, err := ReadInstallation(bundle.Root)
	if err != nil {
		return "", "", err
	}
	if SamePath(target, ImagePath(bundle.Root, record.ImageHash)) {
		return "installed", target, nil
	}
	return "other-bundle", target, nil
}

// InstallEntry changes only the owned entry, retaining a Windows dispatcher
// already in use. The installation's current image is selected on each launch.
func InstallEntry(ctx context.Context, bundle Info, path string) error {
	return withPortableEntryLock(ctx, bundle.Root, func() error {
		record, err := ReadInstallation(bundle.Root)
		if err != nil {
			return err
		}
		source := ImagePath(bundle.Root, record.ImageHash)
		if err := verifyImage(source, record.ImageHash); err != nil {
			return err
		}
		state, _, err := EntryState(path, bundle)
		if err != nil {
			return err
		}
		if state == "foreign" {
			return fmt.Errorf("%s belongs to another installation and was left in place", path)
		}
		if runtime.GOOS == "windows" && state == "installed" {
			return entrySearchPath(filepath.Dir(path), true)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		staged, err := os.CreateTemp(filepath.Dir(path), ".rzm-entry-*")
		if err != nil {
			return err
		}
		temporary := staged.Name()
		if err := staged.Close(); err != nil {
			return err
		}
		defer os.Remove(temporary)
		if runtime.GOOS == "windows" {
			if err := copyExecutable(source, temporary); err != nil {
				return err
			}
		} else {
			if err := os.Remove(temporary); err != nil {
				return err
			}
			if err := os.Symlink(source, temporary); err != nil {
				return err
			}
		}
		if err := fileio.Replace(temporary, path); err != nil {
			return err
		}
		return entrySearchPath(filepath.Dir(path), true)
	})
}

func RemoveEntry(ctx context.Context, bundle Info, path string) error {
	return RemoveEntryExpected(ctx, bundle, path, 0)
}

// RemoveEntryExpected rejects an obsolete installation before touching its CLI entry.
func RemoveEntryExpected(ctx context.Context, bundle Info, path string, generation uint64) error {
	return withPortableEntryLock(ctx, bundle.Root, func() error {
		if generation != 0 {
			record, err := ReadInstallation(bundle.Root)
			if err != nil {
				return err
			}
			if record.Generation != generation || !SamePath(record.AppPath, bundle.AppPath) {
				return errors.New("the desktop installation changed; refresh before removing its integration")
			}
		}
		state, _, err := EntryState(path, bundle)
		if err != nil {
			return err
		}
		if state == "foreign" {
			return fmt.Errorf("%s belongs to another installation and was left in place", path)
		}
		if state != "absent" {
			if err := removeEntryFile(path); err != nil {
				return err
			}
		}
		return entrySearchPath(filepath.Dir(path), false)
	})
}

func copyExecutable(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// DispatchEntry validates the public CLI entry before dotenv loading. Direct
// immutable images remain available to independently owned runtime children.
func DispatchEntry(args []string, stdin io.Reader, stdout, stderr io.Writer) (bool, int) {
	if runtime.GOOS == "darwin" {
		return false, 0
	}
	self, err := os.Executable()
	if err != nil {
		return false, 0
	}
	entry, err := EntryPath()
	if err != nil {
		return false, 0
	}
	return dispatchEntry(self, os.Args[0], entry, args, stdin, stdout, stderr)
}

func dispatchEntry(self, invoked, entry string, args []string, stdin io.Reader, stdout, stderr io.Writer) (bool, int) {
	fail := func(err error) (bool, int) {
		fmt.Fprintln(stderr, err)
		return true, 1
	}
	if runtime.GOOS == "windows" && SamePath(self, entry) {
		root, err := PortableRoot(ReleaseIdentifier)
		if err != nil {
			return fail(err)
		}
		record, err := ReadInstallation(root)
		if err != nil || !record.Enabled {
			return fail(errors.New("Rhizome desktop integration is unavailable; open or reinstall the app"))
		}
		bundle, err := readPortable(root)
		if err != nil {
			return fail(err)
		}
		if state, _, err := EntryState(entry, bundle); err != nil || state != "installed" {
			return fail(errors.New("the command-line entry is not owned by the registered app"))
		}
		if err := verifyImage(bundle.ExecutablePath, record.ImageHash); err != nil {
			return fail(err)
		}
		command := exec.Command(bundle.ExecutablePath, args...)
		command.Stdin, command.Stdout, command.Stderr = stdin, stdout, stderr
		err = runEntryChild(command)
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			fmt.Fprintln(stderr, "Cannot start the installed Rhizome executable:", err)
		}
		return true, childExitCode(err)
	}
	if runtime.GOOS != "windows" {
		if root := invokedImageEntryRoot(self, invoked, entry); root != "" {
			record, err := ReadInstallation(root)
			if err != nil || !record.Enabled {
				return fail(errors.New("Rhizome desktop integration is unavailable; open or reinstall the app"))
			}
			if _, err := readPortable(root); err != nil {
				return fail(err)
			}
		}
	}
	return false, 0
}

// Linux resolves os.Executable through the CLI symlink. Follow original argv[0]
// aliases to the shared entry, leaving links directly to an image independent.
func invokedImageEntryRoot(self, invoked, entry string) string {
	if invoked == "" || entry == "" {
		return ""
	}
	if !strings.ContainsRune(invoked, filepath.Separator) {
		path, err := exec.LookPath(invoked)
		if err != nil && !errors.Is(err, exec.ErrDot) {
			return ""
		}
		invoked = path
	}
	invoked, err := filepath.Abs(invoked)
	if err != nil {
		return ""
	}
	entryDir, err := canonicalDirectory(filepath.Dir(entry))
	if err != nil {
		return ""
	}
	entry = filepath.Join(entryDir, filepath.Base(entry))
	matched := false
	for hops := 0; hops < 40; hops++ {
		invokedDir, err := canonicalDirectory(filepath.Dir(invoked))
		if err != nil {
			return ""
		}
		invoked = filepath.Join(invokedDir, filepath.Base(invoked))
		if SamePath(invoked, entry) {
			matched = true
			break
		}
		target, err := os.Readlink(invoked)
		if err != nil {
			return ""
		}
		invoked = target
		if !filepath.IsAbs(invoked) {
			invoked = filepath.Join(invokedDir, invoked)
		}
	}
	if !matched {
		return ""
	}
	target, err := os.Readlink(entry)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(entry), target)
	}
	root := ImageRoot(filepath.Clean(target))
	if root == "" {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(self)
	if err != nil {
		return ""
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil || !SamePath(resolved, target) {
		return ""
	}
	return root
}

func childExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() >= 0 {
		return exit.ExitCode()
	}
	return 1
}
