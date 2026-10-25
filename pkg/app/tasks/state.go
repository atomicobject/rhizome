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
	maxHistory   = 500
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
)

// RunRecord is one run of a task by this person. Every outcome counts for
// the person's clock.
type RunRecord struct {
	Task    string    `json:"task"`
	At      time.Time `json:"at"`
	Outcome string    `json:"outcome"`
	Tokens  int64     `json:"tokens,omitempty"`
	Message string    `json:"message,omitempty"`
}

// State is one person's task state for one repository.
type State struct {
	Paused  bool                 `json:"paused,omitempty"`
	Modes   map[string]Mode      `json:"modes,omitempty"`
	Seen    map[string]time.Time `json:"seen,omitempty"`
	History []RunRecord          `json:"history,omitempty"`
	// Last keeps each task's latest run even after history is trimmed.
	Last map[string]RunRecord `json:"last,omitempty"`
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

// Record appends a run, keeping history bounded.
func (s *State) Record(r RunRecord) {
	if s.Last == nil {
		s.Last = map[string]RunRecord{}
	}
	s.Last[r.Task] = r
	s.History = append(s.History, r)
	if extra := len(s.History) - maxHistory; extra > 0 {
		s.History = append([]RunRecord(nil), s.History[extra:]...)
	}
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
