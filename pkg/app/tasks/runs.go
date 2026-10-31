package tasks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const (
	runsDir        = "runs"
	checkLogFile   = "check.log"
	transcriptFile = "events.jsonl"
	maxCheckLog    = 1 << 20
	checkLogCut    = "\n[check log truncated]\n"

	keepTranscripts = 20 // per task: records, and events.jsonl files
	keepCheckLogs   = 20 // per task
	keepOtherRuns   = 100
	runFolderGrace  = 7 * 24 * time.Hour
	runIDTimeLayout = "20060102T150405Z"
)

// ErrInvalidRunID means a run ID is not in the YYYYMMDDTHHMMSSZ-<8 hex>
// form; it is checked before any path is built from it.
var ErrInvalidRunID = errors.New("invalid run ID")

var runIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)

// ValidRunID reports whether id is a well-formed run ID.
func ValidRunID(id string) bool { return runIDPattern.MatchString(id) }

func newRunID(now time.Time) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return now.UTC().Format(runIDTimeLayout) + "-" + hex.EncodeToString(b)
}

func runIDTime(id string) (time.Time, bool) {
	if !ValidRunID(id) {
		return time.Time{}, false
	}
	t, err := time.Parse(runIDTimeLayout, id[:len(runIDTimeLayout)])
	return t, err == nil
}

// runDir is a run's folder; the ID is validated first.
func (s *Service) runDir(id string) (string, error) {
	if !ValidRunID(id) {
		return "", ErrInvalidRunID
	}
	return filepath.Join(s.stateDir, runsDir, id), nil
}

func (s *Service) runFile(id, name string) string {
	dir, err := s.runDir(id)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, name)
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// attempt is one run in progress. Drafts get an ID and a folder but are
// never recorded, so they do not move the task's clock or token totals.
type attempt struct {
	id      string
	started time.Time
	draft   bool
}

func (s *Service) newAttempt(draft bool) attempt {
	now := s.opts.Now()
	return attempt{id: newRunID(now), started: now, draft: draft}
}

// record is the attempt's run record with the given outcome.
func (a attempt) record(task, outcome string, at time.Time) RunRecord {
	return RunRecord{ID: a.id, Task: task, StartedAt: a.started, At: at, Outcome: outcome}
}

// checkLog writes a check's stderr to its run folder, creating the file on
// the first byte. Past maxCheckLog it marks the cut and drops the rest. It
// never fails, so a noisy or unwritable log cannot fail the check.
type checkLog struct {
	path string
	f    *os.File
	left int
	done bool
}

func (s *Service) openCheckLog(a attempt) *checkLog {
	return &checkLog{path: s.runFile(a.id, checkLogFile), left: maxCheckLog}
}

func (l *checkLog) Write(p []byte) (int, error) {
	if l.done || len(p) == 0 {
		return len(p), nil
	}
	if l.f == nil {
		if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
			l.done = true
			return len(p), nil
		}
		f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			l.done = true
			return len(p), nil
		}
		l.f = f
	}
	if len(p) > l.left {
		_, _ = l.f.Write(p[:l.left])
		_, _ = l.f.Write([]byte(checkLogCut))
		l.done, l.left = true, 0
		return len(p), nil
	}
	l.left -= len(p)
	_, _ = l.f.Write(p)
	return len(p), nil
}

func (l *checkLog) Close() {
	if l.f != nil {
		_ = l.f.Close()
	}
}

// begin marks the attempt as the running one. Drafts are never Running,
// because an interrupted Running is recorded.
func (s *Service) begin(ctx context.Context, a attempt, r RunRecord, fn func(*State)) error {
	return s.store.Update(ctx, func(st *State) error {
		if !a.draft {
			r.Stage = StageCheck
			st.Running = &r
		}
		if fn != nil {
			fn(st)
		}
		return nil
	})
}

// end records this attempt as failed when it left Running set without a
// record, as when saving its result failed, so the run is not lost. If this
// save fails too, Running stays and the next pass records it as
// interrupted. A crash skips end, which is the point.
func (s *Service) end(ctx context.Context, a attempt) {
	st, err := s.store.Load()
	if err != nil || st.Running == nil || st.Running.ID != a.id {
		return
	}
	_ = s.store.Update(context.WithoutCancel(ctx), func(st *State) error {
		if st.Running != nil && st.Running.ID == a.id {
			r := *st.Running
			r.At, r.Outcome = s.opts.Now(), OutcomeFailed
			r.Message = "the run ended without saving its result"
			if it, err := st.Item(r.Item); r.Item != "" && err == nil && it.State == StateRunning {
				// Keep it retryable and discardable, with its worktree.
				it.set(StateAttention, r.At)
				it.Reason = r.Message
			}
			st.Record(r)
		}
		return nil
	})
}

// running updates the Running record when it is this attempt.
func running(st *State, a attempt, fn func(*RunRecord)) {
	if st.Running != nil && st.Running.ID == a.id {
		fn(st.Running)
	}
}

// finalizeInterrupted records a Running left by a process that ended
// mid-run as one interrupted run, keeping its folder, and moves its item to
// needs attention. Callers hold run.lock, so nothing else is running.
func (s *Service) finalizeInterrupted(ctx context.Context) error {
	return s.store.Update(ctx, func(st *State) error {
		if st.Running == nil {
			return nil
		}
		now := s.opts.Now()
		r := *st.Running
		r.At, r.Outcome = now, OutcomeInterrupted
		r.Message = "the run was interrupted before it finished"
		if r.Item != "" {
			// A manual Run's item is still due or needs attention during
			// its check; done and dismissed items stay closed.
			if it, err := st.Item(r.Item); err == nil {
				switch it.State {
				case StateRunning, StateDue, StateAttention:
					it.interrupt(now)
				}
			}
		}
		st.Record(r)
		st.Running = nil
		return nil
	})
}

// retain bounds history and run folders. Callers hold run.lock. Per task it
// keeps the newest keepTranscripts records with a transcript and
// keepOtherRuns others, and the newest transcripts and check logs; other
// payloads, empty folders, and unreferenced folders past the grace period
// (drafts among them) are deleted. Token buckets keep 30 days.
func (s *Service) retain(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)
	_ = os.RemoveAll(filepath.Join(s.stateDir, "logs")) // phase 1 layout
	referenced := map[string]bool{}
	// Without a saved history, referenced is incomplete and the folder sweep
	// below would delete recorded runs.
	if err := s.store.Update(ctx, func(st *State) error {
		st.seedTokens()
		st.trimTokens(s.opts.Now())
		transcripts, logs := map[string]int{}, map[string]int{}
		withTranscript, others := map[string]int{}, map[string]int{}
		kept := make([]RunRecord, 0, len(st.History))
		for i := len(st.History) - 1; i >= 0; i-- {
			r := st.History[i]
			if path := s.runFile(r.ID, transcriptFile); fileExists(path) {
				if transcripts[r.Task]++; transcripts[r.Task] > keepTranscripts {
					_ = os.Remove(path)
				}
			}
			if path := s.runFile(r.ID, checkLogFile); fileExists(path) {
				if logs[r.Task]++; logs[r.Task] > keepCheckLogs {
					_ = os.Remove(path)
				}
			}
			if fileExists(s.runFile(r.ID, transcriptFile)) {
				if withTranscript[r.Task]++; withTranscript[r.Task] > keepTranscripts {
					continue
				}
			} else if others[r.Task]++; others[r.Task] > keepOtherRuns {
				continue
			}
			kept = append(kept, r)
			referenced[r.ID] = true
		}
		for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
			kept[i], kept[j] = kept[j], kept[i]
		}
		st.History = kept
		if st.Running != nil {
			referenced[st.Running.ID] = true
		}
		return nil
	}); err != nil {
		return
	}
	root := filepath.Join(s.stateDir, runsDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	now := s.opts.Now()
	for _, e := range entries {
		at, ok := runIDTime(e.Name())
		if !ok || !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if !referenced[e.Name()] && now.Sub(at) > runFolderGrace {
			_ = os.RemoveAll(dir)
			continue
		}
		if files, err := os.ReadDir(dir); err == nil && len(files) == 0 {
			_ = os.Remove(dir)
		}
	}
}
