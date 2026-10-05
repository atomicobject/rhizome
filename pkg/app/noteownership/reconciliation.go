package noteownership

import (
	"context"
	"fmt"
	"sort"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
)

// SnapshotStore is the injected read port used by application adapters.
// This package does not open stores or perform direct durable mutation.
type SnapshotStore interface {
	NotePaths(context.Context) ([]string, error)
	IndexedFilePaths(context.Context) ([]string, error)
	PendingOwnershipReconciliation(context.Context) (int64, bool, error)
}

// MutationStore is the injected ownership mutation port. Coordinators retain
// queue and lock ownership; this interface permits bootstrap to use the same
// planning DTOs without importing indexing.
type MutationStore interface {
	ApplyOwnershipTransitions(context.Context, []semdb.OwnershipTransition) (semdb.OwnershipTransitionResult, error)
	AcknowledgeOwnershipReconciliation(context.Context, int64) (bool, error)
}

// ReconciliationTracker records exact-generation work without store access.
type ReconciliationTracker struct {
	required   bool
	generation int64
	paths      map[string]struct{}
	anchorIDs  map[int64]struct{}
}

func NewReconciliationTracker(generation int64, pending bool) (*ReconciliationTracker, error) {
	if generation < 0 {
		return nil, fmt.Errorf("ownership reconciliation generation must not be negative: %d", generation)
	}
	if pending && generation == 0 {
		return nil, fmt.Errorf("pending ownership reconciliation requires a positive generation")
	}
	return &ReconciliationTracker{required: pending, generation: generation, paths: map[string]struct{}{}, anchorIDs: map[int64]struct{}{}}, nil
}
func (t *ReconciliationTracker) Observe(result semdb.OwnershipTransitionResult) error {
	if result.ReconciliationGeneration < 0 {
		return fmt.Errorf("ownership reconciliation generation must not be negative: %d", result.ReconciliationGeneration)
	}
	if result.ReconciliationGeneration == 0 {
		if len(result.AffectedSourcePaths) != 0 || len(result.AffectedAnchorIDs) != 0 || len(result.TransitionedPaths) != 0 {
			return fmt.Errorf("ownership reconciliation result has affected artifacts without a generation")
		}
		return nil
	}
	if result.ReconciliationGeneration < t.generation {
		return fmt.Errorf("ownership reconciliation generation regressed from %d to %d", t.generation, result.ReconciliationGeneration)
	}
	t.required = true
	t.generation = result.ReconciliationGeneration
	for _, path := range result.AffectedSourcePaths {
		if path != "" {
			t.paths[path] = struct{}{}
		}
	}
	for _, id := range result.AffectedAnchorIDs {
		if id > 0 {
			t.anchorIDs[id] = struct{}{}
		}
	}
	return nil
}
func (t *ReconciliationTracker) Required() bool { return t != nil && t.required }
func (t *ReconciliationTracker) AcknowledgementGeneration() int64 {
	if t == nil || !t.required {
		return 0
	}
	return t.generation
}
func (t *ReconciliationTracker) AffectedSourcePaths() []string {
	if t == nil {
		return nil
	}
	result := make([]string, 0, len(t.paths))
	for path := range t.paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}
func (t *ReconciliationTracker) AffectedAnchorIDs() []int64 {
	if t == nil {
		return nil
	}
	result := make([]int64, 0, len(t.anchorIDs))
	for id := range t.anchorIDs {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
