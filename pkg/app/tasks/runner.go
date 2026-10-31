package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

const runLock = "run.lock"

// Options configure a Service.
type Options struct {
	Repo   Repo
	Config repoconfig.LocalTasksConfig
	// Agent runs prompts; nil makes prompt work fail with needs attention.
	Agent AgentRunner
	// Rzm is the executable that runs code-mode check scripts.
	Rzm string
	// Trusted reports whether the person trusted a checkout; nil trusts all.
	Trusted func(checkout string) (bool, error)
	Now     func() time.Time
}

// Service runs and reports on one repository's tasks for one person.
type Service struct {
	opts     Options
	stateDir string
	store    Store
}

// New prepares a Service for the repository.
func New(opts Options) (*Service, error) {
	dir, err := opts.Repo.StateDir()
	if err != nil {
		return nil, err
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{opts: opts, stateDir: dir, store: Store{Dir: dir}}, nil
}

// Store exposes the person's task state.
func (s *Service) Store() Store { return s.store }

// Outcome is what happened to one task during a run.
type Outcome struct {
	Task    string `json:"task"`
	Outcome string `json:"outcome"`
	Item    string `json:"item,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// RunSummary reports a run-due pass.
type RunSummary struct {
	Ref      string    `json:"ref,omitempty"`
	Commit   string    `json:"commit,omitempty"`
	Skipped  string    `json:"skipped,omitempty"`
	Outcomes []Outcome `json:"outcomes,omitempty"`
	Problems []Problem `json:"problems,omitempty"`
}

// RunDue runs every enabled task that is due, once per repository: a pass
// that finds another pass in progress skips.
func (s *Service) RunDue(ctx context.Context) (RunSummary, error) {
	// Runtimes call this in every Git checkout. A repository that declares
	// no tasks, or that the person has not trusted, is left untouched: no
	// state folder, no lock, no recorded error.
	if reason := s.untrusted(); reason != "" {
		return RunSummary{Skipped: reason}, nil
	}
	if _, commit, err := s.opts.Repo.ResolvePrimary(ctx, s.opts.Config.PrimaryRef); err == nil {
		if files, err := s.opts.Repo.TaskFiles(ctx, commit); err == nil && len(files) == 0 {
			return RunSummary{Skipped: "no tasks are declared on the primary ref"}, nil
		}
	} else if _, statErr := os.Stat(s.stateDir); statErr != nil {
		return RunSummary{}, err
	}
	release, err := s.lockRun(ctx, 0)
	if errors.Is(err, errLocked) {
		return RunSummary{Skipped: "another task run is in progress"}, nil
	}
	if err != nil {
		return RunSummary{}, err
	}
	defer func() { _ = release() }()
	if err := s.finalizeInterrupted(ctx); err != nil {
		return RunSummary{}, err
	}
	defer s.retain(ctx)

	ref, commit, err := s.opts.Repo.ResolvePrimary(ctx, s.opts.Config.PrimaryRef)
	if err != nil {
		return RunSummary{}, s.notePassError(ctx, err)
	}
	defs, problems, err := LoadDefinitions(ctx, s.opts.Repo, commit)
	if err != nil {
		return RunSummary{}, s.notePassError(ctx, err)
	}
	summary := RunSummary{Ref: ref, Commit: commit, Problems: problems}
	tagged := s.lastTagged(ctx, ref, defs)
	now := s.opts.Now()
	var paused bool
	if err := s.store.Update(ctx, func(st *State) error {
		st.PassError = nil
		s.bookkeep(ctx, st, defs, tagged, now)
		paused = st.Paused
		return nil
	}); err != nil {
		return summary, err
	}
	if paused {
		summary.Skipped = "paused"
		return summary, nil
	}
	for _, def := range defs {
		if ctx.Err() != nil {
			break
		}
		st, err := s.store.Load()
		if err != nil {
			return summary, err
		}
		if st.Paused {
			summary.Skipped = "paused"
			break
		}
		mode, _ := st.ResolveMode(def.ID, s.opts.Config.DefaultMode)
		// An open item blocks the task, except a needs-attention item, which
		// waits only for the next cadence.
		if it := st.ActiveTaskItem(def.ID); mode == ModeOff || (it != nil && it.State != StateAttention) {
			continue
		}
		last, _ := st.LastRun(def.ID)
		if !(Clock{LastLocal: last.At, LastTagged: tagged[def.ID]}).Due(def, now) {
			continue
		}
		summary.Outcomes = append(summary.Outcomes, s.runTask(ctx, def, mode, ref, commit))
	}
	return summary, nil
}

// notePassError records why a pass failed before running any task, so
// status can explain why automatic work stopped. It returns err.
func (s *Service) notePassError(ctx context.Context, err error) error {
	_ = s.store.Update(ctx, func(st *State) error {
		st.PassError = &PassError{At: s.opts.Now(), Message: err.Error()}
		return nil
	})
	return err
}

// bookkeep announces tasks the person has not seen and closes items whose
// work reached the primary ref.
func (s *Service) bookkeep(ctx context.Context, st *State, defs []Definition, tagged map[string]*Tagged, now time.Time) {
	st.pruneItems(now)
	if st.Seen == nil {
		st.Seen = map[string]time.Time{}
	}
	for _, def := range defs {
		if _, ok := st.Seen[def.ID]; ok {
			continue
		}
		st.Seen[def.ID] = now
		mode, _ := st.ResolveMode(def.ID, s.opts.Config.DefaultMode)
		text := fmt.Sprintf("New task %q runs every %s in %s mode.", def.Name, def.Every, mode)
		if def.Description != "" {
			text += " " + def.Description
		}
		st.AddItem(Item{Task: def.ID, Kind: KindAnnouncement, State: StateDue, Summary: text}, now)
	}
	for i := range st.Items {
		it := &st.Items[i]
		// This pass holds the run lock, so nothing is running: an item left
		// running was interrupted by a crash or reboot. finalizeInterrupted
		// already recorded the run, so the task waits for its cadence.
		if it.State == StateRunning {
			it.interrupt(now)
		}
		if t := tagged[it.Task]; it.Kind == KindTask && it.Active() && t != nil && s.newlyMerged(ctx, *t, *it) {
			it.set(StateDone, now)
			it.Reason = fmt.Sprintf("%s's commit %s reached the primary ref", t.Author, short(t.Commit))
		}
	}
}

// newlyMerged reports whether tagged work reached the primary ref after the
// item was created: it is not already part of the commit the item was made
// from. A merge can bring in work committed earlier, so committer dates
// cannot decide this; they still drive cadence.
func (s *Service) newlyMerged(ctx context.Context, t Tagged, it Item) bool {
	if it.Commit == "" {
		return t.At.After(it.Created)
	}
	if t.Commit == it.Commit {
		return false
	}
	_, err := s.opts.Repo.git(ctx, "merge-base", "--is-ancestor", t.Commit, it.Commit)
	return err != nil
}

func (s *Service) lastTagged(ctx context.Context, ref string, defs []Definition) map[string]*Tagged {
	out := map[string]*Tagged{}
	for _, def := range defs {
		if t, ok, err := s.opts.Repo.LastTagged(ctx, ref, def.ID); err == nil && ok {
			out[def.ID] = &t
		}
	}
	return out
}

// runTask runs one due task's check, then notifies or does the work.
func (s *Service) runTask(ctx context.Context, def Definition, mode Mode, ref, commit string) Outcome {
	a := s.newAttempt(false)
	base := a.record(def.ID, "", time.Time{})
	base.Commit = commit
	if err := s.begin(ctx, a, base, nil); err != nil {
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Reason: err.Error()}
	}
	defer s.end(ctx, a)
	loc := s.locations(def, commit)
	check := CheckResult{Needed: true}
	if def.Check != nil {
		log := s.openCheckLog(a)
		var err error
		check, err = s.runCheck(ctx, def, loc, log, "")
		log.Close()
		if err != nil {
			return s.fail(ctx, def, a, "", commit, "check failed: "+err.Error(), 0)
		}
	}
	now := s.opts.Now()
	if !check.Needed {
		_ = s.store.Update(ctx, func(st *State) error {
			r := a.record(def.ID, OutcomeNotNeeded, now)
			r.Commit = commit
			st.Record(r)
			return nil
		})
		return Outcome{Task: def.ID, Outcome: OutcomeNotNeeded}
	}
	if mode == ModeNotify || strings.TrimSpace(def.Prompt) == "" {
		var id string
		err := s.store.Update(ctx, func(st *State) error {
			id = st.AddItem(Item{Task: def.ID, Kind: KindTask, State: StateDue, Summary: check.Summary, Context: check.Context, Commit: commit}, now).ID
			r := a.record(def.ID, OutcomeNotified, now)
			r.Message, r.Item, r.Commit = check.Summary, id, commit
			st.Record(r)
			return nil
		})
		if err != nil {
			return Outcome{Task: def.ID, Outcome: OutcomeFailed, Reason: err.Error()}
		}
		return Outcome{Task: def.ID, Outcome: OutcomeNotified, Item: id}
	}
	var id string
	if err := s.store.Update(ctx, func(st *State) error {
		id = st.AddItem(Item{Task: def.ID, Kind: KindTask, State: StateRunning, Summary: check.Summary, Context: check.Context, Commit: commit}, now).ID
		running(st, a, func(r *RunRecord) { r.Item = id })
		return nil
	}); err != nil {
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Reason: err.Error()}
	}
	return s.work(ctx, def, a, id, check, ref, commit)
}

// fail records a failed run, with any tokens it used, and a needs-attention
// item. A failure with no item of its own reuses the task's earlier
// failure notice when that holds no worktree, so notices do not pile up.
// The record takes the item's branch and worktree only when this attempt
// made them. A draft's failure updates its item but is not recorded.
func (s *Service) fail(ctx context.Context, def Definition, a attempt, itemID, commit, reason string, tokens int64) Outcome {
	now := s.opts.Now()
	_ = s.store.Update(ctx, func(st *State) error {
		var it *Item
		if itemID != "" {
			it, _ = st.Item(itemID)
		}
		if it == nil {
			if prior := st.ActiveTaskItem(def.ID); prior != nil && prior.State == StateAttention && prior.Worktree == "" {
				it = prior
				it.Commit = commit
			}
		}
		if it == nil {
			it = st.AddItem(Item{Task: def.ID, Kind: KindTask, Commit: commit}, now)
		}
		it.set(StateAttention, now)
		it.Reason, it.Tokens = reason, tokens
		itemID = it.ID
		if a.draft {
			return nil
		}
		r := a.record(def.ID, OutcomeFailed, now)
		r.Message, r.Tokens, r.Item, r.Commit = reason, tokens, it.ID, commit
		if it.Run == a.id {
			r.Branch, r.Worktree = it.Branch, it.Worktree
		}
		st.Record(r)
		return nil
	})
	return Outcome{Task: def.ID, Outcome: OutcomeFailed, Item: itemID, Reason: reason}
}

// untrusted explains why tasks may not run here, or returns "".
func (s *Service) untrusted() string {
	if s.opts.Trusted == nil {
		return ""
	}
	checkout := s.opts.Repo.MainCheckout
	if checkout == "" {
		checkout = s.opts.Repo.CommonDir
	}
	ok, err := s.opts.Trusted(checkout)
	if err != nil {
		return "repository trust could not be read: " + err.Error()
	}
	if !ok {
		return "this repository is not trusted; run `rzm trust` in " + checkout + " to let its tasks run"
	}
	return ""
}

func (s *Service) locations(def Definition, commit string) Locations {
	return Locations{
		TaskID:       def.ID,
		MainCheckout: s.opts.Repo.MainCheckout,
		Commit:       commit,
		DataDir:      filepath.Join(s.stateDir, "data", def.ID),
	}
}

func (s *Service) lockRun(ctx context.Context, wait time.Duration) (func() error, error) {
	if err := os.MkdirAll(s.stateDir, 0o700); err != nil {
		return nil, err
	}
	return acquire(ctx, filepath.Join(s.stateDir, runLock), "tasks/run", wait)
}

func short(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
