package bootstrap

import (
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/cache"
)

// LiveDirtyPath records one path participating in a live watcher epoch.
type LiveDirtyPath struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// LiveEpoch is the deterministic status record for one live watcher batch.
type LiveEpoch struct {
	ID              int64           `json:"id"`
	Status          string          `json:"status"`
	StartedAt       time.Time       `json:"startedAt"`
	CompletedAt     *time.Time      `json:"completedAt,omitempty"`
	Phase           string          `json:"phase"`
	DirtyPaths      []LiveDirtyPath `json:"dirtyPaths,omitempty"`
	DeletedPaths    []string        `json:"deletedPaths,omitempty"`
	Error           string          `json:"error,omitempty"`
	DegradedReasons []string        `json:"degradedReasons,omitempty"`
}

// LiveSchedulerHealth summarizes live scheduler backlog without exposing internals.
type LiveSchedulerHealth struct {
	EmbeddingPendingNotes int    `json:"embeddingPendingNotes"`
	EmbeddingPendingCode  bool   `json:"embeddingPendingCode"`
	EmbeddingRunning      bool   `json:"embeddingRunning"`
	EmbeddingBackoff      string `json:"embeddingBackoff,omitempty"`
	GraphPending          bool   `json:"graphPending"`
	GraphRunning          bool   `json:"graphRunning"`
	GraphLockContention   bool   `json:"graphLockContention"`
}

// LiveHealth is the status payload surfaced by web status APIs.
type LiveHealth struct {
	Role               string              `json:"role"`
	WatcherMode        string              `json:"watcherMode"`
	PendingDirtyCount  int                 `json:"pendingDirtyCount"`
	Scheduler          LiveSchedulerHealth `json:"scheduler"`
	CurrentEpoch       *LiveEpoch          `json:"currentEpoch,omitempty"`
	LastCompletedEpoch *LiveEpoch          `json:"lastCompletedEpoch,omitempty"`
	FailedPhase        string              `json:"failedPhase,omitempty"`
	Error              string              `json:"error,omitempty"`
	DegradedReasons    []string            `json:"degradedReasons,omitempty"`
}

type liveHealthTracker struct {
	mu      sync.Mutex
	nextID  int64
	current *LiveEpoch
	last    *LiveEpoch
}

func newLiveHealthTracker(_ string) *liveHealthTracker {
	return &liveHealthTracker{}
}

func (t *liveHealthTracker) begin(dirty map[string]cache.DirtyKind, resync bool) *LiveEpoch {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextID++
	epoch := &LiveEpoch{
		ID:         t.nextID,
		Status:     "started",
		StartedAt:  time.Now().UTC(),
		Phase:      "cache",
		DirtyPaths: liveDirtyPaths(dirty, resync),
	}
	for _, p := range epoch.DirtyPaths {
		switch p.Kind {
		case string(cache.DirtyRemoved), string(cache.DirtyRenamed):
			epoch.DeletedPaths = append(epoch.DeletedPaths, p.Path)
		}
	}
	t.current = cloneLiveEpoch(epoch)
	return cloneLiveEpoch(epoch)
}

func (t *liveHealthTracker) phase(id int64, phase string) {
	t.update(id, func(epoch *LiveEpoch) {
		epoch.Phase = phase
	})
}

func (t *liveHealthTracker) degrade(id int64, reason string) {
	if reason == "" {
		return
	}
	t.update(id, func(epoch *LiveEpoch) {
		if !stringSliceContains(epoch.DegradedReasons, reason) {
			epoch.DegradedReasons = append(epoch.DegradedReasons, reason)
		}
		if epoch.Status == "started" {
			epoch.Status = "degraded"
		}
	})
}

func (t *liveHealthTracker) fail(id int64, phase string, err error) {
	if err == nil {
		return
	}
	t.update(id, func(epoch *LiveEpoch) {
		epoch.Status = "failed"
		epoch.Phase = phase
		epoch.Error = err.Error()
		now := time.Now().UTC()
		epoch.CompletedAt = &now
	})
}

func (t *liveHealthTracker) complete(id int64) {
	t.update(id, func(epoch *LiveEpoch) {
		if epoch.Status == "failed" {
			return
		}
		if len(epoch.DegradedReasons) > 0 {
			epoch.Status = "degraded"
		} else {
			epoch.Status = "completed"
		}
		epoch.Phase = "complete"
		now := time.Now().UTC()
		epoch.CompletedAt = &now
	})
}

func (t *liveHealthTracker) snapshot() (current, last *LiveEpoch) {
	if t == nil {
		return nil, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return cloneLiveEpoch(t.current), cloneLiveEpoch(t.last)
}

func (t *liveHealthTracker) update(id int64, mutate func(*LiveEpoch)) {
	if t == nil || mutate == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current == nil || t.current.ID != id {
		return
	}
	mutate(t.current)
	if t.current.Status == "completed" || t.current.Status == "failed" || t.current.Status == "degraded" {
		t.last = cloneLiveEpoch(t.current)
		if t.current.CompletedAt != nil {
			t.current = nil
		}
	}
}

func liveDirtyPaths(dirty map[string]cache.DirtyKind, resync bool) []LiveDirtyPath {
	out := make([]LiveDirtyPath, 0, len(dirty))
	for path, kind := range dirty {
		out = append(out, LiveDirtyPath{Path: filepath.ToSlash(path), Kind: string(kind)})
	}
	if resync {
		out = append(out, LiveDirtyPath{Path: "*", Kind: "resync"})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func cloneLiveEpoch(in *LiveEpoch) *LiveEpoch {
	if in == nil {
		return nil
	}
	out := *in
	out.DirtyPaths = append([]LiveDirtyPath(nil), in.DirtyPaths...)
	out.DeletedPaths = append([]string(nil), in.DeletedPaths...)
	out.DegradedReasons = append([]string(nil), in.DegradedReasons...)
	if in.CompletedAt != nil {
		completed := *in.CompletedAt
		out.CompletedAt = &completed
	}
	return &out
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
