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

// ErrBusy means another task run holds the repository's run lock.
var ErrBusy = errors.New("a task run is in progress in this repository; try again when it finishes")

// Run does a due item's work in the background now, at the person's
// request. It reads the task's current definition from the primary ref.
func (s *Service) Run(ctx context.Context, itemID string) (Outcome, error) {
	release, err := s.lockRun(ctx, 0)
	if errors.Is(err, errLocked) {
		return Outcome{}, ErrBusy
	}
	if err != nil {
		return Outcome{}, err
	}
	defer func() { _ = release() }()
	if reason := s.untrusted(); reason != "" {
		return Outcome{}, errors.New(reason)
	}
	st, err := s.store.Load()
	if err != nil {
		return Outcome{}, err
	}
	it, err := st.Item(itemID)
	if err != nil {
		return Outcome{}, err
	}
	if it.Kind != KindTask || (it.State != StateDue && it.State != StateAttention) {
		return Outcome{}, fmt.Errorf("item %s is %s; only a due or needs-attention task can be run", it.ID, it.State)
	}
	ref, commit, def, err := s.currentDefinition(ctx, it.Task)
	if err != nil {
		return Outcome{}, err
	}
	if def.Prompt == "" {
		return Outcome{}, fmt.Errorf("task %q has no prompt to run", def.ID)
	}
	check := CheckResult{Needed: true, Summary: it.Summary, Context: it.Context}
	if err := s.store.Update(ctx, func(st *State) error {
		it, err := st.Item(itemID)
		if err != nil {
			return err
		}
		it.set(StateRunning, s.opts.Now())
		it.Reason = ""
		return nil
	}); err != nil {
		return Outcome{}, err
	}
	return s.work(ctx, def, itemID, check, ref, commit), nil
}

// RunDraft runs a task file from the working tree once, ignoring its
// cadence and mode, so an author can try it before merging. Its check runs
// in the checkout holding the file; its prompt runs like any background run.
func (s *Service) RunDraft(ctx context.Context, file string) (Outcome, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Outcome{}, err
	}
	def, err := ParseDefinition(file, data)
	if err != nil {
		return Outcome{}, fmt.Errorf("%s: %w", file, err)
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return Outcome{}, err
	}
	root, err := gitOutput(ctx, filepath.Dir(abs), "rev-parse", "--show-toplevel")
	if err != nil {
		return Outcome{}, err
	}
	release, err := s.lockRun(ctx, 0)
	if errors.Is(err, errLocked) {
		return Outcome{}, ErrBusy
	}
	if err != nil {
		return Outcome{}, err
	}
	defer func() { _ = release() }()
	if reason := s.untrusted(); reason != "" {
		return Outcome{}, errors.New(reason)
	}
	ref, commit, err := s.opts.Repo.ResolvePrimary(ctx, s.opts.Config.PrimaryRef)
	if err != nil {
		return Outcome{}, err
	}
	check := CheckResult{Needed: true}
	if def.Check != nil {
		log, closeLog := s.openLog(def.ID, "draft-check")
		check, err = s.runCheck(ctx, def, s.locations(def, commit), log, root)
		closeLog()
		if err != nil {
			return Outcome{Task: def.ID, Outcome: OutcomeFailed, Reason: err.Error()}, nil
		}
	}
	if !check.Needed {
		return Outcome{Task: def.ID, Outcome: OutcomeNotNeeded}, nil
	}
	if strings.TrimSpace(def.Prompt) == "" {
		return Outcome{Task: def.ID, Outcome: "needed", Reason: check.Summary}, nil
	}
	var id string
	if err := s.store.Update(ctx, func(st *State) error {
		id = st.AddItem(Item{Task: def.ID, Kind: KindTask, State: StateRunning, Summary: check.Summary, Context: check.Context, Commit: commit}, s.opts.Now()).ID
		return nil
	}); err != nil {
		return Outcome{}, err
	}
	return s.work(ctx, def, id, check, ref, commit), nil
}

// currentDefinition loads one task's definition at the resolved primary ref.
func (s *Service) currentDefinition(ctx context.Context, task string) (ref, commit string, def Definition, err error) {
	ref, commit, err = s.opts.Repo.ResolvePrimary(ctx, s.opts.Config.PrimaryRef)
	if err != nil {
		return "", "", Definition{}, err
	}
	defs, _, err := LoadDefinitions(ctx, s.opts.Repo, commit)
	if err != nil {
		return "", "", Definition{}, err
	}
	for _, d := range defs {
		if d.ID == task {
			return ref, commit, d, nil
		}
	}
	return "", "", Definition{}, fmt.Errorf("task %q is not declared at %s", task, ref)
}

// Snooze hides an item until the given time.
func (s *Service) Snooze(ctx context.Context, itemID string, until time.Time) error {
	return s.updateItem(ctx, itemID, func(st *State, it *Item) error {
		if !it.Active() {
			return fmt.Errorf("item %s is %s", it.ID, it.State)
		}
		it.Snoozed, it.Updated = until, s.opts.Now()
		return nil
	})
}

// Skip dismisses a due item; the task's clock counts it as run.
func (s *Service) Skip(ctx context.Context, itemID string) error {
	return s.updateItem(ctx, itemID, func(st *State, it *Item) error {
		if it.State != StateDue {
			return fmt.Errorf("item %s is %s; only a due item can be skipped", it.ID, it.State)
		}
		it.set(StateDismissed, s.opts.Now())
		if it.Kind == KindTask {
			st.Record(RunRecord{Task: it.Task, At: s.opts.Now(), Outcome: OutcomeSkipped})
		}
		return nil
	})
}

// Discard removes a finished run's worktree and local branch.
func (s *Service) Discard(ctx context.Context, itemID string) error {
	var branch, dir string
	if err := s.updateItem(ctx, itemID, func(st *State, it *Item) error {
		if it.State != StateReady && it.State != StateAttention {
			return fmt.Errorf("item %s is %s; only a ready or needs-attention item can be discarded", it.ID, it.State)
		}
		branch, dir = it.Branch, it.Worktree
		it.set(StateDismissed, s.opts.Now())
		it.Branch, it.Worktree = "", ""
		return nil
	}); err != nil {
		return err
	}
	s.removeWorktree(ctx, branch, dir)
	return nil
}

// Done closes an item.
func (s *Service) Done(ctx context.Context, itemID string) error {
	return s.updateItem(ctx, itemID, func(st *State, it *Item) error {
		if !it.Active() {
			return fmt.Errorf("item %s is already %s", it.ID, it.State)
		}
		it.set(StateDone, s.opts.Now())
		return nil
	})
}

// SetMode overrides a task's mode for the person; "default" removes the
// override.
func (s *Service) SetMode(ctx context.Context, task, mode string) error {
	var m Mode
	if mode != "default" {
		var err error
		if m, err = ParseMode(mode); err != nil {
			return err
		}
	}
	return s.store.Update(ctx, func(st *State) error {
		if mode == "default" {
			delete(st.Modes, task)
			return nil
		}
		if st.Modes == nil {
			st.Modes = map[string]Mode{}
		}
		st.Modes[task] = m
		return nil
	})
}

// SetPaused pauses or resumes all background work for the person.
func (s *Service) SetPaused(ctx context.Context, paused bool) error {
	return s.store.Update(ctx, func(st *State) error {
		st.Paused = paused
		return nil
	})
}

func (s *Service) updateItem(ctx context.Context, itemID string, fn func(*State, *Item) error) error {
	return s.store.Update(ctx, func(st *State) error {
		it, err := st.Item(itemID)
		if err != nil {
			return err
		}
		return fn(st, it)
	})
}
