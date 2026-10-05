package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func namespaceBatchFixture(t *testing.T) (string, NamespaceMutationPlan, map[string][]byte, []byte) {
	t.Helper()
	root, plan, _ := namespaceTrackedFixture(t)
	originals := map[string][]byte{"old.md": []byte("dirty before\n"), "new.md": []byte("destination before\n"), "second.md": []byte("second before\n"), "ref.md": []byte("[[old]] [[second]]\n")}
	for path, content := range originals {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), content, 0o640))
	}
	namespaceFixtureGit(t, root, "add", "--", "new.md", "second.md", "ref.md")
	destinationInfo, err := os.Stat(filepath.Join(root, "new.md"))
	require.NoError(t, err)
	sourceInfo, err := os.Stat(filepath.Join(root, "old.md"))
	require.NoError(t, err)
	sourceMode := repairModeBits(sourceInfo.Mode())
	plan.Operations[0].SourceMode, plan.Operations[1].SourceMode = &sourceMode, &sourceMode
	plan.Operations[0].DestinationState = &RepairDestinationState{Kind: RepairDestinationOccupied, Content: originals["new.md"], Hash: SourceHash(originals["new.md"]), Mode: repairModeBits(destinationInfo.Mode())}
	for _, path := range []string{"second.md", "ref.md"} {
		info, err := os.Stat(filepath.Join(root, path))
		require.NoError(t, err)
		mode := repairModeBits(info.Mode())
		if path == "second.md" {
			plan.Operations = append(plan.Operations, RepairOperation{Kind: RepairOperationRename, Path: path, DestinationPath: "other/new-second.md", SourceHash: SourceHash(originals[path]), SourceMode: &mode, DestinationState: &RepairDestinationState{Kind: RepairDestinationAbsent}})
		} else {
			plan.Operations = append(plan.Operations, RepairOperation{Kind: RepairOperationWrite, Path: path, SourceHash: SourceHash(originals[path]), SourceMode: &mode, Content: []byte("[[new]] [[other/new-second]]\n")})
		}
	}
	return root, plan, originals, namespaceFixtureGit(t, root, "ls-files", "--stage", "-z")
}

func namespaceAssertBatchRestored(t *testing.T, root string, originals map[string][]byte, staging []byte) {
	t.Helper()
	for path, content := range originals {
		require.Equal(t, content, mustReadFile(t, filepath.Join(root, path)))
	}
	require.NoFileExists(t, filepath.Join(root, "other/new-second.md"))
	require.Equal(t, staging, namespaceFixtureGit(t, root, "ls-files", "--stage", "-z"))
	require.NoFileExists(t, filepath.Join(root, ".git/index.lock"))
	journals, err := DetectPendingRepairJournals(namespaceTestRunContext(root))
	require.NoError(t, err)
	require.Empty(t, journals)
}

func TestNamespaceEveryRequiredPublicationInterruptionRestoresBatch(t *testing.T) {
	for _, crash := range []bool{false, true} {
		for boundary := 0; boundary < 6; boundary++ {
			t.Run(fmt.Sprintf("crash=%t/entry=%d", crash, boundary), func(t *testing.T) {
				root, plan, originals, staging := namespaceBatchFixture(t)
				probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md", "second.md", "other/new-second.md", "ref.md"}}}
				failure := errors.New("required publication failure")
				if crash {
					failure = errSimulatedRepairInterruption
				}
				result, err := applyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, nil, &repairExecutionHooks{AfterMutation: func(index int, _ string) error {
					if index == boundary {
						return failure
					}
					return nil
				}})
				require.ErrorIs(t, err, failure)
				if crash {
					require.Equal(t, NamespaceUnresolved, result.Current.Decision)
					result, err = ApplyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, nil)
					require.ErrorContains(t, err, "replan")
					require.Equal(t, NamespaceNotStarted, result.Current.Decision)
					require.Equal(t, NamespaceRestored, result.Recovered[0].Decision)
				} else {
					require.Equal(t, NamespaceRestored, result.Current.Decision)
				}
				namespaceAssertBatchRestored(t, root, originals, staging)
			})
		}
	}
}

func TestNamespaceEveryReverseRollbackInterruptionResumesSafely(t *testing.T) {
	for boundary := 0; boundary < 6; boundary++ {
		t.Run(fmt.Sprintf("entry=%d", boundary), func(t *testing.T) {
			root, plan, originals, staging := namespaceBatchFixture(t)
			probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"old.md", "new.md", "second.md", "other/new-second.md", "ref.md"}}}
			result, err := applyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, nil, &repairExecutionHooks{
				AfterNamespaceIndexPublication: func() error { return errors.New("required publication failure") },
				BeforeRollbackMutation: func(index int, _ string) error {
					if index == boundary {
						return errSimulatedRepairInterruption
					}
					return nil
				},
			})
			require.ErrorIs(t, err, errSimulatedRepairInterruption)
			require.Equal(t, NamespaceUnresolved, result.Current.Decision)
			result, err = ApplyNamespaceMutation(context.Background(), namespaceTestRunContext(root), namespaceFixturePlanner(plan), probe, nil)
			require.ErrorContains(t, err, "replan")
			require.Equal(t, NamespaceRestored, result.Recovered[0].Decision)
			namespaceAssertBatchRestored(t, root, originals, staging)
		})
	}
}
