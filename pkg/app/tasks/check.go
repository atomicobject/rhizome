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

	"github.com/atomicobject/rhizome/pkg/app/procrun"
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
	Test         bool   `json:"test,omitempty"`
}

func (l Locations) env() []string {
	env := []string{
		"RZM_TASK_ID=" + l.TaskID,
		"RZM_TASK_MAIN_CHECKOUT=" + l.MainCheckout,
		"RZM_TASK_COMMIT=" + l.Commit,
		"RZM_TASK_DATA_DIR=" + l.DataDir,
	}
	if l.Test {
		env = append(env, "RZM_TASK_TEST=1")
	}
	return env
}

// taskPath is the login PATH with this runtime's own rzm first. The runtime
// runs with RZM_SKIP_REPO_DELEGATE, which task subprocesses inherit, so a
// bare `rzm` would otherwise run whatever rzm is first on PATH instead of the
// repository's binary that this runtime already resolved.
func (s *Service) taskPath() string {
	return filepath.Dir(s.opts.Rzm) + string(os.PathListSeparator) + procrun.LoginPath()
}

// agentEnv is the environment a task's agent adds to its own.
func (s *Service) agentEnv(loc Locations) []string {
	return append(loc.env(), "PATH="+s.taskPath())
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
		if loc.Test {
			if err := requireTestIndex(loc.MainCheckout); err != nil {
				return CheckResult{}, err
			}
		}
		if cmd, err = procrun.CodeModeCommand(s.opts.Rzm, loc.MainCheckout, def.Limit, loc, code); err != nil {
			return CheckResult{}, err
		}
	} else {
		dir := draftRoot
		if dir == "" {
			var err error
			if dir, err = s.prepareCheckout(ctx, loc.Commit); err != nil {
				return CheckResult{}, fmt.Errorf("prepare check checkout: %w", err)
			}
		}
		name := s.opts.Rzm
		if def.Check.Command[0] != "rzm" {
			var err error
			if name, err = procrun.LookLoginPath(def.Check.Command[0]); err != nil {
				return CheckResult{}, fmt.Errorf("check %w", err)
			}
		}
		cmd = exec.Command(name, def.Check.Command[1:]...)
		cmd.Dir = dir
	}
	cmd.Env = append(procrun.WithPath(os.Environ(), s.taskPath()), loc.env()...)
	if loc.Test && def.Check.Script != "" {
		cmd.Env = append(cmd.Env, "RZM_RUNTIME_AUTOSTART=0")
	}
	out, err := runBounded(ctx, cmd, log)
	if def.Check.Script == "" && err != nil {
		return CheckResult{}, err
	}
	if def.Check.Script != "" {
		// A failed script still prints code mode's result, which holds the
		// script's own error.
		if err != nil && (len(out) == 0 || ctx.Err() != nil) {
			return CheckResult{}, err
		}
		result, ok, message, err := procrun.CodeModeResult(out)
		if err != nil {
			return CheckResult{}, fmt.Errorf("check script output is not JSON: %w", err)
		}
		if !ok {
			msg := "check script failed"
			if message != "" {
				msg += ": " + message
			}
			return CheckResult{}, errors.New(msg)
		}
		out = result
	}
	// needed must be explicit, so a check that prints {} or null fails
	// instead of quietly skipping the task.
	var result struct {
		Needed  *bool  `json:"needed"`
		Summary string `json:"summary"`
		Context string `json:"context"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil || result.Needed == nil {
		return CheckResult{}, fmt.Errorf("check must print one JSON object with a boolean \"needed\", like {\"needed\": true}; got %.200q", out)
	}
	if dec.More() {
		return CheckResult{}, errors.New("check printed more than one JSON value")
	}
	return CheckResult{Needed: *result.Needed, Summary: result.Summary, Context: result.Context}, nil
}

// runBounded runs a check through procrun.Run and labels its errors as the
// check's. stdin stays closed.
func runBounded(ctx context.Context, cmd *exec.Cmd, log io.Writer) ([]byte, error) {
	out, err := procrun.Run(ctx, cmd, log, maxCheckOutput)
	switch {
	case err == nil:
		return out, nil
	case cmd.Process == nil:
		return nil, err
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		return nil, fmt.Errorf("check stopped: %w", err)
	default:
		return out, fmt.Errorf("check exited: %w", err)
	}
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
