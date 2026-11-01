package tasks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxMessage = 8 << 10

// AgentRequest is one background prompt run.
type AgentRequest struct {
	Dir    string
	Prompt string
	// Transcript is the run's events.jsonl path; empty means no capture.
	Transcript string
}

// AgentResult is what a prompt run reports.
type AgentResult struct {
	Message string
	Tokens  int64
}

// AgentRunner runs a prompt unattended in a directory, in the person's
// harness with full access. It must stop everything it started when ctx
// ends.
type AgentRunner interface {
	RunPrompt(ctx context.Context, req AgentRequest) (AgentResult, error)
}

// work runs a task's prompt in a new worktree and commits what it changed.
// The item must already exist. The harness transcript goes to the run's
// events.jsonl.
func (s *Service) work(ctx context.Context, def Definition, a attempt, itemID string, check CheckResult, ref, commit string) Outcome {
	if s.opts.Agent == nil {
		return s.fail(ctx, def, a, itemID, commit, "no agent harness is available to run the prompt", 0)
	}
	branch, dir, err := s.createWorktree(ctx, def, commit)
	if err != nil {
		return s.fail(ctx, def, a, itemID, commit, "create worktree: "+err.Error(), 0)
	}
	// The worktree's location must be saved before any work starts, or a
	// later pass could not find it.
	if err := s.store.Update(ctx, func(st *State) error {
		it, err := st.Item(itemID)
		if err != nil {
			return err
		}
		it.set(StateRunning, s.opts.Now())
		it.Branch, it.Worktree, it.Commit, it.Run = branch, dir, commit, a.id
		running(st, a, func(r *RunRecord) {
			r.Stage, r.Item, r.Branch, r.Worktree, r.Commit = StageAgent, itemID, branch, dir, commit
		})
		return nil
	}); err != nil {
		_ = s.removeWorktree(ctx, branch, dir)
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Item: itemID, Reason: "save the run's worktree: " + err.Error()}
	}
	loc := s.locations(def, commit)
	if err := os.MkdirAll(loc.DataDir, 0o700); err != nil {
		return s.fail(ctx, def, a, itemID, commit, err.Error(), 0)
	}
	transcript := s.runFile(a.id, transcriptFile)
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		return s.fail(ctx, def, a, itemID, commit, err.Error(), 0)
	}
	runCtx, cancel := context.WithTimeout(ctx, def.Limit)
	res, err := s.opts.Agent.RunPrompt(runCtx, AgentRequest{Dir: dir, Prompt: BackgroundPrompt(def, loc, check, ref, branch, dir), Transcript: transcript})
	cancel()
	if err != nil {
		return s.fail(ctx, def, a, itemID, commit, fmt.Sprintf("prompt run failed: %v (worktree kept at %s)", err, dir), res.Tokens)
	}
	message := strings.TrimSpace(res.Message)
	if len(message) > maxMessage {
		message = message[:maxMessage]
	}
	changed, committed, err := commitWork(ctx, def, dir, commit, message)
	if err != nil {
		return s.fail(ctx, def, a, itemID, commit, fmt.Sprintf("commit: %v (worktree kept at %s)", err, dir), res.Tokens)
	}
	now := s.opts.Now()
	outcome := OutcomeReady
	var cleanupErr error
	if !committed {
		outcome = OutcomeNoChanges
		cleanupErr = s.removeWorktree(ctx, branch, dir)
	}
	if err := s.store.Update(ctx, func(st *State) error {
		if it, err := st.Item(itemID); err == nil {
			it.Message, it.Tokens, it.Changed = message, res.Tokens, changed
			switch {
			case outcome == OutcomeReady:
				it.set(StateReady, now)
			case cleanupErr != nil:
				// Keep the location so discard can retry the cleanup.
				it.set(StateAttention, now)
				it.Reason = "nothing changed, but the worktree could not be removed: " + cleanupErr.Error()
			default:
				it.set(StateDone, now)
				it.Branch, it.Worktree = "", ""
			}
		}
		if a.draft {
			return nil
		}
		r := a.record(def.ID, outcome, now)
		r.Tokens, r.Message, r.Item, r.Commit, r.Changed = res.Tokens, firstLine(message), itemID, commit, changed
		if outcome == OutcomeReady || cleanupErr != nil {
			r.Branch, r.Worktree = branch, dir
		}
		st.Record(r)
		return nil
	}); err != nil {
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Item: itemID, Reason: fmt.Sprintf("the run finished (%s, worktree at %s) but its result could not be saved: %v", outcome, dir, err)}
	}
	return Outcome{Task: def.ID, Outcome: outcome, Item: itemID}
}

// BackgroundPrompt is the preamble plus the task's prompt for a background
// run.
func BackgroundPrompt(def Definition, loc Locations, check CheckResult, ref, branch, dir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are running the scheduled task %q (%s) in the background. No person is present to answer questions, so make reasonable choices and keep going.\n\n", def.Name, def.ID)
	fmt.Fprintf(&b, "Work only in this worktree: %s\nIt is on the branch %s, made from commit %s of %s.\n\n", dir, branch, loc.Commit, ref)
	writeLocations(&b, loc, check)
	b.WriteString("When you finish, Rhizome commits every change in this worktree with your final message and tags it with the task. Do not push, open a pull request, or change other worktrees or branches. End with a short message for the person: what you did and what they should review.\n\n")
	b.WriteString("Task instructions:\n\n")
	b.WriteString(strings.TrimSpace(def.Prompt))
	b.WriteString("\n")
	return b.String()
}

func writeLocations(b *strings.Builder, loc Locations, check CheckResult) {
	fmt.Fprintf(b, "Locations:\n- Main checkout: %s\n- This task's private data folder: %s\n\n", loc.MainCheckout, loc.DataDir)
	if check.Summary != "" || check.Context != "" {
		b.WriteString("The pre-flight check found:\n")
		if check.Summary != "" {
			b.WriteString(check.Summary + "\n")
		}
		if check.Context != "" {
			b.WriteString(check.Context + "\n")
		}
		b.WriteString("\n")
	}
}

// createWorktree makes the branch rzm/<task>/<date>, suffixed when taken,
// and a worktree for it under the state folder.
func (s *Service) createWorktree(ctx context.Context, def Definition, commit string) (branch, dir string, err error) {
	branch, err = s.freeBranch(ctx, def)
	if err != nil {
		return "", "", err
	}
	dir = filepath.Join(s.stateDir, "worktrees", strings.ReplaceAll(strings.TrimPrefix(branch, "rzm/"), "/", "-"))
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return "", "", err
	}
	if _, err := s.opts.Repo.git(ctx, "worktree", "add", "--quiet", "-b", branch, dir, commit); err != nil {
		return "", "", err
	}
	return branch, dir, nil
}

// freeBranch returns rzm/<task>/<YYYY-MM-DD>, with -2, -3, ... appended
// when a branch of that name exists.
func (s *Service) freeBranch(ctx context.Context, def Definition) (string, error) {
	base := fmt.Sprintf("rzm/%s/%s", def.ID, s.opts.Now().Format("2006-01-02"))
	for n := 1; n < 100; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		if _, err := s.opts.Repo.git(ctx, "show-ref", "--verify", "--quiet", "refs/heads/"+name); err != nil {
			return name, nil
		}
	}
	return "", errors.New("no free branch name")
}

// removeWorktree removes a run's worktree and local branch. Ones already
// gone are not errors.
func (s *Service) removeWorktree(ctx context.Context, branch, dir string) error {
	if dir != "" {
		if _, err := os.Stat(dir); err == nil {
			if _, err := s.opts.Repo.git(ctx, "worktree", "remove", "--force", dir); err != nil {
				return err
			}
		} else {
			_, _ = s.opts.Repo.git(ctx, "worktree", "prune")
		}
	}
	if branch != "" {
		if _, err := s.opts.Repo.git(ctx, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			if _, err := s.opts.Repo.git(ctx, "branch", "-D", branch); err != nil {
				return err
			}
		}
	}
	return nil
}

// commitWork commits every change in dir with the work's message and the
// task tag. When the work committed everything itself, an empty commit
// carries the tag. It returns the files changed since base, and whether
// there was anything to keep: changes, or commits the work made even if
// they cancel out.
func commitWork(ctx context.Context, def Definition, dir, base, message string) ([]string, bool, error) {
	if _, err := gitOutput(ctx, dir, "add", "-A"); err != nil {
		return nil, false, err
	}
	status, err := gitOutput(ctx, dir, "status", "--porcelain")
	if err != nil {
		return nil, false, err
	}
	ahead, err := gitOutput(ctx, dir, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return nil, false, err
	}
	if status == "" && ahead == "0" {
		return nil, false, nil
	}
	if message == "" {
		message = "The task's agent left no message."
	}
	body := fmt.Sprintf("chore(tasks): %s\n\n%s\n\n%s\n", truncate(def.Name, 60), message, def.Tag())
	args := []string{"commit", "--quiet", "-F", "-"}
	if status == "" {
		args = append(args, "--allow-empty")
	}
	if err := gitWithInput(ctx, dir, body, args...); err != nil {
		return nil, false, err
	}
	out, err := gitOutput(ctx, dir, "diff", "--name-only", base, "HEAD")
	if err != nil {
		return nil, false, err
	}
	if out == "" {
		return []string{}, true, nil
	}
	return strings.Split(out, "\n"), true, nil
}

func gitWithInput(ctx context.Context, dir, input string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n-1]) + "…"
}
