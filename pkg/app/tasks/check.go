package tasks

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
)

const maxCheckOutput = 1 << 20

// CheckResult is what a pre-flight check prints.
type CheckResult struct {
	Needed  bool   `json:"needed"`
	Summary string `json:"summary,omitempty"`
	Context string `json:"context,omitempty"`
}

// Locations are given to a task's check and prompt alike, so work started
// later can read what the check stored.
type Locations struct {
	TaskID       string `json:"taskId"`
	MainCheckout string `json:"mainCheckout"`
	Commit       string `json:"commit"`
	DataDir      string `json:"dataDir"`
}

func (l Locations) env() []string {
	return []string{
		"RZM_TASK_ID=" + l.TaskID,
		"RZM_TASK_MAIN_CHECKOUT=" + l.MainCheckout,
		"RZM_TASK_COMMIT=" + l.Commit,
		"RZM_TASK_DATA_DIR=" + l.DataDir,
	}
}

// runCheck runs a task's check and parses its result. A command runs in the
// reset check checkout at the resolved commit; a code-mode script is read
// at that commit and runs against the main checkout's vault. For a draft,
// draftRoot is the checkout holding the draft, and both come from there.
func (s *Service) runCheck(ctx context.Context, def Definition, loc Locations, log io.Writer, draftRoot string) (CheckResult, error) {
	ctx, cancel := context.WithTimeout(ctx, def.Limit)
	defer cancel()
	if err := os.MkdirAll(loc.DataDir, 0o700); err != nil {
		return CheckResult{}, err
	}
	var cmd *exec.Cmd
	if def.Check.Script != "" {
		var code []byte
		var err error
		if draftRoot != "" {
			code, err = os.ReadFile(filepath.Join(draftRoot, def.Check.Script))
		} else {
			code, err = s.opts.Repo.ShowFile(ctx, loc.Commit, def.Check.Script)
		}
		if err != nil {
			return CheckResult{}, fmt.Errorf("read check script: %w", err)
		}
		input, _ := json.Marshal(loc)
		cmd = exec.Command(s.opts.Rzm, "agent", "code", "execute", "--input", string(input))
		cmd.Dir = loc.MainCheckout
		cmd.Stdin = bytes.NewReader(code)
	} else {
		dir := draftRoot
		if dir == "" {
			var err error
			if dir, err = s.prepareCheckout(ctx, loc.Commit); err != nil {
				return CheckResult{}, fmt.Errorf("prepare check checkout: %w", err)
			}
		}
		name, err := lookLoginPath(def.Check.Command[0])
		if err != nil {
			return CheckResult{}, err
		}
		cmd = exec.Command(name, def.Check.Command[1:]...)
		cmd.Dir = dir
	}
	cmd.Env = append(withPath(os.Environ(), loginPath()), loc.env()...)
	out, err := runBounded(ctx, cmd, log)
	if err != nil {
		return CheckResult{}, err
	}
	if def.Check.Script != "" {
		var envelope struct {
			OK     bool            `json:"ok"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(out, &envelope); err != nil {
			return CheckResult{}, fmt.Errorf("check script output is not JSON: %w", err)
		}
		if !envelope.OK {
			msg := "check script failed"
			if envelope.Error != nil {
				msg += ": " + envelope.Error.Message
			}
			return CheckResult{}, errors.New(msg)
		}
		out = envelope.Result
	}
	var result CheckResult
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return CheckResult{}, fmt.Errorf("check must print one JSON object like {\"needed\": true}: %w", err)
	}
	if dec.More() {
		return CheckResult{}, errors.New("check printed more than one JSON value")
	}
	return result, nil
}

// runBounded runs cmd in its own process group with stdin closed, returns
// at most maxCheckOutput bytes of stdout, writes stderr to log, and kills
// the whole tree when ctx ends.
func runBounded(ctx context.Context, cmd *exec.Cmd, log io.Writer) ([]byte, error) {
	var stdout bytes.Buffer
	cmd.Stdout = &limitWriter{w: &stdout, n: maxCheckOutput}
	cmd.Stderr = log
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return nil, fmt.Errorf("check exited: %w", err)
		}
		return stdout.Bytes(), nil
	case <-ctx.Done():
		killTree(cmd)
		<-done
		return nil, fmt.Errorf("check stopped: %w", ctx.Err())
	}
}

type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if len(p) > l.n {
		return 0, errors.New("check output is too large")
	}
	l.n -= len(p)
	return l.w.Write(p)
}

// prepareCheckout points the check checkout at commit, forcibly resetting
// and cleaning it, and creates it the first time.
func (s *Service) prepareCheckout(ctx context.Context, commit string) (string, error) {
	dir := filepath.Join(s.stateDir, "checkout")
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		if _, err := gitOutput(ctx, dir, "checkout", "--quiet", "--detach", "--force", commit); err == nil {
			if _, err := gitOutput(ctx, dir, "clean", "-ffdxq"); err == nil {
				return dir, nil
			}
		}
	}
	_ = os.RemoveAll(dir)
	_, _ = s.opts.Repo.git(ctx, "worktree", "prune")
	if _, err := s.opts.Repo.git(ctx, "worktree", "add", "--quiet", "--detach", "--force", dir, commit); err != nil {
		return "", err
	}
	return dir, nil
}

var (
	loginPathOnce  sync.Once
	loginPathValue string
)

// loginPath is PATH as the person's login shell sets it. A runtime started
// by the desktop app or launchd has a bare PATH, and version-manager shims
// fail without their environment.
func loginPath() string {
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

// lookLoginPath finds a command the way the login shell would. Names with a
// path separator are left for exec to resolve against the command's Dir.
func lookLoginPath(name string) (string, error) {
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		return name, nil
	}
	for _, dir := range filepath.SplitList(loginPath()) {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("check command %q is not on the login shell's PATH", name)
}

func withPath(env []string, path string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "PATH=") {
			out = append(out, kv)
		}
	}
	return append(out, "PATH="+path)
}
