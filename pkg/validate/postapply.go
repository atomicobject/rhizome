package validate

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/sourcetxn"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// PathRename records one committed source-to-destination move.
type PathRename = sourcetxn.Rename

// PostApplyTiming is one deterministic refresh phase duration.
type PostApplyTiming struct {
	Phase      string `json:"phase"`
	DurationMs int64  `json:"durationMs"`
}

// PostApplyRefreshResult reports exact read-model convergence work.
type PostApplyRefreshResult struct {
	Domains    []string          `json:"domains,omitempty"`
	Paths      []string          `json:"paths,omitempty"`
	Timings    []PostApplyTiming `json:"timings,omitempty"`
	Runtime    *ontology.Runtime `json:"-"`
	closeState *postApplyCloseState
}

type postApplyCloseState struct {
	once sync.Once
	fn   func() error
	err  error
}

// NewPostApplyRefreshResult transfers ownership of a prepared runtime from an
// idempotent owner without exposing its raw cleanup function.
func NewPostApplyRefreshResult(
	runtime *ontology.Runtime,
	owner interface{ Close() error },
) PostApplyRefreshResult {
	result := PostApplyRefreshResult{Runtime: runtime}
	if owner != nil {
		result.closeState = &postApplyCloseState{fn: owner.Close}
	}
	return result
}

// Close releases the prepared runtime exactly once. The repair engine calls it
// after in-lease post-apply validation finishes.
func (r *PostApplyRefreshResult) Close() error {
	if r == nil {
		return nil
	}
	if r.closeState == nil {
		return nil
	}
	r.closeState.once.Do(func() {
		if r.closeState.fn != nil {
			r.closeState.err = r.closeState.fn()
		}
	})
	return r.closeState.err
}

func (r *PostApplyRefreshResult) validateRuntimeOwnership() error {
	if r == nil || r.Runtime == nil {
		return fmt.Errorf("prepared post-apply runtime is absent")
	}
	if r.closeState == nil {
		return fmt.Errorf("prepared post-apply runtime has no cleanup owner")
	}
	return nil
}

// PostApplyRefresher converges read models while borrowing the repair engine's
// held lease. Implementations must never acquire or release the index lock.
type PostApplyRefresher interface {
	Refresh(
		context.Context,
		*indexlock.Lease,
		[]string,
		[]PathRename,
		[]string,
	) (PostApplyRefreshResult, error)
}

// PostApplyRefreshScope retains transaction-local changed-path ownership while
// also exposing the complete committed delta for shared projections.
type PostApplyRefreshScope struct {
	Checks         []string
	Changed        []string
	Renamed        []PathRename
	Deleted        []string
	ChangedByCheck map[string][]string
}

// ChangedForCheck returns only changed paths from transactions owned by check.
func (s PostApplyRefreshScope) ChangedForCheck(check string) []string {
	return append([]string(nil), s.ChangedByCheck[check]...)
}

func (s *PostApplyRefreshScope) add(checks, changed []string, renamed []PathRename, deleted []string) {
	if s.ChangedByCheck == nil {
		s.ChangedByCheck = map[string][]string{}
	}
	checks = sortedUnique(checks)
	changed = sortedUnique(changed)
	for _, check := range checks {
		s.ChangedByCheck[check] = append(s.ChangedByCheck[check], changed...)
	}
	s.Checks = append(s.Checks, checks...)
	s.Changed = append(s.Changed, changed...)
	s.Renamed = append(s.Renamed, renamed...)
	s.Deleted = append(s.Deleted, deleted...)
}

func (s PostApplyRefreshScope) normalized() PostApplyRefreshScope {
	s.Checks = sortedUnique(s.Checks)
	s.Changed = sortedUnique(s.Changed)
	s.Renamed = sortedUniqueRenames(s.Renamed)
	s.Deleted = sortedUnique(s.Deleted)
	for check, changed := range s.ChangedByCheck {
		s.ChangedByCheck[check] = sortedUnique(changed)
	}
	return s
}

// scopedPostApplyRefresher optionally receives exact per-transaction scope so
// derived-index refreshers do not claim or fail work owned by unrelated checks.
type scopedPostApplyRefresher interface {
	RefreshScoped(
		context.Context,
		*indexlock.Lease,
		PostApplyRefreshScope,
	) (PostApplyRefreshResult, error)
}

func refreshPostApply(
	ctx context.Context,
	refresher PostApplyRefresher,
	lease *indexlock.Lease,
	scope PostApplyRefreshScope,
) (PostApplyRefreshResult, error) {
	scope = scope.normalized()
	if scoped, ok := refresher.(scopedPostApplyRefresher); ok {
		return scoped.RefreshScoped(ctx, lease, scope)
	}
	return refresher.Refresh(ctx, lease, scope.Changed, scope.Renamed, scope.Deleted)
}

// repairPostApplyScope is the exact committed or recovered transaction scope
// that must be validated before durable repair evidence becomes cleanup-eligible.
type repairPostApplyScope struct {
	Checks        []string
	AffectedPaths []string
}

// repairPostApplyCheck is the engine-owned verification seam. It runs after
// filesystem verification and read-model refresh, while the lease is held.
type repairPostApplyCheck func(context.Context, *indexlock.Lease, *ontology.Runtime, repairPostApplyScope) error

func newRepairPostApplyScope(checks, changed, deleted []string, renamed []PathRename) repairPostApplyScope {
	paths := append(append([]string(nil), changed...), deleted...)
	for _, rename := range renamed {
		paths = append(paths, rename.From, rename.To)
	}
	return repairPostApplyScope{Checks: sortedUnique(checks), AffectedPaths: sortedUnique(paths)}
}

func repairManifestPostApplyPaths(manifest repairJournalManifest) []string {
	paths := append(append([]string(nil), manifest.Changed...), manifest.Deleted...)
	for _, rename := range manifest.Renamed {
		paths = append(paths, rename.From, rename.To)
	}
	for _, entry := range manifest.Entries {
		if entry.Internal {
			continue
		}
		paths = append(paths, entry.Path, entry.OriginalPath)
	}
	return sortedUnique(paths)
}

func acquireRepairIndexLockLease(lockPath string) (*indexlock.Lease, func() error, error) {
	root := filepath.Dir(filepath.Dir(lockPath))
	lease, acquired, err := indexlock.TryAcquireLease(root, indexlock.AcquireOptions{Role: "interactive/repair"})
	if err != nil {
		return nil, nil, err
	}
	if !acquired {
		return nil, nil, fmt.Errorf("index lock is held: %s", lockPath)
	}
	return repairLeaseHeartbeat(lease)
}

func repairLeaseHeartbeat(lease *indexlock.Lease) (*indexlock.Lease, func() error, error) {
	stop := lease.StartHeartbeat(context.Background(), 30*time.Second)
	var once sync.Once
	var releaseErr error
	return lease, func() error { once.Do(func() { stop(); releaseErr = lease.Release() }); return releaseErr }, nil
}
