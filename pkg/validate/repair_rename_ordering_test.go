package validate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestApplyFixPlanRenamePublicationOrderRecoversEveryMutation(t *testing.T) {
	for _, rename := range []PathRename{{From: "a-old.md", To: "z-new.md"}, {From: "z-old.md", To: "a-new.md"}} {
		for _, failure := range []string{"none", "failure", "interruption"} {
			stops := []int{0, 1, 2}
			if failure == "none" {
				stops = stops[:1]
			}
			for _, failAt := range stops {
				t.Run(fmt.Sprintf("%s/%s/%d", rename.From, failure, failAt), func(t *testing.T) {
					runCtx, plan := renameOrderingFixture(t, rename)
					opts := Options{Fix: true, NonInteractive: true}
					injected := errors.New("publication failed after mutation")
					if failure == "interruption" {
						injected = errSimulatedRepairInterruption
					}
					if failure != "none" {
						opts.repairHooks = &repairExecutionHooks{AfterMutation: func(index int, _ string) error {
							if index == failAt {
								return injected
							}
							return nil
						}}
					}
					_, err := ApplyFixPlan(context.Background(), runCtx, &plan, opts)
					if failure == "none" {
						require.NoError(t, err)
						require.NoFileExists(t, filepath.Join(runCtx.VaultPath, rename.From))
						require.Equal(t, []byte("source after\n"), mustReadFile(t, filepath.Join(runCtx.VaultPath, rename.To)))
						require.Equal(t, []byte("link after\n"), mustReadFile(t, filepath.Join(runCtx.VaultPath, "m-link.md")))
					} else {
						require.ErrorIs(t, err, injected)
						if failure == "interruption" {
							finishRenameOrderingRecovery(t, runCtx, rename)
						} else {
							require.NoError(t, StabilizePendingRepairJournals(runCtx))
							require.NoError(t, StabilizePendingRepairJournals(runCtx))
							requireRenameOrderingOriginals(t, runCtx, rename)
						}
					}
					evidence, err := DetectPendingRepairJournals(runCtx)
					require.NoError(t, err)
					require.Empty(t, evidence)
				})
			}
		}
	}
}

func TestApplyFixPlanRenameReceiptRecoversAfterDestinationPublication(t *testing.T) {
	for _, rename := range []PathRename{{From: "a-old.md", To: "z-new.md"}, {From: "z-old.md", To: "a-new.md"}} {
		t.Run(rename.From, func(t *testing.T) {
			runCtx, plan := renameOrderingFixture(t, rename)
			receipt := filepath.Join(runCtx.VaultPath, ".rhizome", "edit-receipts", "rename.json")
			_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
				Fix: true, NonInteractive: true,
				CompletionArtifact: &RepairCompletionArtifact{Path: receipt, Content: []byte("receipt\n")},
				repairHooks: &repairExecutionHooks{AfterMutation: func(_ int, path string) error {
					if path == rename.To {
						return errSimulatedRepairInterruption
					}
					return nil
				}},
			})
			require.ErrorIs(t, err, errSimulatedRepairInterruption)
			require.NoFileExists(t, filepath.Join(runCtx.VaultPath, rename.From))
			require.FileExists(t, receipt)
			finishRenameOrderingRecovery(t, runCtx, rename)
			require.NoFileExists(t, receipt)
		})
	}
}

func TestApplyFixPlanRenameRecoveryResumesInterruptedReverseRollback(t *testing.T) {
	for _, rename := range []PathRename{{From: "a-old.md", To: "z-new.md"}, {From: "z-old.md", To: "a-new.md"}} {
		for stopAfter := 1; stopAfter <= 2; stopAfter++ {
			t.Run(fmt.Sprintf("%s/%d", rename.From, stopAfter), func(t *testing.T) {
				runCtx, plan := renameOrderingFixture(t, rename)
				_, err := ApplyFixPlan(context.Background(), runCtx, &plan, Options{
					Fix: true, NonInteractive: true,
					repairHooks: &repairExecutionHooks{AfterMutation: func(index int, _ string) error {
						if index == 2 {
							return errSimulatedRepairInterruption
						}
						return nil
					}},
				})
				require.ErrorIs(t, err, errSimulatedRepairInterruption)
				require.NoFileExists(t, filepath.Join(runCtx.VaultPath, rename.From))
				visited := 0
				_, err = ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, Options{
					Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
					repairHooks: &repairExecutionHooks{BeforeRollbackMutation: func(_ int, _ string) error {
						if visited == stopAfter {
							return errSimulatedRepairInterruption
						}
						visited++
						return nil
					}},
				})
				require.ErrorIs(t, err, errSimulatedRepairInterruption)
				require.Equal(t, stopAfter, visited)
				require.NoFileExists(t, filepath.Join(runCtx.VaultPath, rename.From))
				if stopAfter == 2 || rename.To == "z-new.md" {
					require.NoFileExists(t, filepath.Join(runCtx.VaultPath, rename.To))
				}
				evidence, err := DetectPendingRepairJournals(runCtx)
				require.NoError(t, err)
				require.Len(t, evidence, 1)
				require.Equal(t, repairJournalPrepared, evidence[0].State)
				finishRenameOrderingRecovery(t, runCtx, rename)
			})
		}
	}
}

func TestApplyFixPlanLegacyRenameRecoveryPreservesManifestAndResumesRollback(t *testing.T) {
	for _, rename := range []PathRename{{From: "a-old.md", To: "z-new.md"}, {From: "z-old.md", To: "a-new.md"}} {
		for stopAfter := 0; stopAfter <= 2; stopAfter++ {
			t.Run(fmt.Sprintf("%s/%d", rename.From, stopAfter), func(t *testing.T) {
				runCtx, manifestPath := loadLegacyRenameOrderingFixture(t, rename)
				manifestBefore := mustReadFile(t, manifestPath)
				if stopAfter > 0 {
					visited := 0
					_, err := ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, Options{
						Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
						repairHooks: &repairExecutionHooks{BeforeRollbackMutation: func(_ int, _ string) error {
							if visited == stopAfter {
								return errSimulatedRepairInterruption
							}
							visited++
							return nil
						}},
					})
					require.ErrorIs(t, err, errSimulatedRepairInterruption)
					require.NoFileExists(t, filepath.Join(runCtx.VaultPath, rename.From))
				}
				require.NoError(t, StabilizePendingRepairJournals(runCtx))
				require.NoError(t, StabilizePendingRepairJournals(runCtx))
				require.Equal(t, manifestBefore, mustReadFile(t, manifestPath))
				finishRenameOrderingRecovery(t, runCtx, rename)
			})
		}
		t.Run(rename.From+"/recreated source", func(t *testing.T) {
			runCtx, manifestPath := loadLegacyRenameOrderingFixture(t, rename)
			manifestBefore := mustReadFile(t, manifestPath)
			source := filepath.Join(runCtx.VaultPath, rename.From)
			destination := filepath.Join(runCtx.VaultPath, rename.To)
			require.NoError(t, os.WriteFile(source, []byte("source before\n"), 0o640))
			for range 2 {
				err := StabilizePendingRepairJournals(runCtx)
				require.ErrorContains(t, err, "reappeared as a distinct file")
			}
			require.Equal(t, []byte("source before\n"), mustReadFile(t, source))
			require.Equal(t, []byte("source after\n"), mustReadFile(t, destination))
			require.Equal(t, []byte("link after\n"), mustReadFile(t, filepath.Join(runCtx.VaultPath, "m-link.md")))
			require.Equal(t, manifestBefore, mustReadFile(t, manifestPath))
		})
	}
}

func loadLegacyRenameOrderingFixture(t *testing.T, rename PathRename) (RunContext, string) {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join("testdata", "repair-rename-ordering", strings.TrimSuffix(rename.From, ".md")+".json"))
	require.NoError(t, err)
	var fixture struct {
		Manifest repairJournalManifest `json:"manifest"`
		Files    []struct {
			Path    string      `json:"path"`
			Content string      `json:"content"`
			Mode    os.FileMode `json:"mode"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal(encoded, &fixture))
	runCtx := repairRunContext(t, t.TempDir())
	vaultPaths, err := paths.NewVaultPaths(runCtx.VaultPath)
	require.NoError(t, err)
	manifest := fixture.Manifest
	artifactPath := func(path string) string {
		return filepath.FromSlash(strings.ReplaceAll(path, "${VAULT_ROOT}", filepath.ToSlash(vaultPaths.Root())))
	}
	for index := range manifest.Entries {
		entry := &manifest.Entries[index]
		entry.BackupPath = artifactPath(entry.BackupPath)
		entry.StagePath = artifactPath(entry.StagePath)
		entry.CasePath = artifactPath(entry.CasePath)
	}
	for index := range manifest.OwnedArtifacts {
		manifest.OwnedArtifacts[index] = artifactPath(manifest.OwnedArtifacts[index])
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	dir, err := repairJournalDir(runCtx, manifest.TransactionID)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	manifestPath := filepath.Join(dir, "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, manifestBytes, 0o600))
	for _, file := range fixture.Files {
		require.NoError(t, os.WriteFile(filepath.Join(runCtx.VaultPath, file.Path), []byte(file.Content), file.Mode))
	}
	require.NoFileExists(t, filepath.Join(runCtx.VaultPath, rename.From))
	return runCtx, manifestPath
}

func renameOrderingFixture(t *testing.T, rename PathRename) (RunContext, RepairPlan) {
	t.Helper()
	root := t.TempDir()
	before, linkBefore := []byte("source before\n"), []byte("link before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, rename.From), before, 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(root, "m-link.md"), linkBefore, 0o600))
	plan := mustRepairPlan(t, []repairPlanInput{{actionID: "rename-action", issueKey: "rename-issue", operations: []RepairOperation{
		{ID: "rename", Kind: RepairOperationRename, Path: rename.From, DestinationPath: rename.To, SourceHash: SourceHash(before), Content: []byte("source after\n")},
		{ID: "backlink", Kind: RepairOperationWrite, Path: "m-link.md", SourceHash: SourceHash(linkBefore), Content: []byte("link after\n")},
	}}})
	require.Len(t, plan.Transactions, 1)
	return repairRunContext(t, root), plan
}

func requireRenameOrderingOriginals(t *testing.T, runCtx RunContext, rename PathRename) {
	t.Helper()
	source, link := filepath.Join(runCtx.VaultPath, rename.From), filepath.Join(runCtx.VaultPath, "m-link.md")
	require.Equal(t, []byte("source before\n"), mustReadFile(t, source))
	require.Equal(t, []byte("link before\n"), mustReadFile(t, link))
	require.NoFileExists(t, filepath.Join(runCtx.VaultPath, rename.To))
	require.True(t, repairModeMatches(mustRepairMode(t, source), 0o640))
	require.True(t, repairModeMatches(mustRepairMode(t, link), 0o600))
}

func finishRenameOrderingRecovery(t *testing.T, runCtx RunContext, rename PathRename) {
	t.Helper()
	require.NoError(t, StabilizePendingRepairJournals(runCtx))
	require.NoError(t, StabilizePendingRepairJournals(runCtx))
	requireRenameOrderingOriginals(t, runCtx, rename)
	evidence, err := DetectPendingRepairJournals(runCtx)
	require.NoError(t, err)
	require.Len(t, evidence, 1)
	require.Equal(t, repairJournalPrepared, evidence[0].State)
	probe := &repairPostApplyProbe{t: t}
	_, err = ApplyFixPlan(context.Background(), runCtx, &FixPlan{}, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: probe,
	})
	require.NoError(t, err)
	require.Equal(t, []PathRename{{From: rename.To, To: rename.From}}, probe.renamed)
	evidence, err = DetectPendingRepairJournals(runCtx)
	require.NoError(t, err)
	require.Empty(t, evidence)
	requireRenameOrderingOriginals(t, runCtx, rename)
}
