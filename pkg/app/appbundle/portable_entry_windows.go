package appbundle

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

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
	// The console delivers Ctrl-C to both processes. Keep the dispatcher alive
	// while the child handles it, without forwarding a second interrupt.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	return command.Run()
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
