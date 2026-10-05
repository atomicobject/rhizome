package update

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildUpdatePlanMatrix(t *testing.T) {
	global := filepath.Clean("/usr/local/bin/rzm")
	repo := filepath.Clean("/workspace/.rhizome/bin/darwin-arm64/rzm")
	globalInvocation := Invocation{ExecutablePath: global}
	pinnedGlobalInvocation := Invocation{
		ExecutablePath: global,
		RepoTargetPath: repo,
		RepoPinned:     true,
		RepoPinVersion: "0.50.0",
	}
	pinnedRepoInvocation := pinnedGlobalInvocation
	pinnedRepoInvocation.ExecutablePath = repo
	pinnedRepoInvocation.RepoScoped = true

	globalLatest := UpdateTarget{Role: UpdateTargetGlobal, LogicalPath: global, Version: "v0.51.0"}
	repoLatest := UpdateTarget{Role: UpdateTargetRepo, LogicalPath: repo, Version: "v0.51.0", WriteMarker: true}
	repoLatestAndPin := repoLatest
	repoLatestAndPin.WritePin = true
	repoPinned := UpdateTarget{Role: UpdateTargetRepo, LogicalPath: repo, Version: "v0.50.0", WriteMarker: true}
	repoSet := UpdateTarget{Role: UpdateTargetRepo, LogicalPath: repo, Version: "v0.49.0", WriteMarker: true, WritePin: true}

	tests := []struct {
		name       string
		invocation Invocation
		opts       UpdatePlanOptions
		want       []UpdateTarget
		wantErr    string
	}{
		{name: "outside repo default updates current executable", invocation: globalInvocation, opts: UpdatePlanOptions{LatestVersion: "0.51.0"}, want: []UpdateTarget{globalLatest}},
		{name: "outside repo latest updates current executable", invocation: globalInvocation, opts: UpdatePlanOptions{LatestVersion: "v0.51.0", Mode: UpdateModeLatest}, want: []UpdateTarget{globalLatest}},
		{name: "outside repo pinned is rejected", invocation: globalInvocation, opts: UpdatePlanOptions{LatestVersion: "v0.51.0", Mode: UpdateModePinned}, wantErr: "requires a pinned repository"},
		{name: "outside repo set-version is rejected", invocation: globalInvocation, opts: UpdatePlanOptions{Mode: UpdateModeSetVersion, SetVersion: "v0.49.0"}, wantErr: "requires a pinned repository"},
		{
			name:       "global entry default with current pin updates global then repo without rewriting pin",
			invocation: Invocation{ExecutablePath: global, RepoTargetPath: repo, RepoPinned: true, RepoPinVersion: "v0.51.0"},
			opts:       UpdatePlanOptions{LatestVersion: "v0.51.0"},
			want:       []UpdateTarget{globalLatest, repoLatest},
		},
		{name: "global entry default accepted advances pin", invocation: pinnedGlobalInvocation, opts: UpdatePlanOptions{LatestVersion: "v0.51.0", AdvancePin: true}, want: []UpdateTarget{globalLatest, repoLatestAndPin}},
		{name: "global entry default declined updates global only", invocation: pinnedGlobalInvocation, opts: UpdatePlanOptions{LatestVersion: "v0.51.0"}, want: []UpdateTarget{globalLatest}},
		{name: "global entry latest updates both and pin", invocation: pinnedGlobalInvocation, opts: UpdatePlanOptions{LatestVersion: "v0.51.0", Mode: UpdateModeLatest}, want: []UpdateTarget{globalLatest, repoLatestAndPin}},
		{name: "global entry pinned updates repo only", invocation: pinnedGlobalInvocation, opts: UpdatePlanOptions{LatestVersion: "v0.51.0", Mode: UpdateModePinned}, want: []UpdateTarget{repoPinned}},
		{name: "global entry set-version updates repo and pin only", invocation: pinnedGlobalInvocation, opts: UpdatePlanOptions{Mode: UpdateModeSetVersion, SetVersion: "0.49.0"}, want: []UpdateTarget{repoSet}},
		{
			name:       "repo entry default with current pin updates repo without rewriting pin",
			invocation: Invocation{ExecutablePath: repo, RepoTargetPath: repo, RepoPinned: true, RepoScoped: true, RepoPinVersion: "v0.51.0"},
			opts:       UpdatePlanOptions{LatestVersion: "v0.51.0"},
			want:       []UpdateTarget{repoLatest},
		},
		{name: "repo entry default accepted advances pin", invocation: pinnedRepoInvocation, opts: UpdatePlanOptions{LatestVersion: "v0.51.0", AdvancePin: true}, want: []UpdateTarget{repoLatestAndPin}},
		{name: "repo entry default declined has no mutations", invocation: pinnedRepoInvocation, opts: UpdatePlanOptions{LatestVersion: "v0.51.0"}, want: []UpdateTarget{}},
		{name: "repo entry latest updates repo and pin", invocation: pinnedRepoInvocation, opts: UpdatePlanOptions{LatestVersion: "v0.51.0", Mode: UpdateModeLatest}, want: []UpdateTarget{repoLatestAndPin}},
		{name: "repo entry pinned updates exact pin", invocation: pinnedRepoInvocation, opts: UpdatePlanOptions{LatestVersion: "v0.51.0", Mode: UpdateModePinned}, want: []UpdateTarget{repoPinned}},
		{name: "repo entry set-version updates repo and pin", invocation: pinnedRepoInvocation, opts: UpdatePlanOptions{Mode: UpdateModeSetVersion, SetVersion: "v0.49.0"}, want: []UpdateTarget{repoSet}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := BuildUpdatePlan(tt.invocation, tt.opts)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, plan.Targets)
		})
	}
}

func TestBuildUpdatePlanValidatesInputs(t *testing.T) {
	valid := Invocation{
		ExecutablePath: "/usr/local/bin/rzm",
		RepoTargetPath: "/workspace/.rhizome/bin/darwin-arm64/rzm",
		RepoPinned:     true,
		RepoPinVersion: "v0.50.0",
	}

	tests := []struct {
		name    string
		mutate  func(*Invocation, *UpdatePlanOptions)
		wantErr string
	}{
		{name: "default requires latest", mutate: func(_ *Invocation, opts *UpdatePlanOptions) { opts.LatestVersion = "" }, wantErr: "latest version is required"},
		{name: "latest requires latest", mutate: func(_ *Invocation, opts *UpdatePlanOptions) { opts.Mode = UpdateModeLatest; opts.LatestVersion = "" }, wantErr: "latest version is required"},
		{name: "set-version requires requested version", mutate: func(_ *Invocation, opts *UpdatePlanOptions) { opts.Mode = UpdateModeSetVersion; opts.SetVersion = "" }, wantErr: "set version is required"},
		{name: "non-set mode rejects requested version", mutate: func(_ *Invocation, opts *UpdatePlanOptions) { opts.SetVersion = "v0.49.0" }, wantErr: "set version requires set-version mode"},
		{name: "unknown mode is rejected", mutate: func(_ *Invocation, opts *UpdatePlanOptions) { opts.Mode = UpdateMode("unknown") }, wantErr: "unknown update mode"},
		{name: "pin requires repo target", mutate: func(inv *Invocation, _ *UpdatePlanOptions) { inv.RepoTargetPath = "" }, wantErr: "repo target path is required"},
		{name: "pin requires version", mutate: func(inv *Invocation, _ *UpdatePlanOptions) { inv.RepoPinVersion = "" }, wantErr: "repo pin version is required"},
		{name: "repo scoped requires pin", mutate: func(inv *Invocation, _ *UpdatePlanOptions) { inv.RepoScoped = true; inv.RepoPinned = false }, wantErr: "repo-scoped invocation requires a pinned repository"},
		{name: "global target requires executable", mutate: func(inv *Invocation, _ *UpdatePlanOptions) { inv.ExecutablePath = "" }, wantErr: "executable path is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invocation := valid
			opts := UpdatePlanOptions{LatestVersion: "v0.51.0"}
			tt.mutate(&invocation, &opts)
			_, err := BuildUpdatePlan(invocation, opts)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestBuildUpdatePlanDeduplicatesOnlyNormalizedLogicalPaths(t *testing.T) {
	t.Run("same cleaned pathname is one destination", func(t *testing.T) {
		plan, err := BuildUpdatePlan(Invocation{
			ExecutablePath: "/workspace/bin/../bin/rzm",
			RepoTargetPath: "/workspace/bin/rzm",
			RepoPinned:     true,
			RepoPinVersion: "v0.51.0",
		}, UpdatePlanOptions{LatestVersion: "v0.51.0"})

		require.NoError(t, err)
		require.Len(t, plan.Targets, 1)
		require.Equal(t, filepath.Clean("/workspace/bin/rzm"), plan.Targets[0].LogicalPath)
		require.Equal(t, UpdateTargetRepo, plan.Targets[0].Role)
		require.True(t, plan.Targets[0].WriteMarker)
	})

	t.Run("distinct symlink or hardlink shaped names remain destinations", func(t *testing.T) {
		plan, err := BuildUpdatePlan(Invocation{
			ExecutablePath: "/usr/local/bin/rzm",
			RepoTargetPath: "/workspace/.rhizome/bin/rzm",
			RepoPinned:     true,
			RepoPinVersion: "v0.51.0",
		}, UpdatePlanOptions{LatestVersion: "v0.51.0"})

		require.NoError(t, err)
		require.Equal(t, []string{filepath.Clean("/usr/local/bin/rzm"), filepath.Clean("/workspace/.rhizome/bin/rzm")}, []string{
			plan.Targets[0].LogicalPath,
			plan.Targets[1].LogicalPath,
		})
	})
}
