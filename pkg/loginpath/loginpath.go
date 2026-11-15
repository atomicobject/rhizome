// Package loginpath finds commands the way the person's login shell would.
// A process started by the desktop app or launchd has a bare PATH, and
// version-manager shims fail without their environment.
package loginpath

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	once  sync.Once
	value string
)

// Path is PATH as the login shell sets it, followed by the well-known install
// directories missing from it, which covers a shell that sets PATH only in an
// interactive rc file. It is computed once per process.
// On Windows it is the process PATH.
func Path() string {
	once.Do(func() {
		value = os.Getenv("PATH")
		if runtime.GOOS == "windows" {
			return
		}
		if path := shellPath(); path != "" {
			value = path
		}
		value = withFallbacks(value)
	})
	return value
}

func shellPath() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
		if _, err := os.Stat("/bin/zsh"); err == nil && runtime.GOOS == "darwin" {
			shell = "/bin/zsh"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// A profile may print to stdout; the marker finds PATH after it.
	const marker = "__RZM_LOGIN_PATH__"
	cmd := exec.CommandContext(ctx, shell, "-lc", `printf '`+marker+`%s' "$PATH"`)
	// A profile may leave a background child holding stdout; stop waiting
	// for it once the shell is gone and keep what the shell printed.
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	i := strings.LastIndex(string(out), marker)
	if err != nil || i < 0 {
		return ""
	}
	return strings.TrimSpace(string(out[i+len(marker):]))
}

func withFallbacks(path string) string {
	dirs := filepath.SplitList(path)
	var fallbacks []string
	if runtime.GOOS == "darwin" {
		fallbacks = append(fallbacks, "/opt/homebrew/bin", "/usr/local/bin")
	}
	if home, err := os.UserHomeDir(); err == nil {
		fallbacks = append(fallbacks, filepath.Join(home, ".local", "bin"))
	}
	for _, dir := range fallbacks {
		// Listed even before they exist, so a CLI installed into a new
		// ~/.local/bin is found without restarting the runtime.
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

// Look finds a command on Path. Names with a path separator are left for exec
// to resolve against the command's Dir.
func Look(name string) (string, error) {
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		return name, nil
	}
	if runtime.GOOS == "windows" {
		// Path is the process PATH there, and LookPath adds PATHEXT.
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("command %q is not on PATH", name)
	}
	for _, dir := range filepath.SplitList(Path()) {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("command %q is not on the login shell's PATH", name)
}

// Extend returns path followed by the entries of Path it lacks, so an explicit
// PATH keeps priority and a bare one still reaches installed tools.
func Extend(path string) string {
	dirs := filepath.SplitList(path)
	for _, dir := range filepath.SplitList(Path()) {
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

// WithPath returns env with PATH replaced by path.
func WithPath(env []string, path string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "PATH=") {
			out = append(out, kv)
		}
	}
	return append(out, "PATH="+path)
}
