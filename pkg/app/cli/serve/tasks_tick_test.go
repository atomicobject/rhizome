package serve

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/stretchr/testify/require"
)

// dueTasks is a tasks service with a settable due time; the other
// TasksAPI methods are not used by the runtime's check or host stream.
type dueTasks struct {
	web.TasksAPI
	due  time.Time
	ok   bool
	held bool
	err  error
}

func (f *dueTasks) NextDueAt(context.Context) (time.Time, bool, error) { return f.due, f.ok, f.err }
func (f *dueTasks) RunLockHeld() bool                                  { return f.held }
func (f *dueTasks) HostStatus(context.Context) (tasks.HostStatus, error) {
	return tasks.HostStatus{Notices: []tasks.HostNotice{}}, nil
}

func TestTaskDispatchStartsRunDueOnlyUnderAHostWhenSomethingIsDue(t *testing.T) {
	api := &dueTasks{ok: true}
	hosts := &web.TaskHosts{}
	var now time.Time
	starts := 0
	d := &taskDispatch{
		hosts: hosts,
		open:  func(context.Context) (web.TasksAPI, error) { return api, nil },
		start: func() error { starts++; return nil },
		now:   func() time.Time { return now },
	}
	at := func(clock string) time.Time {
		parsed, err := time.Parse(time.DateTime, "2026-11-22 "+clock)
		require.NoError(t, err)
		return parsed
	}
	check := func(clock string) int {
		now = at(clock)
		before := starts
		d.check(context.Background())
		return starts - before
	}

	api.due = at("05:00:00")
	require.Zero(t, check("05:02:00"), "a runtime with no host dispatches nothing")

	release := hosts.Hold()
	api.due = at("05:01:00")
	require.Zero(t, check("05:00:30"), "nothing is due yet")
	require.Equal(t, 1, check("05:01:10"), "due: start run-due")
	require.Zero(t, check("05:01:40"), "the same due time waits for the pass it started")
	api.held = true
	require.Zero(t, check("05:07:00"), "a run holds the run lock")
	api.held = false
	require.Equal(t, 1, check("05:07:00"), "a due time no pass moved is retried")

	api.due = at("05:10:00")
	require.Equal(t, 1, check("05:10:05"), "a new due time starts at once")
	api.due = at("06:00:00")
	require.Equal(t, 1, check("09:00:00"), "a slot passed during sleep runs on waking")

	api.ok = false
	require.Zero(t, check("10:00:00"), "nothing will come due")
	api.ok, api.err = true, errors.New("primary ref is not resolvable")
	require.Zero(t, check("10:00:30"))

	api.err, api.due = nil, at("10:30:00")
	hosts.Drain()
	require.Zero(t, check("10:31:00"), "a drained runtime dispatches nothing")
	release()
}
