package tasks

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
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
	Ref       string     `json:"ref,omitempty"`
	Commit    string     `json:"commit,omitempty"`
	RefError  string     `json:"refError,omitempty"`
	FetchedAt time.Time  `json:"fetchedAt,omitzero"`
	Paused    bool       `json:"paused"`
	Untrusted string     `json:"untrusted,omitempty"`
	PassError *PassError `json:"lastPassError,omitempty"`
	// MergeWarning says when the repository's squash merges drop task tags,
	// so team tasks would not see teammates' merged runs.
	MergeWarning string       `json:"mergeWarning,omitempty"`
	StateDir     string       `json:"stateDir"`
	Tasks        []TaskStatus `json:"tasks"`
	Problems     []Problem    `json:"problems,omitempty"`
}

// Status reports every task's mode, last run, and next due time. It reads
// only; announcements and runs happen in RunDue.
func (s *Service) Status(ctx context.Context) (Report, error) {
	st, err := s.store.Load()
	if err != nil {
		return Report{}, err
	}
	report := Report{Paused: st.Paused, Untrusted: s.untrusted(), PassError: st.PassError, StateDir: s.stateDir, Tasks: []TaskStatus{}}
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
		ts.Tokens = st.TokensLast30Days(def.ID, now)
		report.Tasks = append(report.Tasks, ts)
	}
	for _, def := range defs {
		if def.Scope == ScopeTeam {
			report.MergeWarning = s.cachedMergeWarning(ctx, now)
			break
		}
	}
	return report, nil
}

// mergeWarningTTL bounds how often one Service asks GitHub for the squash
// setting; the Tasks page polls Status, and gh can take seconds.
const mergeWarningTTL = 10 * time.Minute

// readMergeWarning is mergeWarning; tests replace it.
var readMergeWarning = mergeWarning

func (s *Service) cachedMergeWarning(ctx context.Context, now time.Time) string {
	s.merge.mu.Lock()
	defer s.merge.mu.Unlock()
	if s.merge.at.IsZero() || now.Sub(s.merge.at) >= mergeWarningTTL {
		s.merge.value, s.merge.at = readMergeWarning(ctx, s.opts.Repo.MainCheckout), now
	}
	return s.merge.value
}

// mergeWarning reads the GitHub repository's squash setting with gh, when
// it is installed. Only a squash message built from the commit messages
// keeps the task tag; any failure means nothing is known.
func mergeWarning(ctx context.Context, dir string) string {
	if _, err := exec.LookPath("gh"); err != nil || dir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "api", "repos/{owner}/{repo}", "--jq", "[.allow_squash_merge, .squash_merge_commit_message] | @tsv")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) == 2 && fields[0] == "true" && fields[1] != "COMMIT_MESSAGES" {
		return fmt.Sprintf("squash merges use %s as the commit message, which drops the %s line, so teammates' merged runs will not reset team tasks; set the squash message to the commit messages", fields[1], TagKey)
	}
	return ""
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
