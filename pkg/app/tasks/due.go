package tasks

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Status codes for why a task is not running (SPEC-0127 US10). They are
// the only skip vocabulary Rhizome Desktop shows. This package sets
// not-run-here, untrusted, paused, off, and open-item; the runtime host
// and landing set the others.
const (
	BlockedNotRunHere  = "not-run-here"
	BlockedNoHost      = "no-host"
	BlockedUntrusted   = "untrusted"
	BlockedPaused      = "paused"
	BlockedOff         = "off"
	BlockedOpenItem    = "open-item"
	BlockedWrongBranch = "wrong-branch"
	BlockedRequires    = "requires"
	BlockedFetchFailed = "fetch-failed"
)

// fetchTimeout bounds the fetch before a pass; a hung remote then falls
// back to the last fetched ref.
const fetchTimeout = 60 * time.Second

// dueTask is one task's due state in a pass.
type dueTask struct {
	def  Definition
	mode Mode
	due  Due
}

// taskDue is a task's due state at now, its mode, and what blocks it on
// its own: off, or an open item. An open item blocks its task except a
// needs-attention item, which waits only for the next slot or cadence, and
// a due item, whose check runs again to keep it current.
func taskDue(st *State, def Definition, tagged *Tagged, now time.Time, zone *time.Location, defaultMode string) (dueTask, string) {
	mode, _ := st.ResolveMode(def.ID, defaultMode)
	last, _ := st.LastRun(def.ID)
	d := dueTask{def: def, mode: mode, due: clockFor(last, tagged, zone).Next(def, now)}
	if mode == ModeOff {
		return d, BlockedOff
	}
	if it := st.ActiveTaskItem(def.ID); it != nil && it.State != StateAttention && it.State != StateDue {
		return d, BlockedOpenItem
	}
	return d, ""
}

// dueNow lists the unblocked tasks due at now, nearest deadline first: a
// slot by the end of its `late` window, an `every` task by its due time.
func dueNow(st *State, defs []Definition, tagged map[string]*Tagged, now time.Time, zone *time.Location, defaultMode string) []dueTask {
	var out []dueTask
	for _, def := range defs {
		if d, blocked := taskDue(st, def, tagged[def.ID], now, zone, defaultMode); blocked == "" && d.due.Now {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].due.Deadline.Before(out[j].due.Deadline) })
	return out
}

// zone is the person's schedule zone on this machine, or nil for the host's.
func (s *Service) zone() *time.Location {
	m, err := s.MachineSettings()
	if err != nil {
		return nil
	}
	loc, _ := s.personZone(m)
	return loc
}

// primary resolves the primary ref and loads its task definitions, without
// network.
func (s *Service) primary(ctx context.Context) (ref, commit string, defs []Definition, problems []Problem, err error) {
	if ref, commit, err = s.opts.Repo.ResolvePrimary(ctx, s.opts.Config.PrimaryRef); err != nil {
		return ref, commit, nil, nil, err
	}
	defs, problems, err = LoadDefinitions(ctx, s.opts.Repo, commit)
	return ref, commit, defs, problems, err
}

// fetchPrimary fetches a remote-tracking primary ref from its remote and
// records the outcome. A local branch is not fetched. Git runs
// non-interactively with a bounded timeout; on failure the last fetched
// ref stays in use.
func (s *Service) fetchPrimary(ctx context.Context, ref string) {
	full, err := s.opts.Repo.git(ctx, "rev-parse", "--symbolic-full-name", ref)
	rest, remote := strings.CutPrefix(full, "refs/remotes/")
	if err != nil || !remote {
		return
	}
	// ponytail: remote names containing "/" are not supported.
	name, branch, ok := strings.Cut(rest, "/")
	if !ok {
		return
	}
	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	_, err = s.opts.Repo.git(fetchCtx, "fetch", "--quiet", "--no-tags", name, "+refs/heads/"+branch+":"+full)
	status := &FetchStatus{At: s.opts.Now()}
	if err != nil {
		status.Error = redactURLCredentials(err.Error())
	}
	_ = s.store.Update(ctx, func(st *State) error {
		st.Fetch = status
		return nil
	})
}

var (
	urlUserinfo = regexp.MustCompile(`(://)[^/@\s'"]+@`)
	urlQuery    = regexp.MustCompile(`(://[^\s?#'"]*)[?#][^\s'"]*`)
	tokenValue  = regexp.MustCompile(`(?i)(access_token=|token=|x-access-token:)[^\s&#'"@]+`)
)

// redactURLCredentials hides what can carry a credential in Git's error
// text before it is stored or shown: a URL's userinfo, query string, and
// fragment, and token assignments anywhere.
func redactURLCredentials(s string) string {
	s = urlUserinfo.ReplaceAllString(s, "$1***@")
	s = urlQuery.ReplaceAllString(s, "$1?***")
	return tokenValue.ReplaceAllString(s, "$1***")
}

// RunDueHost is RunDue for a Rhizome Desktop host (`rzm tasks run-due
// --host`): it runs nothing unless the person turned on running this
// repository's tasks on this machine, and it records an untrusted
// repository once as the pass error instead of skipping silently.
func (s *Service) RunDueHost(ctx context.Context) (RunSummary, error) {
	m, err := s.MachineSettings()
	if err != nil {
		return RunSummary{}, err
	}
	if !m.Repository.RunHere {
		return RunSummary{Skipped: notRunHere}, nil
	}
	if reason := s.untrusted(); reason != "" {
		err := s.store.Update(ctx, func(st *State) error {
			if st.PassError == nil || st.PassError.Code != PassUntrusted || st.PassError.Message != reason {
				st.PassError = &PassError{At: s.opts.Now(), Code: PassUntrusted, Message: reason}
			}
			return nil
		})
		return RunSummary{Skipped: reason}, err
	}
	return s.runDue(ctx, true)
}

const notRunHere = "tasks do not run on this machine for this repository; turn them on with `rzm tasks machine on`"
