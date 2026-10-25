package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	release, err := s.lockRun(ctx, 0)
	if errors.Is(err, errLocked) {
		return RunSummary{Skipped: "another task run is in progress"}, nil
	}
	if err != nil {
		return RunSummary{}, err
	}
	defer func() { _ = release() }()

	if reason := s.untrusted(); reason != "" {
		return RunSummary{Skipped: reason}, nil
	}
	ref, commit, err := s.opts.Repo.ResolvePrimary(ctx, s.opts.Config.PrimaryRef)
	if err != nil {
		return RunSummary{}, err
	}
	defs, problems, err := LoadDefinitions(ctx, s.opts.Repo, commit)
	if err != nil {
		return RunSummary{}, err
	}
	summary := RunSummary{Ref: ref, Commit: commit, Problems: problems}
	tagged := s.lastTagged(ctx, ref, defs)
	now := s.opts.Now()
	var paused bool
	if err := s.store.Update(ctx, func(st *State) error {
		s.bookkeep(st, defs, tagged, now)
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
		mode, _ := st.ResolveMode(def.ID, s.opts.Config.DefaultMode)
		if mode == ModeOff || st.ActiveTaskItem(def.ID) != nil {
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

// bookkeep announces tasks the person has not seen and closes items whose
// work reached the primary ref.
func (s *Service) bookkeep(st *State, defs []Definition, tagged map[string]*Tagged, now time.Time) {
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
		if t := tagged[it.Task]; it.Kind == KindTask && it.Active() && t != nil && t.At.After(it.Created) {
			it.set(StateDone, now)
			it.Reason = fmt.Sprintf("%s's commit %s reached the primary ref", t.Author, short(t.Commit))
		}
	}
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
	now := s.opts.Now()
	loc := s.locations(def, commit)
	check := CheckResult{Needed: true}
	if def.Check != nil {
		log, closeLog := s.openLog(def.ID, "check")
		var err error
		check, err = s.runCheck(ctx, def, loc, log, "")
		closeLog()
		if err != nil {
			return s.fail(ctx, def, "", commit, "check failed: "+err.Error())
		}
	}
	if !check.Needed {
		_ = s.store.Update(ctx, func(st *State) error {
			st.Record(RunRecord{Task: def.ID, At: now, Outcome: OutcomeNotNeeded})
			return nil
		})
		return Outcome{Task: def.ID, Outcome: OutcomeNotNeeded}
	}
	if mode == ModeNotify || strings.TrimSpace(def.Prompt) == "" {
		var id string
		err := s.store.Update(ctx, func(st *State) error {
			id = st.AddItem(Item{Task: def.ID, Kind: KindTask, State: StateDue, Summary: check.Summary, Context: check.Context, Commit: commit}, now).ID
			st.Record(RunRecord{Task: def.ID, At: now, Outcome: OutcomeNotified, Message: check.Summary})
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
		return nil
	}); err != nil {
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Reason: err.Error()}
	}
	return s.work(ctx, def, id, check, ref, commit)
}

// fail records a failed run and a needs-attention item.
func (s *Service) fail(ctx context.Context, def Definition, itemID, commit, reason string) Outcome {
	now := s.opts.Now()
	_ = s.store.Update(ctx, func(st *State) error {
		var it *Item
		if itemID != "" {
			it, _ = st.Item(itemID)
		}
		if it == nil {
			it = st.AddItem(Item{Task: def.ID, Kind: KindTask, Commit: commit}, now)
		}
		it.set(StateAttention, now)
		it.Reason = reason
		itemID = it.ID
		st.Record(RunRecord{Task: def.ID, At: now, Outcome: OutcomeFailed, Message: reason})
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

// openLog opens a per-run log file; failures fall back to discarding.
func (s *Service) openLog(task, step string) (io.Writer, func()) {
	dir := filepath.Join(s.stateDir, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return io.Discard, func() {}
	}
	name := fmt.Sprintf("%s-%s-%s.log", task, s.opts.Now().UTC().Format("20060102T150405Z"), step)
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return io.Discard, func() {}
	}
	return f, func() { _ = f.Close() }
}

func short(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
