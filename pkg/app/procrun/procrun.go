// Package procrun runs a repository's own commands for scheduled task checks
// ([[scheduled-tasks|SPEC-0127]]) and module actions
// ([[module-actions|SPEC-0131]]): in their own process tree, with the login
// shell's PATH, bounded stdout, and every descendant killed when the command
// ends or its context does. It also builds and reads `rzm agent code execute`
// invocations for code-mode scripts.
package procrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/proctree"
)

// maxCodeModeTimeout is the longest deadline `agent code execute` accepts;
// the caller's own deadline still applies through its context.
const maxCodeModeTimeout = 24 * time.Hour

// ErrOutputTooLarge is the stdout write error once a command passes its limit.
var ErrOutputTooLarge = errors.New("output is too large")

// Run runs cmd in its own process group, returns at most maxStdout bytes of
// stdout, and writes stderr to stderr. Every process the command started is
// killed when it ends or ctx does, and a descendant left holding the output
// pipes cannot keep Wait from returning. A start failure and the command's
// exit error are returned as they are; when ctx ends first, Run returns no
// output and ctx's error. Stdin is whatever cmd.Stdin holds, nil meaning
// closed.
func Run(ctx context.Context, cmd *exec.Cmd, stderr io.Writer, maxStdout int) ([]byte, error) {
	var stdout bytes.Buffer
	cmd.Stdout = &limitWriter{w: &stdout, n: maxStdout}
	cmd.Stderr = stderr
	cmd.WaitDelay = 2 * time.Second
	proctree.Configure(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	tree := proctree.Track(cmd.Process, time.Second)
	defer tree.Stop()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		_ = tree.Kill()
		// The command exited cleanly but something it started held the pipes.
		if errors.Is(err, exec.ErrWaitDelay) {
			err = nil
		}
		return stdout.Bytes(), err
	case <-ctx.Done():
		_ = tree.Kill()
		<-done
		return nil, ctx.Err()
	}
}

type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if len(p) > l.n {
		return 0, ErrOutputTooLarge
	}
	l.n -= len(p)
	return l.w.Write(p)
}

var (
	loginPathOnce  sync.Once
	loginPathValue string
)

// LoginPath is PATH as the person's login shell sets it. A runtime started
// by the desktop app or launchd has a bare PATH, and version-manager shims
// fail without their environment.
func LoginPath() string {
	loginPathOnce.Do(func() {
		loginPathValue = os.Getenv("PATH")
		if runtime.GOOS == "windows" {
			return
		}
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
		out, err := exec.CommandContext(ctx, shell, "-lc", `printf '`+marker+`%s' "$PATH"`).Output()
		if i := strings.LastIndex(string(out), marker); err == nil && i >= 0 {
			if path := strings.TrimSpace(string(out[i+len(marker):])); path != "" {
				loginPathValue = path
			}
		}
	})
	return loginPathValue
}

// LookLoginPath finds a command the way the login shell would. Names with a
// path separator are left for exec to resolve against the command's Dir.
func LookLoginPath(name string) (string, error) {
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		return name, nil
	}
	if runtime.GOOS == "windows" {
		// The login PATH is the process PATH there, and LookPath adds PATHEXT.
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("command %q is not on PATH", name)
	}
	for _, dir := range filepath.SplitList(LoginPath()) {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("command %q is not on the login shell's PATH", name)
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

// CodeModeCommand runs code through `rzm agent code execute` in dir with
// input as the script's input. Code mode has its own default deadline, so
// the command passes timeout, capped at what code mode accepts.
func CodeModeCommand(rzm, dir string, timeout time.Duration, input any, code []byte) (*exec.Cmd, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(rzm, "agent", "code", "execute", "--timeout", min(timeout, maxCodeModeTimeout).String(), "--input", string(payload))
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(code)
	return cmd, nil
}

// CodeModeResult reads what `rzm agent code execute` printed. ok reports
// whether the script returned; when it threw, message is code mode's error
// message, which may be empty. err means the output was not code mode's.
func CodeModeResult(out []byte) (result json.RawMessage, ok bool, message string, err error) {
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, false, "", err
	}
	if !envelope.OK {
		if envelope.Error != nil {
			message = envelope.Error.Message
		}
		return nil, false, message, nil
	}
	return envelope.Result, true, "", nil
}
