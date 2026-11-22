package tasks

import (
	"cmp"
	"context"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Status codes for why a task is not running (SPEC-0127 US10). They are
// the only skip vocabulary Rhizome Desktop shows. This package sets
// not-run-here, untrusted, paused, off, open-item, requires, and
// token-cap; the runtime host and landing set the others.
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
	// BlockedTokenCap: today's tokens reached the person's dailyTokenCap,
	// so scheduled agent work waits (SPEC-0127 US14).
	BlockedTokenCap = "token-cap"
)

// fetchTimeout bounds the fetch before a pass, where a hung remote falls
// back to the last fetched ref, and every other command that talks to a
// remote. Tests shorten it.
var fetchTimeout = 60 * time.Second

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
	_, err = s.opts.Repo.git(fetchCtx, "-c", s.passNoHooks(), "fetch", "--quiet", "--no-tags", name, "+refs/heads/"+branch+":"+full)
	status := &FetchStatus{At: s.opts.Now()}
	if err != nil {
		status.Error = redactURLCredentials(err.Error())
		status.FailingSince = status.At
	}
	_ = s.store.Update(ctx, func(st *State) error {
		if prev := st.Fetch; status.Error != "" && prev != nil && prev.Error != "" {
			status.FailingSince = cmp.Or(prev.FailingSince, prev.At)
		}
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
// repository once as the pass error instead of skipping silently. It
// alerts once per distinct pass-level failure (an untrusted repository, a
// primary ref that cannot be read, a fetch failing for fetchAlertAfter)
// until it clears, then delivers queued alerts.
func (s *Service) RunDueHost(ctx context.Context) (RunSummary, error) {
	m, err := s.MachineSettings()
	if err != nil {
		return RunSummary{}, err
	}
	if !m.Repository.RunHere {
		return RunSummary{Skipped: notRunHere}, nil
	}
	defer s.deliverAlerts(ctx)
	if reason := s.untrusted(); reason != "" {
		err := s.store.Update(ctx, func(st *State) error {
			if st.PassError == nil || st.PassError.Code != PassUntrusted || st.PassError.Message != reason {
				st.PassError = &PassError{At: s.opts.Now(), Code: PassUntrusted, Message: reason}
			}
			s.reviewPassAlert(st)
			return nil
		})
		return RunSummary{Skipped: reason}, err
	}
	// The person chose this machine for the repository, so a pass that
	// cannot read the primary ref records why even before any task ran.
	if err := os.MkdirAll(s.stateDir, 0o700); err != nil {
		return RunSummary{}, err
	}
	summary, err := s.runDue(ctx, true)
	// The pass's own delivery ran before its failure was noted; the deferred
	// one sends it, unless a stop cancelled ctx, which leaves it queued.
	s.notePassAlert(context.WithoutCancel(ctx))
	return summary, err
}

const notRunHere = "tasks do not run on this machine for this repository; turn them on with `rzm tasks machine on`"

// notePassError records why a pass failed before running any task, so
// status can explain why automatic work stopped. It returns err. A stop
// that cancelled ctx is not a failure of the pass and is not recorded, so
// it raises no pass alert.
func (s *Service) notePassError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return err
	}
	_ = s.store.Update(ctx, func(st *State) error {
		st.PassError = &PassError{At: s.opts.Now(), Message: err.Error()}
		return nil
	})
	return err
}
