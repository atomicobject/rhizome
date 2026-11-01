package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

const recentRuns = 50

// ErrRunNotFound means no recorded or running run has the ID.
var ErrRunNotFound = errors.New("no such run")

// LiveState is what the Tasks page polls while a run is in progress.
type LiveState struct {
	Paused  bool       `json:"paused"`
	Running *RunRecord `json:"running,omitempty"`
	// Stale means Running is set but the run.lock holder is gone: its
	// process died, or it has a different runtime identity.
	Stale  bool        `json:"stale,omitempty"`
	Items  []Item      `json:"items"`
	Recent []RunRecord `json:"recent"`
}

// RunView is a run with what its folder holds.
type RunView struct {
	RunRecord
	HasTranscript bool `json:"hasTranscript"`
	HasCheckLog   bool `json:"hasCheckLog"`
}

// RunDetail is one run with its transcript lines after an offset.
type RunDetail struct {
	Run    RunView           `json:"run"`
	Events []json.RawMessage `json:"events"`
	// Next is the byte offset to resume reading the transcript from.
	Next int64 `json:"next"`
	// CheckLog is sent only when reading from offset 0.
	CheckLog string `json:"checkLog,omitempty"`
}

// LiveState reads state.json and the run.lock holder. It runs no git.
// Items are every open item, snoozed ones included.
func (s *Service) LiveState(ctx context.Context) (LiveState, error) {
	st, err := s.store.Load()
	if err != nil {
		return LiveState{}, err
	}
	live := LiveState{Paused: st.Paused, Running: st.Running, Items: []Item{}, Recent: []RunRecord{}}
	for _, it := range st.Items {
		if it.Active() {
			live.Items = append(live.Items, it)
		}
	}
	for i := len(st.History) - 1; i >= 0 && len(live.Recent) < recentRuns; i-- {
		live.Recent = append(live.Recent, st.History[i])
	}
	if st.Running != nil {
		live.Stale = !runLockHeld(filepath.Join(s.stateDir, runLock))
	}
	return live, nil
}

// runLockHeld reports whether a live process on this host and boot holds
// the lock. Unreadable metadata on an existing file counts as held.
func runLockHeld(path string) bool {
	data, ok := indexlock.ReadLockData(path)
	if !ok {
		_, err := os.Stat(path)
		return !errors.Is(err, os.ErrNotExist)
	}
	id, err := indexlock.RuntimeIdentity()
	if err != nil || data.Runtime != id {
		return false
	}
	return indexlock.PIDExists(data.PID)
}

// Runs lists a task's runs newest first, the one in progress first of all;
// an empty task lists every task's.
func (s *Service) Runs(task string) ([]RunView, error) {
	st, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	out := []RunView{}
	if st.Running != nil && (task == "" || st.Running.Task == task) {
		out = append(out, s.view(*st.Running))
	}
	for i := len(st.History) - 1; i >= 0; i-- {
		if r := st.History[i]; task == "" || r.Task == task {
			out = append(out, s.view(r))
		}
	}
	return out, nil
}

func (s *Service) view(r RunRecord) RunView {
	return RunView{
		RunRecord:     r,
		HasTranscript: fileExists(s.runFile(r.ID, transcriptFile)),
		HasCheckLog:   fileExists(s.runFile(r.ID, checkLogFile)),
	}
}

// RunDetail returns a recorded or running run with its transcript lines
// from byte offset after, clamped to the file, and its check log when after
// is 0. It returns ErrInvalidRunID or ErrRunNotFound.
func (s *Service) RunDetail(id string, after int64) (RunDetail, error) {
	if !ValidRunID(id) {
		return RunDetail{}, ErrInvalidRunID
	}
	st, err := s.store.Load()
	if err != nil {
		return RunDetail{}, err
	}
	var found *RunRecord
	if st.Running != nil && st.Running.ID == id {
		found = st.Running
	}
	for i := len(st.History) - 1; found == nil && i >= 0; i-- {
		if st.History[i].ID == id {
			found = &st.History[i]
		}
	}
	if found == nil {
		return RunDetail{}, ErrRunNotFound
	}
	detail := RunDetail{Run: s.view(*found), Events: []json.RawMessage{}}
	if detail.Run.HasTranscript {
		events, next, err := ReadTranscript(s.runFile(id, transcriptFile), after)
		if err != nil {
			return RunDetail{}, err
		}
		detail.Events, detail.Next = append(detail.Events, events...), next
	}
	if after <= 0 && detail.Run.HasCheckLog {
		data, err := os.ReadFile(s.runFile(id, checkLogFile))
		if err != nil {
			return RunDetail{}, err
		}
		detail.CheckLog = string(data)
	}
	return detail, nil
}
