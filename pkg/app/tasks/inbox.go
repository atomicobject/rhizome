package tasks

import (
	"fmt"
	"time"
)

// ItemState is where an inbox item stands (SPEC-0126 US6).
type ItemState string

const (
	StateDue       ItemState = "due"
	StateRunning   ItemState = "running"
	StateReady     ItemState = "ready"
	StateStarted   ItemState = "started"
	StateAttention ItemState = "needs-attention"
	StateDone      ItemState = "done"
	StateDismissed ItemState = "dismissed"
)

// Item kinds.
const (
	KindTask         = "task"
	KindAnnouncement = "announcement"
)

// Item is a task that needs the person, or a notice about a new task.
type Item struct {
	ID       string    `json:"id"`
	Task     string    `json:"task"`
	Kind     string    `json:"kind"`
	State    ItemState `json:"state"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Snoozed  time.Time `json:"snoozedUntil,omitzero"`
	Summary  string    `json:"summary,omitempty"`
	Context  string    `json:"context,omitempty"`
	Commit   string    `json:"commit,omitempty"`
	Branch   string    `json:"branch,omitempty"`
	Worktree string    `json:"worktree,omitempty"`
	Message  string    `json:"message,omitempty"`
	Changed  []string  `json:"changed,omitempty"`
	Tokens   int64     `json:"tokens,omitempty"`
	Reason   string    `json:"reason,omitempty"`
}

// Active reports whether the item still needs the person or is in progress.
func (it Item) Active() bool { return it.State != StateDone && it.State != StateDismissed }

// Visible reports whether the inbox shows the item at now.
func (it Item) Visible(now time.Time) bool { return it.Active() && !now.Before(it.Snoozed) }

// ActiveTaskItem returns the task's open work item, if any.
func (s *State) ActiveTaskItem(task string) *Item {
	for i := range s.Items {
		if it := &s.Items[i]; it.Task == task && it.Kind == KindTask && it.Active() {
			return it
		}
	}
	return nil
}

// Item finds an item by id.
func (s *State) Item(id string) (*Item, error) {
	for i := range s.Items {
		if s.Items[i].ID == id {
			return &s.Items[i], nil
		}
	}
	return nil, fmt.Errorf("no inbox item %q", id)
}

// AddItem appends a new item and returns it.
func (s *State) AddItem(it Item, now time.Time) *Item {
	it.ID = s.newItemID()
	it.Created, it.Updated = now, now
	s.Items = append(s.Items, it)
	return &s.Items[len(s.Items)-1]
}

// set moves an item to a new state.
func (it *Item) set(state ItemState, now time.Time) {
	it.State, it.Updated = state, now
}

// pruneItems drops finished items older than a month, keeping the file
// bounded.
func (s *State) pruneItems(now time.Time) {
	kept := s.Items[:0]
	for _, it := range s.Items {
		if it.Active() || now.Sub(it.Updated) < 30*24*time.Hour {
			kept = append(kept, it)
		}
	}
	s.Items = kept
}
