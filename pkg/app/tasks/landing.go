package tasks

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

// Landing is a task's land mode and write patterns (SPEC-0127 US12).
type Landing struct {
	Land   string   `json:"land"`
	Writes []string `json:"writes,omitempty"`
}

func landingOf(def Definition) Landing { return Landing{Land: def.Land, Writes: def.Writes} }

// widenedBy reports whether next lands somewhere l did not: in place after
// pr, or with a write pattern l did not list.
// ponytail: patterns compare as strings, so a new pattern that an old one
// already covers (`docs/a/**` under `docs/**`) still asks for approval.
func (l Landing) widenedBy(next Landing) bool {
	if next.Land != taskdef.LandInPlace {
		return false
	}
	if l.Land != taskdef.LandInPlace {
		return true
	}
	for _, p := range next.Writes {
		if !slices.Contains(l.Writes, p) {
			return true
		}
	}
	return false
}

func (l Landing) equal(o Landing) bool { return l.Land == o.Land && slices.Equal(l.Writes, o.Writes) }

// describe finishes "It ..." for inbox text.
func (l Landing) describe() string {
	if l.Land == taskdef.LandInPlace {
		return "lands in place in your checkout, committing only " + strings.Join(l.Writes, ", ")
	}
	return "lands on a branch in its own worktree"
}

// reviewLandings records the landing of tasks seen for the first time, and
// of tasks whose landing did not widen; a widened one waits in Widened,
// with one announcement, until the person approves it. A task seen before
// landings were recorded counts as pr, the only landing there was.
func reviewLandings(st *State, defs []Definition, now time.Time) {
	if st.Landings == nil {
		st.Landings = map[string]Landing{}
	}
	if st.Widened == nil {
		st.Widened = map[string]Landing{}
	}
	for _, def := range defs {
		next := landingOf(def)
		prev, known := st.Landings[def.ID]
		if _, seen := st.Seen[def.ID]; seen && !known {
			prev, known = Landing{Land: taskdef.LandPR}, true
		}
		if !known || !prev.widenedBy(next) {
			st.Landings[def.ID] = next
			delete(st.Widened, def.ID)
			continue
		}
		if pending, ok := st.Widened[def.ID]; ok && pending.equal(next) {
			continue
		}
		st.Widened[def.ID] = next
		st.AddItem(Item{Task: def.ID, Kind: KindAnnouncement, State: StateDue, Summary: fmt.Sprintf(
			"Task %q now %s. It runs as notify until you approve it with `rzm tasks approve %s`.", def.Name, next.describe(), def.ID)}, now)
	}
}

// landingMode is the mode a task runs in: notify while its widened landing
// waits for approval.
func (st *State) landingMode(task string, mode Mode) Mode {
	if _, ok := st.Widened[task]; ok && mode == ModeAuto {
		return ModeNotify
	}
	return mode
}

// Approve accepts the widened landing the person was told about, so the
// task runs in its own mode again, and closes the task's announcements. It
// approves only the landing recorded with the notice: when the task file
// on the primary ref now lands differently, it raises a notice for the new
// landing and refuses, so nothing is approved unseen.
func (s *Service) Approve(ctx context.Context, task string) error {
	_, _, def, err := s.currentDefinition(ctx, task)
	if err != nil {
		return err
	}
	unseen := false
	if err := s.store.Update(ctx, func(st *State) error {
		next := landingOf(def)
		if pending, ok := st.Widened[def.ID]; !ok || !pending.equal(next) {
			reviewLandings(st, []Definition{def}, s.opts.Now())
			_, unseen = st.Widened[def.ID]
			return nil
		}
		if st.Landings == nil {
			st.Landings = map[string]Landing{}
		}
		st.Landings[def.ID] = next
		delete(st.Widened, def.ID)
		for i := range st.Items {
			if it := &st.Items[i]; it.Task == def.ID && it.Kind == KindAnnouncement && it.Active() {
				it.set(StateDone, s.opts.Now())
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if unseen {
		return fmt.Errorf("task %s now %s, which differs from the landing you were asked to approve; read the new inbox note, then run `rzm tasks approve %s` again", task, landingOf(def).describe(), task)
	}
	return nil
}

// protectedPrefixes hold agent instructions and Rhizome configuration,
// including task files; an in-place run may change them only through a
// writes pattern that names the folder itself, so `**` does not count.
var protectedPrefixes = []string{".agents/", ".rhizome/"}

func protected(def Definition, path string) bool {
	for _, prefix := range protectedPrefixes {
		// `.Agents/` is `.agents/` on a case-insensitive file system.
		if !strings.HasPrefix(strings.ToLower(path), prefix) {
			continue
		}
		for _, p := range def.Writes {
			if strings.HasPrefix(p, prefix) && (Definition{Writes: []string{p}}).MayWrite(path) {
				return false
			}
		}
		return true
	}
	return false
}

// slotOf names the scheduled slot a run of def at now is for, or "" for an
// unscheduled run, whose slot is its run ID. Integration seam: the
// wall-clock schedule (SPEC-0127 US9) supplies the slot time as RFC3339.
func (s *Service) slotOf(def Definition, now time.Time) string {
	if s.slotFor != nil {
		return s.slotFor(def, now)
	}
	if len(def.Schedule) == 0 {
		return ""
	}
	// With no earlier run, Next reports the latest slot still inside its
	// late window: the slot a run at now is for.
	due := clockFor(RunRecord{}, nil, s.zone()).Next(def, now)
	if !due.Now || due.At.IsZero() {
		return ""
	}
	return due.At.Format(time.RFC3339)
}

// SlotLanded finds the commit on ref that carries both the task's tag and
// its slot line, as when another machine already ran the slot (US13).
func (r Repo) SlotLanded(ctx context.Context, ref, taskID, slot string) (string, bool, error) {
	out, err := r.git(ctx, "log", "-1", "--format=%H", "-E", "--all-match",
		"--grep=^"+regexp.QuoteMeta(TagKey+": "+taskID)+"$",
		"--grep=^"+regexp.QuoteMeta(taskdef.SlotKey+": "+slot)+"$", ref)
	return out, err == nil && out != "", err
}

// slotDone records a due slot that already landed on the primary ref as a
// skipped run, so the task waits for its next slot.
func (s *Service) slotDone(ctx context.Context, def Definition, slot, landed, commit string) Outcome {
	reason := fmt.Sprintf("slot %s already landed as commit %s", slot, short(landed))
	_ = s.store.Update(ctx, func(st *State) error {
		st.Record(RunRecord{Task: def.ID, At: s.opts.Now(), Outcome: OutcomeSkipped, Message: reason, Commit: commit, Landing: def.Land, Landed: landed})
		return nil
	})
	return Outcome{Task: def.ID, Outcome: OutcomeSkipped, Reason: reason}
}
