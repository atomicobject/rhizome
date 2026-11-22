package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

const runLock = "run.lock"

// Options configure a Service.
type Options struct {
	Checkout string
	Repo     Repo
	Config   repoconfig.LocalTasksConfig
	// Agent runs prompts; nil makes prompt work fail with needs attention.
	Agent AgentRunner
	// Rzm is the executable that runs code-mode check scripts.
	Rzm string
	// Trusted reports whether the person trusted a checkout; nil trusts all.
	Trusted func(checkout string) (bool, error)
	// Env locates the user configuration with the person's per-machine
	// task settings; a zero Env means none, so tasks do not run here.
	Env vaultconfig.Env
	Now func() time.Time
	// Push is the person's per-machine push setting for this repository
	// (SPEC-0127 US13); pushing also needs the repository's allowPush.
	Push bool
}

// Service runs and reports on one repository's tasks for one person.
type Service struct {
	opts     Options
	stateDir string
	store    Store
	merge    struct {
		mu    sync.Mutex
		value string
		at    time.Time
	}
	due dueInputsCache
	// slotFor names a task's due slot; nil means runs have no slot but
	// their own ID. See slotOf.
	slotFor func(Definition, time.Time) string
}

// New prepares a Service for the repository.
func New(opts Options) (*Service, error) {
	dir, err := opts.Repo.StateDir()
	if err != nil {
		return nil, err
	}
	if opts.Checkout == "" {
		opts.Checkout = opts.Repo.MainCheckout
	}
	if resolved, err := filepath.EvalSymlinks(opts.Checkout); err == nil {
		opts.Checkout = resolved
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

// RunDue runs every enabled task that is due, once per repository, nearest
// deadline first: a pass that finds another pass in progress skips, and a
// pass with due work fetches the primary ref's remote first.
func (s *Service) RunDue(ctx context.Context) (RunSummary, error) { return s.runDue(ctx, false) }

// runDue is RunDue; a host pass stops before any task once the person turns
// off running tasks on this machine.
func (s *Service) runDue(ctx context.Context, host bool) (RunSummary, error) {
	// Runtimes call this in every Git checkout. A repository that declares
	// no tasks, or that the person has not trusted, is left untouched: no
	// state folder, no lock, no recorded error.
	if reason := s.untrusted(); reason != "" {
		return RunSummary{Skipped: reason}, nil
	}
	if _, commit, err := s.opts.Repo.ResolvePrimary(ctx, s.opts.Config.PrimaryRef); err == nil {
		if files, err := s.opts.Repo.TaskFiles(ctx, commit); err == nil && len(files) == 0 {
			// A run interrupted before the last task was removed is still
			// recorded once; otherwise state is left alone.
			if st, err := s.store.Load(); err == nil && st.Running != nil {
				if release, err := s.lockRun(ctx, 0); err == nil {
					_ = s.finalizeInterrupted(ctx)
					_ = release()
				}
			}
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

	ref, commit, defs, problems, err := s.primary(ctx)
	if err != nil {
		return RunSummary{}, s.notePassError(ctx, err)
	}
	zone := s.zone()
	// Whether anything is due is decided from the local primary ref, with
	// no network; only a pass with due work, or items waiting for their work
	// to reach the primary ref, fetches, then reloads.
	if st, err := s.store.Load(); err == nil && (len(dueNow(&st, defs, s.lastTagged(ctx, ref, defs), s.opts.Now(), zone, s.opts.Config.DefaultMode)) > 0 || settleDue(&st, defs, true, s.opts.Now())) {
		s.fetchPrimary(ctx, ref)
		if ref, commit, defs, problems, err = s.primary(ctx); err != nil {
			return RunSummary{}, s.notePassError(ctx, err)
		}
	}
	summary := RunSummary{Ref: ref, Commit: commit, Problems: problems}
	tagged := s.lastTagged(ctx, ref, defs)
	now := s.opts.Now()
	var (
		paused bool
		order  []dueTask
	)
	if err := s.store.Update(ctx, func(st *State) error {
		st.PassError, st.LastPass = nil, now
		if st.Paused && !st.PausedAt(now) {
			st.Paused, st.PausedUntil = false, time.Time{}
		}
		s.bookkeep(ctx, st, defs, tagged, now)
		paused = st.PausedAt(now)
		order = dueNow(st, defs, tagged, now, zone, s.opts.Config.DefaultMode)
		return nil
	}); err != nil {
		return summary, err
	}
	if paused {
		summary.Skipped = "paused"
		return summary, nil
	}
	for _, d := range order {
		if ctx.Err() != nil {
			break
		}
		st, err := s.store.Load()
		if err != nil {
			return summary, err
		}
		if st.PausedAt(s.opts.Now()) {
			summary.Skipped = "paused"
			break
		}
		if host {
			if m, err := s.MachineSettings(); err != nil || !m.Repository.RunHere {
				summary.Skipped = notRunHere
				break
			}
		}
		// An earlier task's run took time: decide again now, so a slot whose
		// late window passed meanwhile waits for its next slot.
		current, blocked := taskDue(&st, d.def, tagged[d.def.ID], s.opts.Now(), zone, s.opts.Config.DefaultMode)
		mode := st.landingMode(d.def.ID, current.mode)
		if blocked != "" || !current.due.Now {
			continue
		}
		if it := st.ActiveTaskItem(d.def.ID); it != nil && it.State == StateDue {
			summary.Outcomes = append(summary.Outcomes, s.recheck(ctx, d.def, it.ID, commit))
			continue
		}
		if slot := s.slotOf(d.def, s.opts.Now()); slot != "" {
			if landed, ok, _ := s.opts.Repo.SlotLanded(ctx, ref, d.def.ID, slot); ok {
				summary.Outcomes = append(summary.Outcomes, s.slotDone(ctx, d.def, slot, landed, commit))
				continue
			}
		}
		summary.Outcomes = append(summary.Outcomes, s.runTask(ctx, d.def, mode, ref, commit))
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
	reviewLandings(st, defs, now)
	for _, def := range defs {
		if _, ok := st.Seen[def.ID]; ok {
			continue
		}
		st.Seen[def.ID] = now
		mode, _ := st.ResolveMode(def.ID, s.opts.Config.DefaultMode)
		text := fmt.Sprintf("New task %q runs %s in %s mode. It %s.", def.Name, def.CadenceText(), mode, landingOf(def).describe())
		if def.Description != "" {
			text += " " + def.Description
		}
		st.AddItem(Item{Task: def.ID, Kind: KindAnnouncement, State: StateDue, Summary: text}, now)
	}
	for i := range st.Items {
		it := &st.Items[i]
		// This pass holds the run lock, so nothing is running: an item left
		// running was interrupted by a crash or reboot. finalizeInterrupted
		// already recorded the run, so the task waits for its cadence. An item
		// from before run IDs (no Run) had no Running record, so record it
		// here, as phase 1 did.
		if it.State == StateRunning {
			if it.Kind == KindTask && it.Run == "" {
				st.Record(RunRecord{Task: it.Task, At: now, Outcome: OutcomeInterrupted, Item: it.ID, Message: interruptedMessage})
			}
			it.interrupt(now, interruptedMessage)
		}
		// An in-place item's work is already on the branch; only a pr
		// branch's merge closes its item.
		if t := tagged[it.Task]; it.Kind == KindTask && it.Active() && it.Landing != taskdef.LandInPlace && t != nil && s.newlyMerged(ctx, *t, *it) {
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
	a.slot = s.slotOf(def, s.opts.Now())
	base := a.record(def.ID, "", time.Time{})
	base.Commit = commit
	if err := s.begin(ctx, a, base, nil); err != nil {
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Reason: err.Error()}
	}
	defer s.end(ctx, a)
	if a.missing = s.missingRequirements(ctx, def); len(a.missing) > 0 {
		return s.fail(ctx, def, a, "", commit, requirementsMessage(a.missing), 0)
	}
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
	if def.Land == taskdef.LandInPlace {
		return s.runInPlace(ctx, def, a, "", check, ref, commit)
	}
	var id string
	if err := s.store.Update(ctx, func(st *State) error {
		id = st.AddItem(Item{Task: def.ID, Kind: KindTask, State: StateRunning, Summary: check.Summary, Context: check.Context, Commit: commit, Landing: def.Land}, now).ID
		running(st, a, func(r *RunRecord) { r.Item = id })
		return nil
	}); err != nil {
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Reason: err.Error()}
	}
	return s.work(ctx, def, a, id, check, ref, commit)
}

// recheck runs a due item's check again (SPEC-0127 US6). Nothing needed
// closes the item as done; still needed refreshes its summary and context
// in place, keeping any snooze. It never adds an item or starts work, and a
// failed check moves the item to needs attention.
func (s *Service) recheck(ctx context.Context, def Definition, itemID, commit string) Outcome {
	a := s.newAttempt(false)
	base := a.record(def.ID, "", time.Time{})
	base.Commit, base.Item = commit, itemID
	if err := s.begin(ctx, a, base, nil); err != nil {
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Reason: err.Error()}
	}
	defer s.end(ctx, a)
	if a.missing = s.missingRequirements(ctx, def); len(a.missing) > 0 {
		return s.failRecheck(ctx, def, a, itemID, commit, requirementsMessage(a.missing))
	}
	check := CheckResult{Needed: true}
	if def.Check != nil {
		log := s.openCheckLog(a)
		var err error
		check, err = s.runCheck(ctx, def, s.locations(def, commit), log, "")
		log.Close()
		if err != nil {
			return s.failRecheck(ctx, def, a, itemID, commit, "check failed: "+err.Error())
		}
	}
	outcome := OutcomeNotified
	if !check.Needed {
		outcome = OutcomeNotNeeded
	}
	now := s.opts.Now()
	err := s.store.Update(ctx, func(st *State) error {
		r := a.record(def.ID, outcome, now)
		r.Item, r.Commit = itemID, commit
		// The person may have acted on the item while the check ran.
		if it, _ := st.Item(itemID); it != nil && it.State == StateDue {
			if check.Needed {
				it.Summary, it.Context, it.Updated = check.Summary, check.Context, now
				r.Message = check.Summary
			} else {
				it.set(StateDone, now)
				it.Reason = "the check found nothing to do"
			}
		}
		st.Record(r)
		return nil
	})
	if err != nil {
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Reason: err.Error()}
	}
	return Outcome{Task: def.ID, Outcome: outcome, Item: itemID}
}

// failRecheck records a failed re-check and moves its item to needs
// attention only while the item is still due: the person may have closed it
// while the check ran.
func (s *Service) failRecheck(ctx context.Context, def Definition, a attempt, itemID, commit, reason string) Outcome {
	now := s.opts.Now()
	_ = s.store.Update(ctx, func(st *State) error {
		if it, _ := st.Item(itemID); it != nil && it.State == StateDue {
			it.set(StateAttention, now)
			it.Reason, it.Tokens = reason, 0
		}
		r := a.record(def.ID, OutcomeFailed, now)
		r.Message, r.Item, r.Commit = reason, itemID, commit
		st.Record(r)
		return nil
	})
	return Outcome{Task: def.ID, Outcome: OutcomeFailed, Item: itemID, Reason: reason}
}

// fail records a failed run, with any tokens it used, and a needs-attention
// item. A failure with no item of its own reuses the task's earlier
// failure notice when that holds no worktree, so notices do not pile up.
// The record takes the item's branch and worktree only when this attempt
// made them.
func (s *Service) fail(ctx context.Context, def Definition, a attempt, itemID, commit, reason string, tokens int64) Outcome {
	return s.failWith(ctx, def, a, itemID, commit, OutcomeFailed, reason, tokens, nil)
}

// failWith is fail with another outcome, such as a skipped in-place run,
// and extra changes made in the same state update.
func (s *Service) failWith(ctx context.Context, def Definition, a attempt, itemID, commit, outcome, reason string, tokens int64, extra func(*State, *Item, *RunRecord)) Outcome {
	now := s.opts.Now()
	_ = s.store.Update(ctx, func(st *State) error {
		var it *Item
		if itemID != "" {
			it, _ = st.Item(itemID)
		}
		// The person may have closed the item during the run; keep it closed.
		closed := it != nil && !it.Active()
		if it == nil {
			if prior := st.ActiveTaskItem(def.ID); prior != nil && prior.State == StateAttention && prior.Worktree == "" {
				it = prior
				it.Commit = commit
			}
		}
		if it == nil {
			it = st.AddItem(Item{Task: def.ID, Kind: KindTask, Commit: commit, Landing: def.Land}, now)
		}
		if !closed {
			it.set(StateAttention, now)
			it.Reason, it.Tokens = reason, tokens
		}
		itemID = it.ID
		r := a.record(def.ID, outcome, now)
		r.Message, r.Tokens, r.Item, r.Commit, r.Landing = reason, tokens, it.ID, commit, def.Land
		if it.Run == a.id {
			r.Branch, r.Worktree = it.Branch, it.Worktree
		}
		if extra != nil {
			extra(st, it, &r)
		}
		st.Record(r)
		return nil
	})
	return Outcome{Task: def.ID, Outcome: outcome, Item: itemID, Reason: reason}
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
	return repositorytrust.DenialReason(checkout, "tasks", s.opts.Trusted)
}

func (s *Service) locations(def Definition, commit string) Locations {
	return Locations{
		TaskID:       def.ID,
		MainCheckout: s.opts.Repo.MainCheckout,
		Commit:       commit,
		DataDir:      filepath.Join(s.stateDir, "data", taskdef.PathKey(def.ID)),
	}
}

func (s *Service) lockRun(ctx context.Context, wait time.Duration) (func() error, error) {
	if err := os.MkdirAll(s.stateDir, 0o700); err != nil {
		return nil, err
	}
	return acquire(ctx, s.runLockPath(), "tasks/run", wait)
}

func (s *Service) runLockPath() string { return filepath.Join(s.stateDir, runLock) }
