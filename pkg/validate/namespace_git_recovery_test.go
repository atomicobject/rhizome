package validate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate/namespaceadmission"
	"github.com/stretchr/testify/require"
)

func namespaceFixtureGit(t *testing.T, root string, args ...string) []byte {
	t.Helper()
	command := exec.Command("git", append([]string{"-c", "core.fsmonitor=false"}, args...)...)
	command.Dir = root
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	return output
}

func namespaceTrackedFixture(t *testing.T) (string, NamespaceMutationPlan, []byte) {
	t.Helper()
	root := t.TempDir()
	namespaceFixtureGit(t, root, "init", "--quiet")
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("staged before\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "unrelated.md"), []byte("unrelated\n"), 0o600))
	namespaceFixtureGit(t, root, "add", "--", "old.md", "unrelated.md")
	staging := namespaceFixtureGit(t, root, "ls-files", "--stage", "-z")
	require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("dirty before\n"), 0o600))
	return root, namespaceTestPlan(t, root), staging
}

func namespaceFixturePlanner(plan NamespaceMutationPlan) namespaceTestPlanner {
	return func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error) { return plan, nil }
}

func TestNamespaceGitPublicationPreservesStagedVersusDirtyContent(t *testing.T) {
	root, plan, _ := namespaceTrackedFixture(t)
	probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md"}}}
	result, err := ApplyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, nil)
	require.NoError(t, err)
	require.Equal(t, NamespaceCommitted, result.Current.Decision)
	require.Equal(t, []PathRename{{From: "old.md", To: "new.md"}}, result.Current.GitMoves)
	require.Equal(t, []byte("staged before\n"), namespaceFixtureGit(t, root, "show", ":new.md"))
	require.Equal(t, []byte("after\n"), mustReadFile(t, filepath.Join(root, "new.md")))
	require.Equal(t, []byte("unrelated\n"), namespaceFixtureGit(t, root, "show", ":unrelated.md"))
	require.NoFileExists(t, filepath.Join(root, ".git", "index.lock"))
}

func TestNamespaceGitFailureAndCrashAfterIndexRestoreOriginalStaging(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "crash"}[crash], func(t *testing.T) {
			root, plan, original := namespaceTrackedFixture(t)
			probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md"}}}
			failure := errors.New("after index failure")
			if crash {
				failure = errSimulatedRepairInterruption
			}
			result, err := applyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, nil, &repairExecutionHooks{AfterNamespaceIndexPublication: func() error { return failure }})
			require.ErrorIs(t, err, failure)
			if crash {
				require.Equal(t, NamespaceUnresolved, result.Current.Decision)
				require.FileExists(t, filepath.Join(root, ".git", "index.lock"))
				require.NoError(t, StabilizePendingRepairJournals(namespaceTestRunContext(root)))
				require.NoError(t, StabilizePendingRepairJournals(namespaceTestRunContext(root)))
				result, err = ApplyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, nil)
				require.ErrorContains(t, err, "replan")
				require.Equal(t, NamespaceNotStarted, result.Current.Decision)
				require.Equal(t, NamespaceRestored, result.Recovered[0].Decision)
			} else {
				require.Equal(t, NamespaceRestored, result.Current.Decision)
			}
			require.Equal(t, original, namespaceFixtureGit(t, root, "ls-files", "--stage", "-z"))
			require.Equal(t, []byte("dirty before\n"), mustReadFile(t, filepath.Join(root, "old.md")))
			require.NoFileExists(t, filepath.Join(root, "new.md"))
			require.NoFileExists(t, filepath.Join(root, ".git", "index.lock"))
		})
	}
}

func TestNamespaceGitForeignInitialLockUsesFilesystemFallback(t *testing.T) {
	root, plan, original := namespaceTrackedFixture(t)
	foreign := []byte("foreign Git owner\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git", "index.lock"), foreign, 0o600))
	probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md"}}}
	result, err := ApplyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, nil)
	require.NoError(t, err)
	require.Equal(t, NamespaceCommitted, result.Current.Decision)
	require.Empty(t, result.Current.GitMoves)
	require.Equal(t, foreign, mustReadFile(t, filepath.Join(root, ".git", "index.lock")))
	require.Equal(t, original, namespaceFixtureGit(t, root, "ls-files", "--stage", "-z"))
	require.Equal(t, []byte("after\n"), mustReadFile(t, filepath.Join(root, "new.md")))
}

func TestNamespaceDurableDecisionSurvivesForeignLockReleaseFailure(t *testing.T) {
	for _, restored := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "restored"}[restored], func(t *testing.T) {
			root, plan, _ := namespaceTrackedFixture(t)
			foreign := []byte("replacement Git owner\n")
			lock := filepath.Join(root, ".git", "index.lock")
			hooks := &repairExecutionHooks{BeforeNamespaceDecisionDirectorySync: func(string) error {
				require.NoError(t, os.Rename(lock, lock+".old-owner"))
				require.NoError(t, os.WriteFile(lock, foreign, 0o600))
				return nil
			}}
			if restored {
				hooks.AfterNamespaceIndexPublication = func() error { return errors.New("force restoration") }
			}
			probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md"}}}
			calls := 0
			callback := func(context.Context, *IndexLockLease) { calls++ }
			result, err := applyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, callback, hooks)
			require.Error(t, err)
			expected := NamespaceCommitted
			if restored {
				expected = NamespaceRestored
			}
			require.Equal(t, expected, result.Current.Decision)
			require.True(t, result.Current.RecoveryPending)
			require.Nil(t, probe.lease, "refresh cannot precede settled exclusion")
			require.Error(t, namespaceadmission.Check(root))
			require.Equal(t, foreign, mustReadFile(t, lock))
			require.Error(t, StabilizePendingRepairJournals(namespaceTestRunContext(root)))
			require.Equal(t, foreign, mustReadFile(t, lock))
			result, err = ApplyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, callback)
			require.Error(t, err)
			require.Equal(t, NamespaceNotStarted, result.Current.Decision)
			require.Equal(t, expected, result.Recovered[0].Decision)
			require.Zero(t, calls, "unsettled Git exclusion cannot invoke optional writes")
		})
	}
}

func TestNamespaceDecisionSyncUncertaintyFencesGitAndNoGit(t *testing.T) {
	for _, withGit := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-git", true: "git"}[withGit], func(t *testing.T) {
			var root string
			var plan NamespaceMutationPlan
			if withGit {
				root, plan, _ = namespaceTrackedFixture(t)
			} else {
				root = t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(root, "old.md"), []byte("before\n"), 0o600))
				plan = namespaceTestPlan(t, root)
			}
			probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md"}}}
			uncertain := errors.New("decision directory sync failed")
			result, err := applyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, nil, &repairExecutionHooks{BeforeNamespaceDecisionDirectorySync: func(string) error { return uncertain }})
			require.ErrorIs(t, err, uncertain)
			require.Equal(t, NamespaceUnresolved, result.Current.Decision)
			require.Error(t, namespaceadmission.Check(root))
			require.Nil(t, probe.lease)
			if withGit {
				require.FileExists(t, filepath.Join(root, ".git", "index.lock"))
			}
			result, err = ApplyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, nil)
			require.ErrorContains(t, err, "replan")
			require.Equal(t, NamespaceCommitted, result.Recovered[0].Decision)
			require.False(t, result.Recovered[0].RecoveryPending)
			require.Equal(t, []byte("after\n"), mustReadFile(t, filepath.Join(root, "new.md")))
			require.NoFileExists(t, filepath.Join(root, ".git", "index.lock"))
		})
	}
}
