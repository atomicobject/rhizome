package serve

import (
	"context"
	"errors"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/stretchr/testify/require"
)

// dueTasks is a tasks service with a settable due time and recent runs;
// the other TasksAPI methods are not used by the runtime's check or host
// stream.
type dueTasks struct {
	web.TasksAPI
	due    time.Time
	ok     bool
	held   bool
	err    error
	recent []tasks.RunRecord
	// onNextDue runs inside NextDueAt, between the check's host test and
	// its start.
	onNextDue func()
}

func (f *dueTasks) NextDueAt(context.Context) (time.Time, bool, error) {
	if f.onNextDue != nil {
		f.onNextDue()
	}
	return f.due, f.ok, f.err
}
func (f *dueTasks) RunLockHeld() bool { return f.held }
func (f *dueTasks) LiveState(context.Context) (tasks.LiveState, error) {
	return tasks.LiveState{Recent: f.recent}, nil
}
func (f *dueTasks) HostStatus(context.Context) (tasks.HostStatus, error) {
	return tasks.HostStatus{Notices: []tasks.HostNotice{}}, nil
}

// fakeRunDue starts run-due children the test ends with exit.
type fakeRunDue struct {
	starts int
	done   chan struct{}
}

func (f *fakeRunDue) start() (*appruntime.Detached, error) {
	f.starts++
	f.done = make(chan struct{})
	return &appruntime.Detached{PID: 1, Done: f.done}, nil
}

func (f *fakeRunDue) exit() { close(f.done) }

func TestTaskDispatchStartsRunDueOnlyUnderAHostWhenSomethingIsDue(t *testing.T) {
	api := &dueTasks{ok: true}
	hosts := &web.TaskHosts{}
	runDue := &fakeRunDue{}
	var now time.Time
	d := &taskDispatch{
		hosts: hosts,
		open:  func(context.Context) (web.TasksAPI, error) { return api, nil },
		start: runDue.start,
		now:   func() time.Time { return now },
	}
	at := func(clock string) time.Time {
		parsed, err := time.Parse(time.DateTime, "2026-11-22 "+clock)
		require.NoError(t, err)
		return parsed
	}
	check := func(clock string) int {
		now = at(clock)
		before := runDue.starts
		d.check(context.Background())
		return runDue.starts - before
	}
	recorded := func(clock string) {
		api.recent = append([]tasks.RunRecord{{Task: "a", At: at(clock)}}, api.recent...)
	}

	api.due = at("05:00:00")
	require.Zero(t, check("05:02:00"), "a runtime with no host dispatches nothing")

	release := hosts.Hold()
	api.due = at("05:01:00")
	require.Zero(t, check("05:00:30"), "nothing is due yet")
	require.Equal(t, 1, check("05:01:10"), "due: start run-due")
	require.Zero(t, check("05:01:40"), "the run-due it started is still running")

	// It exits without recording a run; due times that move with the
	// clock must not start another at once.
	runDue.exit()
	api.due = at("05:02:10")
	require.Zero(t, check("05:02:10"), "a pass that recorded nothing waits")
	api.due = at("05:02:40")
	require.Zero(t, check("05:02:40"), "a moving due time does not bypass the wait")
	api.held = true
	require.Zero(t, check("05:07:20"), "a run holds the run lock")
	api.held = false
	require.Equal(t, 1, check("05:07:20"), "five minutes after it exited, run-due is retried")

	recorded("05:08:00")
	runDue.exit()
	api.due = at("05:10:00")
	require.Equal(t, 1, check("05:10:05"), "after a pass that recorded a run, the next due time starts at once")
	recorded("05:11:00")
	runDue.exit()
	api.due = at("06:00:00")
	require.Equal(t, 1, check("09:00:00"), "a slot passed during sleep runs on waking")
	recorded("09:01:00")
	runDue.exit()

	api.ok = false
	require.Zero(t, check("10:00:00"), "nothing will come due")
	api.ok, api.err = true, errors.New("primary ref is not resolvable")
	require.Zero(t, check("10:00:30"))

	api.err, api.due = nil, at("10:30:00")
	hosts.Drain()
	require.Zero(t, check("10:31:00"), "a drained runtime dispatches nothing")
	release()
}

// A drain that arrives while a check decides is honoured: the check starts
// nothing after it, because the host registry checks the drain under the
// lock it starts under.
func TestTaskDispatchStartsNothingAfterADrainMidCheck(t *testing.T) {
	hosts := &web.TaskHosts{}
	defer hosts.Hold()()
	now := time.Date(2026, 11, 22, 5, 0, 0, 0, time.UTC)
	api := &dueTasks{ok: true, due: now, onNextDue: hosts.Drain}
	runDue := &fakeRunDue{}
	d := &taskDispatch{
		hosts: hosts,
		open:  func(context.Context) (web.TasksAPI, error) { return api, nil },
		start: runDue.start,
		now:   func() time.Time { return now },
	}
	d.check(context.Background())
	require.Zero(t, runDue.starts, "run-due started after the drain")
}
