package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

const maxMessage = 8 << 10

// AgentRequest is one background prompt run.
type AgentRequest struct {
	// Task is the task's id, for its per-person harness settings.
	Task   string
	Env    []string
	Dir    string
	Prompt string
	// Deny names tools the harness must refuse (the task's deny list).
	Deny []string
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
	noHooks, err := s.noHooks(a)
	if err != nil {
		return s.fail(ctx, def, a, itemID, commit, err.Error(), 0)
	}
	branch, dir, err := s.createWorktree(ctx, def, commit, noHooks)
	if err != nil {
		return s.fail(ctx, def, a, itemID, commit, "create worktree: "+err.Error(), 0)
	}
	// The worktree's location must be saved before any work starts, or a
	// later pass could not find it.
	closed := false
	if err := s.store.Update(ctx, func(st *State) error {
		it, err := st.Item(itemID)
		if err != nil {
			return err
		}
		if it.State != StateRunning {
			// The person closed the item while the worktree was made.
			closed = true
			r := a.record(def.ID, OutcomeSkipped, s.opts.Now())
			r.Message, r.Item, r.Commit = "the item was closed before the prompt ran", itemID, commit
			st.Record(r)
			return nil
		}
		it.set(StateRunning, s.opts.Now())
		it.Branch, it.Worktree, it.Commit, it.Run, it.Landing = branch, dir, commit, a.id, def.Land
		running(st, a, func(r *RunRecord) {
			r.Stage, r.Item, r.Branch, r.Worktree, r.Commit, r.Landing = StageAgent, itemID, branch, dir, commit, def.Land
		})
		return nil
	}); err != nil {
		_ = s.removeWorktree(ctx, branch, dir)
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Item: itemID, Reason: "save the run's worktree: " + err.Error()}
	}
	if closed {
		_ = s.removeWorktree(ctx, branch, dir)
		return Outcome{Task: def.ID, Outcome: OutcomeSkipped, Item: itemID, Reason: "the item was closed before the prompt ran"}
	}
	loc := s.withResultFile(s.locations(def, commit), a) // SPEC-0127 US14
	if err := os.MkdirAll(loc.DataDir, 0o700); err != nil {
		return s.fail(ctx, def, a, itemID, commit, err.Error(), 0)
	}
	transcript := s.runFile(a.id, transcriptFile)
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		return s.fail(ctx, def, a, itemID, commit, err.Error(), 0)
	}
	runCtx, cancel := context.WithTimeout(ctx, def.Limit)
	res, err := s.opts.Agent.RunPrompt(runCtx, AgentRequest{Task: def.ID, Dir: dir, Deny: def.Deny, Prompt: BackgroundPrompt(def, loc, check, ref, branch, dir), Transcript: transcript, Env: s.agentEnv(loc)})
	cancel()
	a.result = afterPrompt(def, loc) // SPEC-0127 US14: trim the handoff note, read the result file
	if err != nil {
		return s.fail(ctx, def, a, itemID, commit, fmt.Sprintf("prompt run failed: %v (worktree kept at %s)", err, dir), res.Tokens)
	}
	message := strings.TrimSpace(res.Message)
	if len(message) > maxMessage {
		message = message[:maxMessage]
	}
	changed, committed, err := commitWork(ctx, def, dir, commit, message, a.slotID(), noHooks)
	if err != nil {
		return s.fail(ctx, def, a, itemID, commit, fmt.Sprintf("commit: %v (worktree kept at %s)", err, dir), res.Tokens)
	}
	outcome := OutcomeReady
	var cleanupErr error
	var pr, notice string
	if !committed {
		outcome = OutcomeNoChanges
		cleanupErr = s.removeWorktree(ctx, branch, dir)
	} else if s.pushAllowed() {
		pr, notice = s.pushPR(ctx, def, a, ref, branch, dir, message)
	}
	// A local task branch waits for the person's review; a pushed pull
	// request is the review surface itself, so a quiet or ok run's item
	// closes and its worktree goes (SPEC-0127 US14). The local branch stays,
	// so a later run that day takes a new name instead of the pull
	// request's branch.
	alert := notifiesFor(outcome, a.result.status(), pr == "") || cleanupErr != nil
	reviewed := outcome == OutcomeReady && !alert
	if reviewed {
		if err := s.removePushedWorktree(ctx, branch, dir, noHooks); err != nil {
			reviewed, alert = false, true
			notice = fmt.Sprintf("The pull request is open, but its local worktree could not be removed: %v.", err)
		}
	}
	now := s.opts.Now()
	if err := s.store.Update(ctx, func(st *State) error {
		// The person may have closed the item during the run; keep their
		// state, dropping only a worktree this run removed.
		if it, err := st.Item(itemID); err == nil && it.State == StateRunning {
			it.Message, it.Tokens, it.Changed, it.PR = message, res.Tokens, changed, pr
			switch {
			case reviewed:
				it.set(StateDone, now)
				it.Branch, it.Worktree = "", ""
			case outcome == OutcomeReady:
				it.set(StateReady, now)
				it.Reason = notice
			case cleanupErr != nil:
				// Keep the location so discard can retry the cleanup.
				it.set(StateAttention, now)
				it.Reason = "nothing changed, but the worktree could not be removed: " + cleanupErr.Error()
			case alert:
				it.set(StateAttention, now)
				it.Branch, it.Worktree = "", ""
			default:
				it.set(StateDone, now)
				it.Branch, it.Worktree = "", ""
			}
			if a.result.attention() && cleanupErr == nil {
				it.Reason = a.result.Summary
			}
		} else if err == nil && (!committed || reviewed) && cleanupErr == nil {
			it.Branch, it.Worktree = "", ""
		}
		r := a.record(def.ID, outcome, now)
		r.Tokens, r.Message, r.Item, r.Commit, r.Changed = res.Tokens, firstLine(message), itemID, commit, changed
		r.Landing, r.PR = def.Land, pr
		switch {
		case reviewed:
			r.Branch = branch // pushed; the worktree is gone
		case outcome == OutcomeReady || cleanupErr != nil:
			r.Branch, r.Worktree = branch, dir
		}
		st.Record(r)
		if alert {
			s.queueRunAlert(st, def.Name, a.id, notice) // SPEC-0127 US14
		}
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
	writeMemory(&b, def, loc)
	writeResultFile(&b, loc)
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
// and a worktree for it under the state folder, running no repository
// hooks.
func (s *Service) createWorktree(ctx context.Context, def Definition, commit, noHooks string) (branch, dir string, err error) {
	branch, err = s.freeBranch(ctx, def)
	if err != nil {
		return "", "", err
	}
	dir = filepath.Join(s.stateDir, "worktrees", worktreeName(def, branch))
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return "", "", err
	}
	if _, err := s.opts.Repo.git(ctx, "-c", noHooks, "worktree", "add", "--quiet", "-b", branch, dir, commit); err != nil {
		return "", "", err
	}
	return branch, dir, nil
}

// worktreeName names a run's worktree folder and Claude worktree: the task's
// path key and the branch's date suffix, so module task `a/b` and flat task
// `a-b` never share one.
func worktreeName(def Definition, branch string) string {
	return taskdef.PathKey(def.ID) + "-" + strings.TrimPrefix(branch, "rzm/"+def.ID+"/")
}

// freeBranch returns rzm/<task>/<YYYY-MM-DD>, with -2, -3, ... appended
// when a branch of that name exists here or on a remote-tracking ref, so a
// discarded run's pushed branch is not pushed over.
func (s *Service) freeBranch(ctx context.Context, def Definition) (string, error) {
	base := fmt.Sprintf("rzm/%s/%s", def.ID, s.opts.Now().Format("2006-01-02"))
	for n := 1; n < 100; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		if out, err := s.opts.Repo.git(ctx, "for-each-ref", "--count=1", "--format=%(refname)", "refs/heads/"+name, "refs/remotes/*/"+name); err == nil && out == "" {
			return name, nil
		}
	}
	return "", errors.New("no free branch name")
}

// removeWorktree removes a run's worktree and local branch. Ones already
// gone are not errors. It refuses anything Rhizome did not make (see
// removable) and a branch outside rzm/.
func (s *Service) removeWorktree(ctx context.Context, branch, dir string) error {
	if branch != "" && !strings.HasPrefix(branch, "rzm/") {
		return fmt.Errorf("%s is not a branch Rhizome made, so it was left alone", branch)
	}
	if dir != "" {
		if err := s.removable(ctx, branch, dir); err != nil {
			return err
		}
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

// removePushedWorktree removes a pushed run's worktree but keeps its
// branch, which the pull request uses. Like removeWorktree, it leaves a
// worktree that is no longer on the run's branch, and it also leaves one
// with uncommitted changes, since the person may be working there.
func (s *Service) removePushedWorktree(ctx context.Context, branch, dir, noHooks string) error {
	if err := s.removable(ctx, branch, dir); err != nil {
		return err
	}
	if _, err := os.Stat(dir); err == nil {
		if status, err := gitOutput(ctx, dir, "-c", noHooks, "status", "--porcelain"); err != nil {
			return err
		} else if status != "" {
			return fmt.Errorf("%s has uncommitted changes, so it was left alone", dir)
		}
	}
	return s.removeWorktree(ctx, "", dir)
}

// removable refuses to remove dir unless it is a run or test worktree
// folder under the state folder, is not and does not hold the main
// checkout, the host checkout, or the selected checkout, and, when it
// exists, Git lists it as a linked worktree on the run's branch (a test
// worktree may be on any). The open checkout and a worktree the person
// took over are never removed.
func (s *Service) removable(ctx context.Context, branch, dir string) error {
	dir = filepath.Clean(dir)
	refused := func(why string) error {
		return fmt.Errorf("%s %s, so it was left alone", dir, why)
	}
	if parent := filepath.Dir(dir); parent != filepath.Join(s.stateDir, "worktrees") && parent != filepath.Join(s.stateDir, "tests") {
		return refused("is not a worktree Rhizome made")
	}
	for _, keep := range []string{s.opts.Repo.MainCheckout, s.hostCheckout(), s.opts.Checkout} {
		if keep != "" && holds(dir, keep) {
			return refused("is or holds the checkout " + keep)
		}
	}
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	out, err := s.opts.Repo.git(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return err
	}
	for i, entry := range strings.Split(out, "\n\n") {
		lines := strings.Split(entry, "\n")
		path, ok := strings.CutPrefix(lines[0], "worktree ")
		if !ok || !samePath(path, dir) {
			continue
		}
		on := "detached"
		for _, line := range lines[1:] {
			if b, ok := strings.CutPrefix(line, "branch "); ok {
				on = b
			}
		}
		switch {
		case i == 0:
			return refused("is the main worktree")
		case branch != "" && on != "refs/heads/"+branch:
			return refused("is on " + strings.TrimPrefix(on, "refs/heads/") + ", not the run's branch " + branch)
		}
		return nil
	}
	return refused("is not a Git worktree of this repository")
}

// holds reports whether path is dir or inside it.
func holds(dir, path string) bool {
	rel, err := filepath.Rel(resolved(dir), resolved(path))
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func samePath(a, b string) bool { return resolved(a) == resolved(b) }

func resolved(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return filepath.Clean(path)
}

// commitWork commits every change in dir with the work's message, the task
// tag, and the slot line, so another machine skips the slot once the pull
// request merges. When the work committed everything itself, an empty
// commit carries them. It returns the files changed since base, and whether
// there was anything to keep: changes, or commits the work made even if
// they cancel out. Repository hooks do not run.
func commitWork(ctx context.Context, def Definition, dir, base, message, slot, noHooks string) ([]string, bool, error) {
	if _, err := gitOutput(ctx, dir, "-c", noHooks, "add", "-A"); err != nil {
		return nil, false, err
	}
	status, err := gitOutput(ctx, dir, "-c", noHooks, "status", "--porcelain")
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
	body := taskMessage(def, message, slot)
	args := []string{"-c", noHooks, "commit", "--quiet", "--no-verify", "-F", "-"}
	if status == "" {
		args = append(args, "--allow-empty")
	}
	if _, err := gitBytesInput(ctx, dir, nil, []byte(body), args...); err != nil {
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
