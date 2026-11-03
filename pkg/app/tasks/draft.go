package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RunDraft tests captured local source once, without changing scheduled state.
func (s *Service) RunDraft(ctx context.Context, file string) (Outcome, error) {
	root, path, err := s.testSource(ctx, file)
	if err != nil {
		return Outcome{}, err
	}
	if reason := s.untrusted(); reason != "" {
		return Outcome{}, errors.New(reason)
	}
	identity, err := ParseDefinition(path, []byte("name: Local test\nevery: 1d\nprompt: Test\n"))
	if err != nil {
		return Outcome{}, err
	}
	a := s.newAttempt(true)
	a.source = root
	release, err := s.lockRun(ctx, 0)
	if errors.Is(err, errLocked) {
		_ = s.store.Update(context.WithoutCancel(ctx), func(st *State) error {
			r := a.record(identity.ID, OutcomeFailed, s.opts.Now())
			r.Message = ErrBusy.Error()
			st.Record(r)
			return nil
		})
		return Outcome{}, ErrBusy
	}
	if err != nil {
		return Outcome{}, err
	}
	defer func() { _ = release() }()
	if err = s.finalizeInterrupted(ctx); err != nil {
		return Outcome{}, err
	}
	defer s.retain(ctx)
	if err = s.begin(ctx, a, a.record(identity.ID, "", time.Time{}), nil); err != nil {
		return Outcome{}, err
	}
	defer s.end(ctx, a)
	if err = s.clearTestWorktrees(ctx, identity.ID, root); err != nil {
		return s.finishTest(ctx, a, identity.ID, "", "", OutcomeFailed, "remove previous test worktree: "+err.Error(), 0)
	}
	snapshot, dir, err := s.captureTest(ctx, a, root)
	if dir != "" {
		if saveErr := s.store.Update(ctx, func(st *State) error {
			running(st, a, func(r *RunRecord) { r.Commit, r.Worktree = snapshot, dir })
			return nil
		}); saveErr != nil {
			_ = s.removeWorktree(context.WithoutCancel(ctx), "", dir)
			return Outcome{}, saveErr
		}
	}
	if err != nil {
		return s.finishTest(ctx, a, identity.ID, snapshot, dir, OutcomeFailed, "capture source: "+err.Error(), 0)
	}
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return s.finishTest(ctx, a, identity.ID, snapshot, dir, OutcomeFailed, "read captured task: "+err.Error(), 0)
	}
	def, err := ParseDefinition(path, data)
	if err != nil {
		return s.finishTest(ctx, a, identity.ID, snapshot, dir, OutcomeFailed, err.Error(), 0)
	}
	loc := Locations{TaskID: def.ID, MainCheckout: root, Commit: snapshot, DataDir: s.runFile(a.id, "data"), Test: true}
	if err = os.MkdirAll(loc.DataDir, 0o700); err != nil {
		return s.finishTest(ctx, a, def.ID, snapshot, dir, OutcomeFailed, err.Error(), 0)
	}
	check := CheckResult{Needed: true}
	if def.Check != nil {
		log := s.openCheckLog(a)
		check, err = s.runCheck(ctx, def, loc, log, dir)
		log.Close()
		if err != nil {
			return s.finishTest(ctx, a, def.ID, snapshot, dir, OutcomeFailed, "check failed: "+err.Error(), 0)
		}
	}
	if !check.Needed {
		return s.finishTest(ctx, a, def.ID, snapshot, dir, OutcomeNotNeeded, check.Summary, 0)
	}
	if strings.TrimSpace(def.Prompt) == "" {
		return s.finishTest(ctx, a, def.ID, snapshot, dir, OutcomeNotified, check.Summary, 0)
	}
	if s.opts.Agent == nil {
		return s.finishTest(ctx, a, def.ID, snapshot, dir, OutcomeFailed, "no agent harness is available to run the prompt", 0)
	}
	// Give the agent the same clean source baseline as the check, with shared data outside it.
	if _, err = gitOutput(ctx, dir, "-c", "core.hooksPath="+s.runFile(a.id, "hooks"), "reset", "--hard", snapshot); err == nil {
		_, err = gitOutput(ctx, dir, "clean", "-ffdxq")
	}
	if err != nil {
		return s.finishTest(ctx, a, def.ID, snapshot, dir, OutcomeFailed, "reset test worktree: "+err.Error(), 0)
	}
	if err = s.store.Update(ctx, func(st *State) error { running(st, a, func(r *RunRecord) { r.Stage = StageAgent }); return nil }); err != nil {
		return Outcome{}, err
	}
	prompt := testPrompt(def, loc, check, dir)
	runCtx, cancel := context.WithTimeout(ctx, def.Limit)
	res, runErr := s.opts.Agent.RunPrompt(runCtx, AgentRequest{Dir: dir, Prompt: prompt, Transcript: s.runFile(a.id, transcriptFile), Env: loc.env()})
	cancel()
	outcome := OutcomeReady
	message := strings.TrimSpace(res.Message)
	if len(message) > maxMessage {
		message = message[:maxMessage]
	}
	if runErr != nil {
		outcome, message = OutcomeFailed, "prompt run failed: "+runErr.Error()
	}
	return s.finishTest(ctx, a, def.ID, snapshot, dir, outcome, message, res.Tokens)
}

func testPrompt(def Definition, loc Locations, check CheckResult, dir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are testing the task %q (%s). No person is present; make reasonable choices and keep going.\n\nWork only in this detached test worktree: %s\nIts immutable source snapshot is %s.\n\n", def.Name, def.ID, dir, loc.Commit)
	writeLocations(&b, loc, check)
	b.WriteString("This is a local test with fresh private data. Rhizome records your changes for review without committing or scheduling them. Do not commit, push, open a pull request, or change other worktrees or branches. End with a short message describing the result.\n\nTask instructions:\n\n")
	b.WriteString(strings.TrimSpace(def.Prompt))
	b.WriteByte('\n')
	return b.String()
}

func (s *Service) finishTest(ctx context.Context, a attempt, task, snapshot, dir, outcome, message string, tokens int64) (Outcome, error) {
	ctx = context.WithoutCancel(ctx)
	var changed []string
	if dir != "" {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			dir = ""
		}
	}
	if dir != "" {
		_, err := gitOutput(ctx, dir, "-c", "core.hooksPath="+s.runFile(a.id, "hooks"), "add", "-A")
		var diff string
		if err == nil {
			diff, err = gitOutput(ctx, dir, "diff", "--name-only", "-z", snapshot)
		}
		if err != nil {
			outcome, message = OutcomeFailed, "read test changes: "+err.Error()
		} else if diff != "" {
			changed = strings.Split(strings.TrimSuffix(diff, "\x00"), "\x00")
		}
		if err == nil && len(changed) == 0 {
			if err := s.removeWorktree(ctx, "", dir); err == nil {
				dir = ""
			} else {
				outcome, message = OutcomeFailed, "remove test worktree: "+err.Error()
			}
			if outcome == OutcomeReady {
				outcome = OutcomeNoChanges
			}
		}
	}
	err := s.store.Update(ctx, func(st *State) error {
		r := a.record(task, outcome, s.opts.Now())
		r.Commit, r.Worktree, r.Message, r.Tokens, r.Changed = snapshot, dir, message, tokens, changed
		st.Record(r)
		return nil
	})
	return Outcome{Task: task, Outcome: outcome, Reason: message}, err
}

func (s *Service) clearTestWorktrees(ctx context.Context, task, source string) error {
	return s.store.Update(ctx, func(st *State) error {
		for i := range st.Tests {
			r := &st.Tests[i]
			if r.Task != task || r.SourceCheckout != source || r.Worktree == "" {
				continue
			}
			if err := s.removeWorktree(ctx, "", r.Worktree); err != nil {
				return err
			}
			r.Worktree = ""
		}
		return nil
	})
}
