package cmd

import (
	"bytes"
	"testing"

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
