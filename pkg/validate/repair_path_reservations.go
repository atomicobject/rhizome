package validate

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// RepairPathCoordinator is the shared server admission boundary for manual
// drafts and repair applies. Both sides reserve through one mutex, establishing
// manual-session admission before repair reservation before vault write lock.
type RepairPathCoordinator struct {
	mu      sync.Mutex
	manual  map[string]map[string]map[string]string
	repairs map[string]map[string]int
}

func NewRepairPathCoordinator() *RepairPathCoordinator {
	return &RepairPathCoordinator{
		manual:  make(map[string]map[string]map[string]string),
		repairs: make(map[string]map[string]int),
	}
}

// SetManualPaths atomically admits the complete projected path set for a
// manual session. A failed replacement leaves its previous reservation intact.
func (c *RepairPathCoordinator) SetManualPaths(vault, sessionID string, paths []string) error {
	if c == nil {
		return fmt.Errorf("repair path coordinator is unavailable")
	}
	vault = strings.TrimSpace(vault)
	sessionID = strings.TrimSpace(sessionID)
	if vault == "" || sessionID == "" {
		return fmt.Errorf("manual path reservation requires vault and session identities")
	}
	normalized := reservationPathSet(paths)
	c.mu.Lock()
	defer c.mu.Unlock()
	var overlap []string
	for key, display := range normalized {
		if c.repairs[vault][key] > 0 {
			overlap = append(overlap, display)
		}
	}
	if len(overlap) > 0 {
		reviewErr := newRepairReviewError(RepairReviewErrorManualOverlap, "a repair is applying to these paths; retry the manual change after it completes")
		reviewErr.OverlappingPaths = sortedUnique(overlap)
		return reviewErr
	}
	if c.manual[vault] == nil {
		c.manual[vault] = make(map[string]map[string]string)
	}
	if len(normalized) == 0 {
		delete(c.manual[vault], sessionID)
	} else {
		c.manual[vault][sessionID] = normalized
	}
	return nil
}

func (c *RepairPathCoordinator) ReleaseManualSession(vault, sessionID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	sessions := c.manual[strings.TrimSpace(vault)]
	delete(sessions, strings.TrimSpace(sessionID))
	if len(sessions) == 0 {
		delete(c.manual, strings.TrimSpace(vault))
	}
}

func (c *RepairPathCoordinator) ReserveRepairPaths(_ context.Context, vault string, paths []string) (RepairPathReservation, error) {
	if c == nil {
		return nil, fmt.Errorf("repair path coordinator is unavailable")
	}
	vault = strings.TrimSpace(vault)
	if vault == "" {
		return nil, fmt.Errorf("repair path reservation requires a vault identity")
	}
	normalized := reservationPathSet(paths)
	c.mu.Lock()
	defer c.mu.Unlock()
	var overlap []string
	for _, sessionPaths := range c.manual[vault] {
		for key, display := range normalized {
			if _, ok := sessionPaths[key]; ok {
				overlap = append(overlap, display)
			}
		}
	}
	if len(overlap) > 0 {
		reviewErr := newRepairReviewError(RepairReviewErrorManualOverlap, "manual drafts overlap this repair; save or discard them, then revalidate")
		reviewErr.OverlappingPaths = sortedUnique(overlap)
		return nil, reviewErr
	}
	if c.repairs[vault] == nil {
		c.repairs[vault] = make(map[string]int)
	}
	keys := make([]string, 0, len(normalized))
	for key := range normalized {
		c.repairs[vault][key]++
		keys = append(keys, key)
	}
	return &repairPathReservation{coordinator: c, vault: vault, keys: keys}, nil
}

type repairPathReservation struct {
	once        sync.Once
	coordinator *RepairPathCoordinator
	vault       string
	keys        []string
}

func (r *repairPathReservation) Release() {
	if r == nil || r.coordinator == nil {
		return
	}
	r.once.Do(func() {
		r.coordinator.mu.Lock()
		defer r.coordinator.mu.Unlock()
		for _, key := range r.keys {
			r.coordinator.repairs[r.vault][key]--
			if r.coordinator.repairs[r.vault][key] <= 0 {
				delete(r.coordinator.repairs[r.vault], key)
			}
		}
		if len(r.coordinator.repairs[r.vault]) == 0 {
			delete(r.coordinator.repairs, r.vault)
		}
	})
}

func reservationPathSet(paths []string) map[string]string {
	set := make(map[string]string, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path != "" {
			set[repairCollisionKey(path)] = path
		}
	}
	return set
}

var _ RepairPathReservationSource = (*RepairPathCoordinator)(nil)
