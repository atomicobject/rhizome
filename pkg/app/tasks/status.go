package tasks

import (
	"context"
	"time"
)

// TaskStatus describes one task for `rzm tasks status`.
type TaskStatus struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Scope      string     `json:"scope"`
	Every      string     `json:"every"`
	Mode       Mode       `json:"mode"`
	ModeSource string     `json:"modeSource"`
	LastRun    *RunRecord `json:"lastRun,omitempty"`
	LastTagged *Tagged    `json:"lastTagged,omitempty"`
	NextDue    time.Time  `json:"nextDue,omitzero"`
	Due        bool       `json:"due"`
	Item       *Item      `json:"item,omitempty"`
	Tokens     int64      `json:"tokensLast30Days,omitempty"`
}

// Report is the whole `rzm tasks status` result.
type Report struct {
	Ref       string       `json:"ref,omitempty"`
	Commit    string       `json:"commit,omitempty"`
	RefError  string       `json:"refError,omitempty"`
	FetchedAt time.Time    `json:"fetchedAt,omitzero"`
	Paused    bool         `json:"paused"`
	Untrusted string       `json:"untrusted,omitempty"`
	StateDir  string       `json:"stateDir"`
	Tasks     []TaskStatus `json:"tasks"`
	Problems  []Problem    `json:"problems,omitempty"`
}

// Status reports every task's mode, last run, and next due time. It reads
// only; announcements and runs happen in RunDue.
func (s *Service) Status(ctx context.Context) (Report, error) {
	st, err := s.store.Load()
	if err != nil {
		return Report{}, err
	}
	report := Report{Paused: st.Paused, Untrusted: s.untrusted(), StateDir: s.stateDir, Tasks: []TaskStatus{}}
	if t, ok := s.opts.Repo.FetchedAt(); ok {
		report.FetchedAt = t
	}
	ref, commit, err := s.opts.Repo.ResolvePrimary(ctx, s.opts.Config.PrimaryRef)
	report.Ref, report.Commit = ref, commit
	if err != nil {
		report.RefError = err.Error()
		return report, nil
	}
	defs, problems, err := LoadDefinitions(ctx, s.opts.Repo, commit)
	if err != nil {
		return report, err
	}
	report.Problems = problems
	tagged := s.lastTagged(ctx, ref, defs)
	now := s.opts.Now()
	for _, def := range defs {
		mode, source := st.ResolveMode(def.ID, s.opts.Config.DefaultMode)
		ts := TaskStatus{ID: def.ID, Name: def.Name, Scope: def.Scope, Every: def.Every, Mode: mode, ModeSource: source, LastTagged: tagged[def.ID]}
		if last, ok := st.LastRun(def.ID); ok {
			ts.LastRun = &last
		}
		clock := Clock{LastTagged: tagged[def.ID]}
		if ts.LastRun != nil {
			clock.LastLocal = ts.LastRun.At
		}
		ts.NextDue, ts.Due = clock.NextDue(def), clock.Due(def, now)
		if it := st.ActiveTaskItem(def.ID); it != nil {
			copy := *it
			ts.Item = &copy
		}
		for _, r := range st.History {
			if r.Task == def.ID && now.Sub(r.At) < 30*24*time.Hour {
				ts.Tokens += r.Tokens
			}
		}
		report.Tasks = append(report.Tasks, ts)
	}
	return report, nil
}

// Inbox lists the items the person should see now.
func (s *Service) Inbox() ([]Item, error) {
	st, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	now := s.opts.Now()
	items := []Item{}
	for _, it := range st.Items {
		if it.Visible(now) {
			items = append(items, it)
		}
	}
	return items, nil
}
