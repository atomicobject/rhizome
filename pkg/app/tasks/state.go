package tasks

import (
	"slices"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
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
	// OutcomeStopped is a run the person stopped (Service.Stop).
	OutcomeStopped = "stopped"
	// OutcomeLanded is an in-place run that committed its files.
	OutcomeLanded = "landed"
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
	Test bool `json:"test,omitempty"`
	// Scheduled is a run a pass started rather than the person; only its
	// tokens count toward the daily token cap (SPEC-0127 US14).
	Scheduled      bool      `json:"scheduled,omitempty"`
	SourceCheckout string    `json:"sourceCheckout,omitempty"`
	ID             string    `json:"id,omitempty"`
	Task           string    `json:"task"`
	StartedAt      time.Time `json:"startedAt,omitzero"`
	At             time.Time `json:"at"`
	Outcome        string    `json:"outcome"`
	Stage          string    `json:"stage,omitempty"`
	Tokens         int64     `json:"tokens,omitempty"`
	Message        string    `json:"message,omitempty"`
	Item           string    `json:"item,omitempty"`
	Branch         string    `json:"branch,omitempty"`
	Commit         string    `json:"commit,omitempty"`
	Worktree       string    `json:"worktree,omitempty"`
	Changed        []string  `json:"changed,omitempty"`
	// Missing names the requirements a failed run lacked (SPEC-0127 US11).
	Missing []string `json:"missing,omitempty"`

	// Landing (SPEC-0127 US12, US13): the run's land mode, the commit an
	// in-place run made or pushed, a pr run's pull request, and the paths an
	// in-place run left uncommitted.
	Landing string   `json:"landing,omitempty"`
	Landed  string   `json:"landed,omitempty"`
	PR      string   `json:"pr,omitempty"`
	Left    []string `json:"left,omitempty"`
	// Result is what the agent wrote to $RZM_TASK_RESULT_FILE, and
	// NotifyError why the notify command failed after the run (US14).
	Result      *RunResult `json:"result,omitempty"`
	NotifyError string     `json:"notifyError,omitempty"`
	// Holder names the run lock acquisition that started the run (PID and
	// lock time), so Stop marks only its holder's run.
	Holder string `json:"holder,omitempty"`
}

// State is one person's task state for one repository.
type State struct {
	Tests  []RunRecord `json:"tests,omitempty"`
	Paused bool        `json:"paused,omitempty"`
	// PausedUntil ends a pause on its own; zero pauses until resumed.
	PausedUntil time.Time            `json:"pausedUntil,omitzero"`
	Modes       map[string]Mode      `json:"modes,omitempty"`
	Seen        map[string]time.Time `json:"seen,omitempty"`
	History     []RunRecord          `json:"history,omitempty"`
	// Last keeps each task's latest run even after history is trimmed.
	Last map[string]RunRecord `json:"last,omitempty"`
	// Running is the run holding run.lock, set when it starts. One left
	// behind by a crash is recorded as interrupted by the next run.
	Running *RunRecord `json:"running,omitempty"`
	// Tokens is task -> UTC day ("2006-01-02") -> tokens, for 30 days, so
	// totals survive history trimming.
	Tokens map[string]map[string]int64 `json:"tokens,omitempty"`
	// ScheduledTokens is UTC day -> tokens of scheduled runs only, summed
	// over tasks: the unattended spending the daily token cap limits.
	ScheduledTokens map[string]int64 `json:"scheduledTokens,omitempty"`
	// PassError is why the latest run-due pass failed before running tasks.
	PassError *PassError `json:"passError,omitempty"`
	// Fetch is the latest fetch of the primary ref's remote.
	Fetch *FetchStatus `json:"fetch,omitempty"`
	// LastPass is when the latest run-due pass settled items (bookkeep).
	LastPass time.Time `json:"lastPass,omitzero"`
	Items    []Item    `json:"items,omitempty"`
	NextID   int       `json:"nextId,omitempty"`
	// Stopping is the run Stop was asked to end. Whichever process records
	// that run records it as stopped.
	Stopping string `json:"stopping,omitempty"`

	// Landings is each task's landing as the person last saw or approved
	// it; Widened holds a wider landing awaiting approval, which runs as
	// notify (SPEC-0127 US12).
	Landings map[string]Landing `json:"landings,omitempty"`
	Widened  map[string]Landing `json:"widened,omitempty"`
	// Leftover is the files each task's failed, stopped, or uncommitted
	// in-place run left, by task.
	Leftover map[string]Leftover `json:"leftoverFiles,omitempty"`
	// Made is the in-place commits Rhizome landed on this machine, newest
	// last, kept apart from the trimmed run history so commits that waited
	// while push was off are still recognised as Rhizome's (US13).
	Made []string `json:"madeCommits,omitempty"`

	// TokenCapNotified is the UTC day the daily token cap last stopped
	// work and notified, so it notifies once a day (SPEC-0127 US14).
	TokenCapNotified string `json:"tokenCapNotified,omitempty"`
	// PendingAlerts are notify-command calls waiting for delivery, oldest
	// first; DroppedAlerts counts older ones dropped to keep the bound.
	// PassAlert is the pass-level failure last alerted (US14).
	PendingAlerts []PendingAlert `json:"pendingAlerts,omitempty"`
	DroppedAlerts int            `json:"droppedAlerts,omitempty"`
	PassAlert     *PassAlert     `json:"passAlert,omitempty"`
}

// Leftover is the files an in-place run left in one checkout: path -> the
// content ("<mode> <object id>", "" when deleted). The task's next run
// adopts a path only in that checkout and only while its content is still
// that; otherwise the path is the person's.
type Leftover struct {
	// Checkout is the checkout's resolved path.
	Checkout string            `json:"checkout"`
	Files    map[string]string `json:"files"`
}

// PassError records a run-due pass that failed before running any task.
type PassError struct {
	At time.Time `json:"at"`
	// Code is PassUntrusted for a host pass in an untrusted repository.
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// PassUntrusted marks a host pass refused because the repository is not
// trusted; it is recorded once.
const PassUntrusted = "untrusted"

// FetchStatus is the latest fetch before a pass with due tasks. A failed
// fetch leaves the last fetched primary ref in use.
type FetchStatus struct {
	At    time.Time `json:"at"`
	Error string    `json:"error,omitempty"`
	// FailingSince is the first failure of the current run of failed
	// fetches; zero after a success.
	FailingSince time.Time `json:"failingSince,omitzero"`
}

// PausedAt reports whether background work is paused at now; a pause
// whose PausedUntil has passed is over.
func (s *State) PausedAt(now time.Time) bool {
	return s.Paused && (s.PausedUntil.IsZero() || now.Before(s.PausedUntil))
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
	r.Stage, r.Holder = "", ""
	// A skipped slot's Landed is another machine's commit, not Rhizome's.
	if r.Landing == taskdef.LandInPlace && r.Landed != "" && r.Outcome != OutcomeSkipped {
		s.noteMade(r.Landed)
	}
	if s.Stopping != "" && r.ID == s.Stopping {
		r.Outcome, r.Message, s.Stopping = OutcomeStopped, stoppedMessage, ""
	}
	if s.Running != nil && r.ID != "" && s.Running.ID == r.ID {
		s.Running = nil
	}
	if r.Test {
		s.Tests = append(s.Tests, r)
		return
	}
	if s.Last == nil {
		s.Last = map[string]RunRecord{}
	}
	// Seed before appending, so this run is not counted twice.
	s.seedTokens()
	s.Last[r.Task] = r
	s.History = append(s.History, r)
	if r.Tokens > 0 {
		s.addTokens(r.Task, r.At, r.Tokens)
	}
	if r.Scheduled && r.Tokens > 0 {
		if s.ScheduledTokens == nil {
			s.ScheduledTokens = map[string]int64{}
		}
		s.ScheduledTokens[tokenDay(r.At)] += r.Tokens
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
	for day := range s.ScheduledTokens {
		if day < cutoff {
			delete(s.ScheduledTokens, day)
		}
	}
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

const (
	interruptedMessage = "the run was interrupted before it finished"
	stoppedMessage     = "the run was stopped"
)

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

// maxMade bounds State.Made; a commit older than this many landings is long
// since pushed or abandoned.
const maxMade = 1000

// noteMade records an in-place commit Rhizome landed.
func (s *State) noteMade(c string) {
	if slices.Contains(s.Made, c) {
		return
	}
	s.Made = append(s.Made, c)
	if over := len(s.Made) - maxMade; over > 0 {
		s.Made = s.Made[over:]
	}
}
