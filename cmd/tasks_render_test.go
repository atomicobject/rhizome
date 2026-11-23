package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/stretchr/testify/require"
)

func TestTaskStatusShowsLocalErrorsWithoutPrimaryRef(t *testing.T) {
	var out bytes.Buffer
	renderTaskStatus(&out, tasks.Report{
		RefError: "primary ref missing",
		Tasks: []tasks.TaskStatus{{ID: "local", Name: "Local", Local: &tasks.LocalVersion{
			State: "invalid", Path: ".rhizome/tasks/local.yml", Error: "invalid cadence",
		}}},
	})
	require.Equal(t, "No scheduled tasks run: primary ref missing\n\nLocal (local)\n  local invalid (.rhizome/tasks/local.yml)\n  local error: invalid cadence\n  not scheduled (local test only)\n", out.String())
}

func TestTaskStatusSeparatesLocalAndScheduledVersions(t *testing.T) {
	var out bytes.Buffer
	renderTaskStatus(&out, tasks.Report{Ref: "origin/main", Commit: "1234567890", Tasks: []tasks.TaskStatus{
		{ID: "scheduled", Name: "Scheduled", Scheduled: true, Every: "1d", Scope: "person", Mode: tasks.ModeAuto, ModeSource: "default", Due: true,
			Local: &tasks.LocalVersion{State: "differs", Path: ".rhizome/tasks/scheduled.yml"}},
		{ID: "draft", Name: "Draft", Local: &tasks.LocalVersion{State: "new", Path: ".rhizome/tasks/draft.yml"}},
	}})
	require.Contains(t, out.String(), "local differs (.rhizome/tasks/scheduled.yml)\n  every 1d, person scope, auto mode (default)\n  last run never\n  due now")
	require.Contains(t, out.String(), "Draft (draft)\n  local new (.rhizome/tasks/draft.yml)\n  not scheduled (local test only)")
	require.NotContains(t, out.String(), "Jan 1")
}

func TestTaskStatusExplainsHostingAndSchedules(t *testing.T) {
	var out bytes.Buffer
	next := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	renderTaskStatus(&out, tasks.Report{Ref: "origin/main", Commit: "1234567890",
		Fetch: &tasks.FetchStatus{At: time.Now(), Error: "git fetch: could not read from remote"},
		Tasks: []tasks.TaskStatus{
			{ID: "dream", Name: "Dream", Scheduled: true, Schedule: "weekdays at 05:00 (Asia/Tokyo)", Scope: "person", Mode: tasks.ModeOff, ModeSource: "override", NextAt: next, Timezone: "Asia/Tokyo", BlockedBy: tasks.BlockedOff},
		}})
	text := out.String()
	require.Contains(t, text, "The last fetch failed just now, so tasks use the last fetched primary ref: git fetch: could not read from remote\n")
	require.Contains(t, text, "Scheduled tasks do not run on this machine; `rzm tasks machine on` turns them on.\n")
	require.Contains(t, text, "  weekdays at 05:00 (Asia/Tokyo), person scope, off mode (override)\n")
	require.Contains(t, text, "  not running: the task is off\n")
	if time.Local.String() != "Asia/Tokyo" {
		require.Contains(t, text, "  next due Tue Nov 24 19:00 Asia/Tokyo\n")
	}
}
