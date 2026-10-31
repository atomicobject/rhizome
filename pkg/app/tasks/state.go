package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

const (
	stateFile    = "state.json"
	stateLock    = "state.lock"
	lockWait     = 10 * time.Second
	lockPollTime = 50 * time.Millisecond
)

// Mode says whether a task runs in the background on its own, only
// notifies, or does nothing.
type Mode string

const (
	ModeAuto   Mode = "auto"
	ModeNotify Mode = "notify"
	ModeOff    Mode = "off"
)

// ParseMode accepts auto, notify, or off.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(s); m {
	case ModeAuto, ModeNotify, ModeOff:
		return m, nil
	}
	return "", fmt.Errorf("mode must be auto, notify, or off, not %q", s)
}

// Run outcomes recorded in history.
const (
	OutcomeNotNeeded = "not-needed"
	OutcomeNotified  = "notified"
	OutcomeReady     = "ready"
	OutcomeNoChanges = "no-changes"
	OutcomeFailed    = "failed"
	OutcomeStarted   = "started"
	OutcomeSkipped   = "skipped"
	// OutcomeInterrupted is a run whose process ended before it finished.
	OutcomeInterrupted = "interrupted"
)

// Run stages, recorded only on State.Running.
const (
	StageCheck = "check"
	StageAgent = "agent"
)

// RunRecord is one run of a task by this person. Every outcome counts for
// the person's clock. A record keeps its own item, branch, and worktree, so
// an old run never shows a newer attempt's work.
type RunRecord struct {
	ID        string    `json:"id,omitempty"`
	Task      string    `json:"task"`
	StartedAt time.Time `json:"startedAt,omitzero"`
	At        time.Time `json:"at"`
	Outcome   string    `json:"outcome"`
	Stage     string    `json:"stage,omitempty"`
	Tokens    int64     `json:"tokens,omitempty"`
	Message   string    `json:"message,omitempty"`
	Item      string    `json:"item,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	Commit    string    `json:"commit,omitempty"`
	Worktree  string    `json:"worktree,omitempty"`
	Changed   []string  `json:"changed,omitempty"`
}

// State is one person's task state for one repository.
type State struct {
	Paused  bool                 `json:"paused,omitempty"`
	Modes   map[string]Mode      `json:"modes,omitempty"`
	Seen    map[string]time.Time `json:"seen,omitempty"`
	History []RunRecord          `json:"history,omitempty"`
	// Last keeps each task's latest run even after history is trimmed.
	Last map[string]RunRecord `json:"last,omitempty"`
	// Running is the run holding run.lock, set when it starts. One left
	// behind by a crash is recorded as interrupted by the next run.
	Running *RunRecord `json:"running,omitempty"`
	// Tokens is task -> UTC day ("2006-01-02") -> tokens, for 30 days, so
	// totals survive history trimming.
	Tokens map[string]map[string]int64 `json:"tokens,omitempty"`
	// PassError is why the latest run-due pass failed before running tasks.
	PassError *PassError `json:"passError,omitempty"`
	Items     []Item     `json:"items,omitempty"`
	NextID    int        `json:"nextId,omitempty"`
}

// PassError records a run-due pass that failed before running any task.
type PassError struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

// LastRun returns the person's latest run of a task.
func (s *State) LastRun(task string) (RunRecord, bool) {
	if r, ok := s.Last[task]; ok {
		return r, true
	}
	for i := len(s.History) - 1; i >= 0; i-- {
		if s.History[i].Task == task {
			return s.History[i], true
		}
	}
	return RunRecord{}, false
}

// Record appends a run and counts its tokens. It clears Running when that
// is this run. Retention trims history at the end of each run.
func (s *State) Record(r RunRecord) {
	if s.Last == nil {
		s.Last = map[string]RunRecord{}
	}
	r.Stage = ""
	// Seed before appending, so this run is not counted twice.
	s.seedTokens()
	s.Last[r.Task] = r
	s.History = append(s.History, r)
	if s.Running != nil && r.ID != "" && s.Running.ID == r.ID {
		s.Running = nil
	}
	if r.Tokens > 0 {
		s.addTokens(r.Task, r.At, r.Tokens)
	}
}

const tokenDays = 30

func tokenDay(t time.Time) string { return t.UTC().Format(time.DateOnly) }

// tokenCutoff is the oldest day bucket that still counts at now.
func tokenCutoff(now time.Time) string { return tokenDay(now.AddDate(0, 0, -(tokenDays - 1))) }

func (s *State) addTokens(task string, at time.Time, n int64) {
	if s.Tokens[task] == nil {
		s.Tokens[task] = map[string]int64{}
	}
	s.Tokens[task][tokenDay(at)] += n
}

// seedTokens builds the token buckets from history the first time, for
// state written before buckets existed.
func (s *State) seedTokens() {
	if s.Tokens != nil {
		return
	}
	s.Tokens = map[string]map[string]int64{}
	for _, r := range s.History {
		if r.Tokens > 0 {
			s.addTokens(r.Task, r.At, r.Tokens)
		}
	}
}

// trimTokens drops day buckets older than 30 days.
func (s *State) trimTokens(now time.Time) {
	cutoff := tokenCutoff(now)
	for task, days := range s.Tokens {
		for day := range days {
			if day < cutoff {
				delete(days, day)
			}
		}
		if len(days) == 0 {
			delete(s.Tokens, task)
		}
	}
}

// TokensLast30Days sums a task's token buckets for the last 30 UTC days.
func (s *State) TokensLast30Days(task string, now time.Time) int64 {
	st := *s
	if st.Tokens == nil {
		st.seedTokens()
	}
	cutoff := tokenCutoff(now)
	var total int64
	for day, n := range st.Tokens[task] {
		if day >= cutoff {
			total += n
		}
	}
	return total
}

// ResolveMode returns a task's mode and where it came from: the person's
// override, the repository default, or the built-in notify default.
func (s *State) ResolveMode(task string, repoDefault string) (Mode, string) {
	if m, ok := s.Modes[task]; ok {
		return m, "override"
	}
	if m, err := ParseMode(repoDefault); err == nil && m != ModeOff {
		return m, "repository"
	}
	return ModeNotify, "default"
}

// Store reads and writes state.json under a short lock.
type Store struct{ Dir string }

// Path is the state file.
func (st Store) Path() string { return filepath.Join(st.Dir, stateFile) }

// Load reads state; a missing file is empty state. Writes are atomic, so
// reading needs no lock.
func (st Store) Load() (State, error) {
	var s State
	data, err := os.ReadFile(st.Path())
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("read %s: %w", st.Path(), err)
	}
	return s, nil
}

// Update applies fn to the current state under the state lock and saves
// the result when fn succeeds.
func (st Store) Update(ctx context.Context, fn func(*State) error) error {
	if err := os.MkdirAll(st.Dir, 0o700); err != nil {
		return err
	}
	release, err := acquire(ctx, filepath.Join(st.Dir, stateLock), "tasks/state", lockWait)
	if err != nil {
		return err
	}
	defer func() { _ = release() }()
	s, err := st.Load()
	if err != nil {
		return err
	}
	if err := fn(&s); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fileio.WriteFileAtomic(st.Path(), data, 0o600)
}

// acquire waits up to wait for a lock, or returns at once when wait is 0.
func acquire(ctx context.Context, path, role string, wait time.Duration) (func() error, error) {
	deadline := time.Now().Add(wait)
	for {
		release, ok, err := indexlock.TryAcquireWithOptions(path, indexlock.AcquireOptions{Role: role})
		if err != nil {
			return nil, err
		}
		if ok {
			return release, nil
		}
		if !time.Now().Before(deadline) {
			return nil, errLocked
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockPollTime):
		}
	}
}

var errLocked = errors.New("lock is held by another process")

func (s *State) newItemID() string {
	s.NextID++
	return strconv.Itoa(s.NextID)
}
