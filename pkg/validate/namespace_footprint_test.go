package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func namespaceCaseOnlyRecoveryFixture(t *testing.T) (string, RunContext) {
	return namespaceCaseOnlyRecoveryFixtureFor(t, "Old.md", "old.md", false)
}

func namespaceCaseOnlyRecoveryFixtureFor(t *testing.T, from, to string, restored bool) (string, RunContext) {
	t.Helper()
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, from), before, 0o600))
	info, err := os.Lstat(filepath.Join(root, from))
	require.NoError(t, err)
	alias, err := os.Lstat(filepath.Join(root, to))
	if os.IsNotExist(err) {
		t.Skip("requires a case-insensitive fixture filesystem")
	}
	require.NoError(t, err)
	require.True(t, os.SameFile(info, alias))
	mode := repairModeBits(info.Mode())
	plan := NamespaceMutationPlan{Operations: []RepairOperation{{
		Kind: RepairOperationRename, Path: from, DestinationPath: to,
		SourceHash: SourceHash(before), SourceMode: &mode,
		DestinationState: &RepairDestinationState{Kind: RepairDestinationCaseOnly},
	}}}
	runCtx := namespaceTestRunContext(root)
	blocked := errors.New("projection interrupted")
	decision := NamespaceCommitted
	var hooks *repairExecutionHooks
	if restored {
		decision = NamespaceRestored
		hooks = &repairExecutionHooks{AfterMutation: func(int, string) error { return errors.New("publication rejected") }}
	}
	first, err := applyNamespaceMutation(context.Background(), runCtx, namespaceTestPlanner(func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error) {
		return plan, nil
	}), &repairPostApplyProbe{t: t, refreshErr: blocked}, nil, hooks)
	require.ErrorIs(t, err, blocked)
	require.Equal(t, decision, first.Current.Decision, "apply: %v", err)
	require.True(t, first.Current.RecoveryPending)
	return root, runCtx
}

func namespaceRecoveryOnlyPlanner(t *testing.T) NamespaceMutationPlanner {
	t.Helper()
	return namespaceTestPlanner(func(context.Context, *IndexLockLease) (NamespaceMutationPlan, error) {
		t.Fatal("terminal recovery must not plan another mutation")
		return NamespaceMutationPlan{}, nil
	})
}

func TestNamespaceRecoveryProjectsExactCurrentSpelling(t *testing.T) {
	for _, current := range []string{"old.md", "OLD.md"} {
		t.Run(current, func(t *testing.T) {
			root, runCtx := namespaceCaseOnlyRecoveryFixture(t)
			if current != "old.md" {
				require.NoError(t, os.Rename(filepath.Join(root, "old.md"), filepath.Join(root, current)))
			}
			probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"Old.md", "old.md", current}}}
			result, err := ApplyNamespaceMutation(context.Background(), runCtx, namespaceRecoveryOnlyPlanner(t), probe, nil)
			require.ErrorContains(t, err, "replan")
			require.Len(t, result.Recovered, 1)
			require.False(t, result.Recovered[0].RecoveryPending)
			require.Equal(t, []string{current}, probe.changed)
			wantDeleted := []string{"Old.md"}
			if current != "old.md" {
				wantDeleted = append(wantDeleted, "old.md")
			}
			require.Equal(t, wantDeleted, probe.deleted)
			require.Empty(t, probe.renamed)
			require.Equal(t, "before\n", string(mustReadFile(t, filepath.Join(root, current))))
		})
	}
}

type namespaceSpellingMutationRefresher struct {
	probe  *repairPostApplyProbe
	mutate func()
}

func (r namespaceSpellingMutationRefresher) Refresh(ctx context.Context, lease *IndexLockLease, changed []string, renamed []PathRename, deleted []string) (PostApplyRefreshResult, error) {
	result, err := r.probe.Refresh(ctx, lease, changed, renamed, deleted)
	r.mutate()
	return result, err
}

func TestNamespaceRecoveryDetectsSameByteSpellingChangeDuringRefresh(t *testing.T) {
	root, runCtx := namespaceCaseOnlyRecoveryFixture(t)
	probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"Old.md", "old.md"}}}
	result, err := ApplyNamespaceMutation(context.Background(), runCtx, namespaceRecoveryOnlyPlanner(t), namespaceSpellingMutationRefresher{
		probe: probe,
		mutate: func() {
			require.NoError(t, os.Rename(filepath.Join(root, "old.md"), filepath.Join(root, "OLD.md")))
		},
	}, nil)
	require.ErrorContains(t, err, "footprint changed")
	require.Len(t, result.Recovered, 1)
	require.Equal(t, NamespaceCommitted, result.Recovered[0].Decision)
	require.True(t, result.Recovered[0].RecoveryPending)
	require.Equal(t, "before\n", string(mustReadFile(t, filepath.Join(root, "OLD.md"))))
	require.NoError(t, os.Rename(filepath.Join(root, "OLD.md"), filepath.Join(root, "old.md")))
	result, err = ApplyNamespaceMutation(context.Background(), runCtx, namespaceRecoveryOnlyPlanner(t), probe, nil)
	require.ErrorContains(t, err, "replan")
	require.False(t, result.Recovered[0].RecoveryPending)
}

func TestNamespaceFootprintPreservesExactHardlinkEntries(t *testing.T) {
	for _, name := range []string{"second.md", "FIRST.md"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "first.md"), []byte("same inode\n"), 0o600))
			if err := os.Link(filepath.Join(root, "first.md"), filepath.Join(root, name)); err != nil {
				if os.IsExist(err) {
					t.Skip("fixture filesystem cannot hold these two case-distinct names")
				}
				require.NoError(t, err)
			}
			manifest := repairJournalManifest{Changed: []string{"first.md", name}, Deleted: []string{"missing.md"}}
			files, changed, deleted, err := snapshotNamespaceFootprint(namespaceTestRunContext(root), manifest)
			require.NoError(t, err)
			require.Len(t, files, 3)
			require.ElementsMatch(t, []string{"first.md", name}, changed)
			require.Equal(t, []string{"missing.md"}, deleted)
		})
	}
}

func TestNamespaceFootprintIndexesCaseCandidatesWithoutAssumingAliases(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Upper.md"), []byte("current\n"), 0o600))
	observer := namespaceFootprintObserver{root: root, directories: make(map[string]namespaceDirectoryEntries)}
	entries := observer.directory(root)
	require.NoError(t, entries.err)
	// This tests candidate availability on every host, without claiming a Linux
	// case-folding mount. Only the file observer's physical proof admits aliases.
	require.Equal(t, []string{"Upper.md"}, entries.folded["upper.md"])
	require.True(t, entries.exact["Upper.md"])
	require.False(t, entries.exact["upper.md"])
	_, changed, deleted, err := snapshotNamespaceFootprint(namespaceTestRunContext(root), repairJournalManifest{Changed: []string{"Upper.md", "upper.md"}})
	require.NoError(t, err)
	require.Equal(t, []string{"Upper.md"}, changed)
	require.Equal(t, []string{"upper.md"}, deleted)
}

func TestNamespaceFootprintIndexesUnicodeSimpleFoldCandidates(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Σ.md"), []byte("current\n"), 0o600))
	observer := namespaceFootprintObserver{root: root, directories: make(map[string]namespaceDirectoryEntries)}
	entries := observer.directory(root)
	require.NoError(t, entries.err)
	for _, name := range []string{"Σ.md", "σ.md", "ς.md"} {
		require.Equal(t, []string{"Σ.md"}, entries.folded[namespaceCaseCandidateKey(name)])
	}
}

func TestNamespaceFootprintObservesUnicodeCaseAlias(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "Σ.md"), []byte("current\n"), 0o600))
	info, err := os.Lstat(filepath.Join(root, "Σ.md"))
	require.NoError(t, err)
	alias, err := os.Lstat(filepath.Join(root, "ς.md"))
	if os.IsNotExist(err) {
		t.Skip("fixture filesystem does not resolve the Unicode case alias")
	}
	require.NoError(t, err)
	require.True(t, os.SameFile(info, alias))
	_, changed, deleted, err := snapshotNamespaceFootprint(namespaceTestRunContext(root), repairJournalManifest{Changed: []string{"Σ.md", "ς.md"}})
	require.NoError(t, err)
	require.Equal(t, []string{"Σ.md"}, changed)
	require.Equal(t, []string{"ς.md"}, deleted)
}

func TestNamespaceFootprintObservesUnicodeNormalizationAlias(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "é.md"), []byte("current\n"), 0o600))
	info, err := os.Lstat(filepath.Join(root, "é.md"))
	require.NoError(t, err)
	alias, err := os.Lstat(filepath.Join(root, "e\u0301.md"))
	if os.IsNotExist(err) {
		t.Skip("fixture filesystem does not resolve normalization aliases")
	}
	require.NoError(t, err)
	require.True(t, os.SameFile(info, alias))
	_, changed, deleted, err := snapshotNamespaceFootprint(namespaceTestRunContext(root), repairJournalManifest{Changed: []string{"é.md", "e\u0301.md"}})
	require.NoError(t, err)
	require.Equal(t, []string{"é.md"}, changed)
	require.Equal(t, []string{"e\u0301.md"}, deleted)
}

func TestNamespaceFootprintFoldCandidatesDoNotProvePhysicalAliases(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "ß.md"), []byte("first\n"), 0o600))
	first, err := os.Lstat(filepath.Join(root, "ß.md"))
	require.NoError(t, err)
	second, err := os.Lstat(filepath.Join(root, "SS.md"))
	if err == nil && os.SameFile(first, second) {
		t.Skip("fixture filesystem treats these broader folds as physical aliases")
	}
	require.True(t, os.IsNotExist(err), "fixture must initially have only the exact ß entry")
	require.Equal(t, namespaceCaseCandidateKey("ß.md"), namespaceCaseCandidateKey("SS.md"))
	_, changed, deleted, err := snapshotNamespaceFootprint(namespaceTestRunContext(root), repairJournalManifest{Changed: []string{"ß.md", "SS.md"}})
	require.NoError(t, err)
	require.Equal(t, []string{"ß.md"}, changed)
	require.Equal(t, []string{"SS.md"}, deleted)
	// Even broader-fold-equivalent hardlink names remain independent exact keys.
	require.NoError(t, os.Link(filepath.Join(root, "ß.md"), filepath.Join(root, "SS.md")))
	_, changed, deleted, err = snapshotNamespaceFootprint(namespaceTestRunContext(root), repairJournalManifest{Changed: []string{"ß.md", "SS.md"}})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"ß.md", "SS.md"}, changed)
	require.Empty(t, deleted)
}

func TestNamespaceRecoveryProjectsUnicodeCurrentSpelling(t *testing.T) {
	for _, restored := range []bool{false, true} {
		for _, later := range []bool{false, true} {
			name := map[bool]string{false: "committed", true: "restored"}[restored] + map[bool]string{false: "/same", true: "/later"}[later]
			t.Run(name, func(t *testing.T) {
				root, runCtx := namespaceCaseOnlyRecoveryFixtureFor(t, "Σ.md", "ς.md", restored)
				current, decision := "ς.md", NamespaceCommitted
				if restored {
					current, decision = "Σ.md", NamespaceRestored
				}
				if later {
					require.NoError(t, os.Rename(filepath.Join(root, current), filepath.Join(root, "σ.md")))
					current = "σ.md"
				}
				probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"Σ.md", "ς.md", current}}}
				result, err := ApplyNamespaceMutation(context.Background(), runCtx, namespaceRecoveryOnlyPlanner(t), probe, nil)
				require.ErrorContains(t, err, "replan")
				require.Len(t, result.Recovered, 1)
				require.Equal(t, decision, result.Recovered[0].Decision)
				require.False(t, result.Recovered[0].RecoveryPending)
				require.Equal(t, []string{current}, probe.changed)
				var wantDeleted []string
				for _, recorded := range []string{"Σ.md", "ς.md"} {
					if recorded != current {
						wantDeleted = append(wantDeleted, recorded)
					}
				}
				require.ElementsMatch(t, wantDeleted, probe.deleted)
				require.Empty(t, probe.renamed)
				require.Equal(t, "before\n", string(mustReadFile(t, filepath.Join(root, current))))
				journals, err := DetectPendingRepairJournals(runCtx)
				require.NoError(t, err)
				require.Empty(t, journals)
			})
		}
	}
}

func TestNamespaceRecoveryProjectsCurrentNormalizationSpelling(t *testing.T) {
	for _, restored := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "restored"}[restored], func(t *testing.T) {
			root, runCtx := namespaceCaseOnlyRecoveryFixtureFor(t, "É.md", "é.md", restored)
			previous, decision := "é.md", NamespaceCommitted
			if restored {
				previous, decision = "É.md", NamespaceRestored
			}
			current := "e\u0301.md"
			previousInfo, err := os.Lstat(filepath.Join(root, previous))
			require.NoError(t, err)
			aliasInfo, err := os.Lstat(filepath.Join(root, current))
			if os.IsNotExist(err) {
				t.Skip("fixture filesystem does not resolve normalization aliases")
			}
			require.NoError(t, err)
			require.True(t, os.SameFile(previousInfo, aliasInfo))
			// Later activity chooses a decomposed spelling. The intermediate name
			// makes the exact new entry observable even on aliasing filesystems.
			require.NoError(t, os.Rename(filepath.Join(root, previous), filepath.Join(root, "later.md")))
			require.NoError(t, os.Rename(filepath.Join(root, "later.md"), filepath.Join(root, current)))
			probe := &repairPostApplyProbe{t: t, result: PostApplyRefreshResult{Paths: []string{"É.md", "é.md", current}}}
			result, err := ApplyNamespaceMutation(context.Background(), runCtx, namespaceRecoveryOnlyPlanner(t), probe, nil)
			require.ErrorContains(t, err, "replan")
			require.Len(t, result.Recovered, 1)
			require.Equal(t, decision, result.Recovered[0].Decision)
			require.False(t, result.Recovered[0].RecoveryPending)
			require.Equal(t, []string{current}, probe.changed)
			require.ElementsMatch(t, []string{"É.md", "é.md"}, probe.deleted)
			require.Empty(t, probe.renamed)
			require.Equal(t, "before\n", string(mustReadFile(t, filepath.Join(root, current))))
		})
	}
}

func TestNamespaceFootprintDoesNotProjectStaleParentSpelling(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "Parent"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Parent", "note.md"), []byte("note\n"), 0o600))
	if _, err := os.Lstat(filepath.Join(root, "parent")); os.IsNotExist(err) {
		t.Skip("requires a case-insensitive fixture filesystem")
	}
	_, changed, deleted, err := snapshotNamespaceFootprint(namespaceTestRunContext(root), repairJournalManifest{Changed: []string{"parent/note.md"}})
	if err != nil {
		require.ErrorContains(t, err, "parent spelling")
		return
	}
	// Windows' existing confined-parent resolver already restores actual case.
	// Platforms that retain the stale spelling instead return the conflict above.
	require.Equal(t, []string{"Parent/note.md"}, changed)
	require.Equal(t, []string{"parent/note.md"}, deleted)
}
