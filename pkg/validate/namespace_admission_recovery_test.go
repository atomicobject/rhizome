package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNamespacePrepublicationGitChangeDoesNotStrandOwnedLock(t *testing.T) {
	root, plan, _ := namespaceTrackedFixture(t)
	var admitted []byte
	probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md"}}}
	once := false
	result, err := applyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, nil, &repairExecutionHooks{AfterJournalPublished: func(string) error {
		if once {
			return nil
		}
		once = true
		require.NoError(t, os.WriteFile(filepath.Join(root, "unrelated.md"), []byte("later legitimate staging\n"), 0600))
		namespaceFixtureGit(t, root, "add", "--", "unrelated.md")
		admitted = namespaceFixtureGit(t, root, "ls-files", "--stage", "-z")
		return nil
	}})
	require.Error(t, err)
	require.Equal(t, NamespaceNotStarted, result.Current.Decision)
	require.False(t, result.Current.RecoveryPending)
	require.Equal(t, []byte("dirty before\n"), mustReadFile(t, filepath.Join(root, "old.md")))
	require.NoFileExists(t, filepath.Join(root, "new.md"))
	require.Equal(t, admitted, namespaceFixtureGit(t, root, "ls-files", "--stage", "-z"))
	require.NoFileExists(t, filepath.Join(root, ".git", "index.lock"), "a competing completed Git write before admission must not strand an engine-owned lock")
}
