package repoexec

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Resolve is the one decision both front ends act on, so each authority's
// target, trust, install, and build rules are pinned down here.
func TestResolveCompletesEveryAuthority(t *testing.T) {
	root := t.TempDir()
	pin := filepath.Join(root, ".rhizome", "bin", "rzm")
	devApp := filepath.Join(t.TempDir(), "dev-app-rzm")
	for _, tc := range []struct {
		name   string
		plan   Plan
		caller Caller
		want   Execution
	}{
		{
			name:   "external runs the caller's executable, which may be a shim",
			plan:   Plan{Authority: "external", ConfigDir: root},
			caller: Caller{Executable: "/usr/local/bin/rzm"},
			want:   Execution{Target: "/usr/local/bin/rzm", FromCaller: true},
		},
		{
			name:   "external prefers the user's choice",
			plan:   Plan{Authority: "external", ConfigDir: root},
			caller: Caller{Executable: "/usr/local/bin/rzm", Chosen: "/tools/rzm"},
			want:   Execution{Target: "/tools/rzm"},
		},
		{
			name:   "global runs the caller's executable",
			plan:   Plan{Authority: "global"},
			caller: Caller{Executable: "/usr/local/bin/rzm"},
			want:   Execution{Target: "/usr/local/bin/rzm", FromCaller: true, ExactBuild: true},
		},
		{
			name:   "a managed pin yields to the explicit worktree choice",
			plan:   Plan{Authority: "managed", Target: pin, ConfigDir: root, PinnedVersion: "v1.0.0", PinTarget: pin},
			caller: Caller{Chosen: "/tools/rzm"},
			want:   Execution{Target: "/tools/rzm", TrustRoot: root},
		},
		{
			name:   "the app's own choice does not bypass managed checkout trust",
			plan:   Plan{Authority: "managed", Target: pin, ConfigDir: root, PinnedVersion: "v1.0.0", PinTarget: pin},
			caller: Caller{Executable: "/app/rzm", Chosen: "/app/rzm"},
			want:   Execution{Target: "/app/rzm", TrustRoot: root},
		},
		{
			name:   "an unpinned worktree also honors the explicit choice",
			plan:   Plan{Authority: "global"},
			caller: Caller{Executable: "/app/rzm", Chosen: "/tools/rzm"},
			want:   Execution{Target: "/tools/rzm"},
		},
		{
			name:   "an override keeps trust even when the caller already runs the pin",
			plan:   Plan{Authority: "managed", Target: pin, ConfigDir: root, PinnedVersion: "v1.0.0", PinTarget: pin},
			caller: Caller{Executable: pin, Chosen: "/tools/rzm"},
			want:   Execution{Target: "/tools/rzm", TrustRoot: root},
		},
		{
			name:   "the caller already running the pinned binary needs no trust",
			plan:   Plan{Authority: "managed", Target: pin, ConfigDir: root, PinnedVersion: "v1.0.0", PinTarget: pin},
			caller: Caller{Executable: pin, ActsOnPin: true},
			want:   Execution{Target: pin, InstallPin: true, ExactBuild: true},
		},
		{
			name:   "the Dev app's rzm needs no trust",
			plan:   Plan{Authority: "development", Target: devApp, DevApp: true, ConfigDir: root, PinnedVersion: "v1.0.0", PinTarget: pin},
			caller: Caller{},
			want:   Execution{Target: devApp, ExactBuild: true},
		},
		{
			name:   "a caller acting on the pin needs the pin's trust even when the Dev app runs",
			plan:   Plan{Authority: "development", Target: devApp, DevApp: true, ConfigDir: root, PinnedVersion: "v1.0.0", PinTarget: pin},
			caller: Caller{Executable: "/usr/local/bin/rzm", ActsOnPin: true},
			want:   Execution{Target: devApp, TrustRoot: root, ExactBuild: true},
		},
		{
			name:   "a repository build needs trust",
			plan:   Plan{Authority: "development", Target: filepath.Join(root, "bin", "rzm"), SourceRoot: root},
			caller: Caller{},
			want:   Execution{Target: filepath.Join(root, "bin", "rzm"), TrustRoot: root, ExactBuild: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.plan, tc.caller)
			require.NotEmpty(t, got.Reason)
			got.Reason = ""
			tc.want.Plan = tc.plan
			require.Equal(t, tc.want, got)
		})
	}
}
