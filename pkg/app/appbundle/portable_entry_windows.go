package appbundle

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/hiddencommand"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var getEntryConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")

// EntryPath is stable across app versions; its copied rzm only dispatches.
func EntryPath() (string, error) {
	base, err := portableBaseDirectory()
	if err != nil {
		return "", err
	}
	base, err = canonicalDirectory(base)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "bin", "rzm.exe"), nil
}

func runEntryChild(command *exec.Cmd) error {
	console, _, _ := getEntryConsoleWindow.Call()
	if console == 0 {
		hiddencommand.Configure(command)
	}
	// The console delivers Ctrl-C to both processes. Keep the dispatcher alive
	// while the child handles it, without forwarding a second interrupt.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	return command.Run()
}

func removeEntryFile(path string) error {
	retired, err := os.CreateTemp(filepath.Dir(path), ".rzm-entry-retired-*.exe")
	if err != nil {
		return err
	}
	// Retire the public name first; unlinking a running Windows image may fail.
	defer os.Remove(retired.Name())
	if err := retired.Close(); err != nil {
		return err
	}
	return fileio.Replace(path, retired.Name())
}

func setEntrySearchPath(directory string, install bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	current, kind, err := key.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	next := entrySearchPathValue(current, directory, install)
	if next == current {
		return nil
	}
	if kind == registry.EXPAND_SZ {
		err = key.SetExpandStringValue("Path", next)
	} else {
		err = key.SetStringValue("Path", next)
	}
	if err != nil {
		return err
	}
	text, _ := windows.UTF16PtrFromString("Environment")
	var resultCode uintptr
	_, _, _ = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(
		0xffff, 0x1a, 0, uintptr(unsafe.Pointer(text)), 0x2, 5000, uintptr(unsafe.Pointer(&resultCode)))
	return nil
}

func entrySearchPathValue(current, directory string, install bool) string {
	var parts []string
	if current != "" {
		parts = strings.Split(current, ";")
	}
	found := false
	result := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		if SamePath(part, directory) {
			found = true
			if !install {
				continue
			}
		}
		result = append(result, part)
	}
	if install && !found {
		result = append(result, directory)
	}
	return strings.Join(result, ";")
}
