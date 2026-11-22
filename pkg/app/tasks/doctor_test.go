package tasks

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// newRemoteRepo is newRepo with a bare origin it has pushed main to.
func newRemoteRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := newRepo(t, files)
	remote := filepath.Join(t.TempDir(), "origin.git")
	run(t, dir, "init", "--quiet", "--bare", remote)
	run(t, dir, "remote", "add", "origin", remote)
	run(t, dir, "push", "--quiet", "origin", "main")
	run(t, dir, "fetch", "--quiet", "origin")
	return dir
}

func doctorService(t *testing.T, dir string, agent AgentRunner, trusted bool) *Service {
	t.Helper()
	repo, err := OpenRepo(context.Background(), dir)
	require.NoError(t, err)
	svc, err := New(Options{
		Repo:    repo,
		Config:  repoconfig.LocalTasksConfig{PrimaryRef: "origin/main"},
		Agent:   agent,
		Trusted: func(string) (bool, error) { return trusted, nil },
	})
	require.NoError(t, err)
	return svc
}

// checksByName keys checks by name; a later check of the same name wins.
func checksByName(checks []DoctorCheck) map[string]DoctorCheck {
	out := map[string]DoctorCheck{}
	for _, c := range checks {
		out[c.Check] = c
	}
	return out
}

func TestDoctorReportsEachTasksNeeds(t *testing.T) {
	dir := newRemoteRepo(t, map[string]string{
		"triage.yml": "name: Triage\nevery: 1d\nrequires:\n  commands: [sh, rzm, rzm-test-no-such-command]\n  mcp: [linear, files]\ndeny: [WebFetch]\nprompt: Triage.\n",
		"look.yml":   "name: Look\nevery: 1d\ncheck:\n  command: [\"true\"]\n",
	})
	driver := &listingHarness{
		Harness: harnesstest.Harness{StatusResult: harness.Status{Installed: true, LoggedIn: true, Version: "2.1.330", LoginHint: "claude auth login"}},
		servers: []harness.MCPServerState{{Name: "linear", Status: "connected", Usable: true}, {Name: "files", Status: "needs authentication"}},
	}
	agent := resolvingAgent{fakeAgent: &fakeAgent{}, kind: harness.KindClaude, driver: driver}
	ctx := context.Background()

	report, err := doctorService(t, dir, agent, true).Doctor(ctx, DoctorOptions{Push: true})
	require.NoError(t, err)
	require.False(t, report.OK)
	repo := checksByName(report.Checks)
	require.True(t, repo[CheckTrust].OK)
	require.True(t, repo[CheckPrimaryRef].OK)
	require.Contains(t, repo[CheckPrimaryRef].Detail, "origin/main at ")
	require.Contains(t, repo[CheckPrimaryRef].Detail, "last fetched")
	require.True(t, repo[CheckPush].OK, repo[CheckPush].Detail)
	require.True(t, repo[CheckObsidianGit].OK)

	require.Len(t, report.Tasks, 2)
	look, triage := report.Tasks[0], report.Tasks[1]
	if look.ID != "look" {
		look, triage = triage, look
	}
	require.True(t, look.OK)
	require.Empty(t, look.Checks, "a check-only task needs no harness")
	require.False(t, triage.OK)
	var failed []string
	for _, c := range triage.Checks {
		if !c.OK {
			failed = append(failed, c.Check+": "+c.Detail)
		}
	}
	require.Equal(t, []string{
		`command: command "rzm-test-no-such-command" is not on the login shell's PATH`,
		`mcp: MCP server "files" for claude reports "needs authentication"`,
	}, failed)
	got := checksByName(triage.Checks)
	require.Equal(t, "claude 2.1.330 is signed in", got[CheckHarness].Detail)
	require.True(t, got[CheckDeny].OK)
	require.Equal(t, 1, driver.listed)

	// One task, a remote that refuses the push, and an untrusted repository.
	run(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	report, err = doctorService(t, dir, resolvingAgent{fakeAgent: &fakeAgent{}, kind: harness.KindCodex, driver: driver}, false).Doctor(ctx, DoctorOptions{Task: "triage", Push: true})
	require.NoError(t, err)
	require.Len(t, report.Tasks, 1)
	repo = checksByName(report.Checks)
	require.False(t, repo[CheckTrust].OK)
	require.False(t, repo[CheckPush].OK)
	require.Contains(t, checksByName(report.Tasks[0].Checks)[CheckDeny].Detail, "codex cannot enforce deny")

	_, err = doctorService(t, dir, agent, true).Doctor(ctx, DoctorOptions{Task: "nope"})
	require.ErrorContains(t, err, `task "nope" is not declared`)
}

func TestDoctorSpotsObsidianGitTimers(t *testing.T) {
	cases := []struct {
		name     string
		plugins  string
		settings string
		ok       bool
	}{
		{name: "plugin not installed", ok: true},
		{name: "plugin disabled", plugins: `["dataview"]`, settings: `{"autoSaveInterval": 5}`, ok: true},
		{name: "enabled with defaults", plugins: `["obsidian-git"]`, ok: true},
		{name: "enabled without timers", plugins: `["obsidian-git"]`, settings: `{"autoSaveInterval": 0, "autoPullInterval": 0, "autoPushInterval": 10}`, ok: true},
		{name: "auto-commit", plugins: `["obsidian-git"]`, settings: `{"autoSaveInterval": 5}`},
		{name: "auto-pull", plugins: `["obsidian-git"]`, settings: `{"autoPullInterval": 10}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.plugins != "" {
				writeFile(t, filepath.Join(dir, ".obsidian", "community-plugins.json"), tc.plugins)
			}
			if tc.settings != "" {
				writeFile(t, filepath.Join(dir, ".obsidian", "plugins", "obsidian-git", "data.json"), tc.settings)
			}
			check := obsidianGitCheck(dir)
			require.Equal(t, tc.ok, check.OK, check.Detail)
		})
	}
}
