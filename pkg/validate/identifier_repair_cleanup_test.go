package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestIdentifierRepairAdapterCleanupRecoveryUsesCanonicalReviewedChecksAndStopsBeforeRemap(t *testing.T) {
	tests := []struct {
		name               string
		boundary           string
		wantEvidenceChecks []string
	}{
		{
			name:               "committed marker already removed",
			boundary:           cleanupAfterCommittedRemoved,
			wantEvidenceChecks: []string{CheckBrokenLinks, CheckIdentifiers, CheckOntology},
		},
		{
			name:     "manifest already removed",
			boundary: cleanupAfterManifestRemoved,
			// The detached directory remains terminal authority after its
			// best-effort manifest is gone. Adapter validation must recover
			// the exact check union from the canonical reviewed plan.
			wantEvidenceChecks: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := buildIdentifierRepairAdapterFixture(t)
			plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
			require.NoError(t, err)
			require.Equal(t, []string{CheckBrokenLinks, CheckIdentifiers, CheckOntology}, plan.Transactions[0].Checks)

			interrupted := errors.New("injected identifier cleanup interruption")
			firstRefresh := &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx}
			firstExecution, err := ApplyIdentifierRepairPlan(
				context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings,
				Options{
					Fix: true, NonInteractive: true, PostApplyRefresher: firstRefresh,
					repairHooks: &repairExecutionHooks{AfterCleanupBoundary: func(boundary, _ string) error {
						if boundary == tt.boundary {
							return interrupted
						}
						return nil
					}},
				},
			)
			require.ErrorIs(t, err, interrupted)
			require.NotNil(t, firstExecution)
			require.Equal(t, []string{fixture.bindings[0].Action.ID}, firstExecution.Applied)
			require.Equal(t, 1, firstRefresh.calls)

			evidence, detectErr := DetectPendingRepairJournals(fixture.runCtx)
			require.NoError(t, detectErr)
			require.Len(t, evidence, 1)
			require.Equal(t, repairJournalCleanupPending, evidence[0].State)
			require.Equal(t, tt.wantEvidenceChecks, evidence[0].RequiredChecks)

			destinationPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.newPath))
			inboundPath := filepath.Join(fixture.root, "notes", "inbound.md")
			destinationAfterFirstApply, readErr := os.ReadFile(destinationPath)
			require.NoError(t, readErr)
			inboundAfterFirstApply, readErr := os.ReadFile(inboundPath)
			require.NoError(t, readErr)
			_, statErr := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.oldPath)))
			require.ErrorIs(t, statErr, os.ErrNotExist)

			secondRefresh := &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx}
			secondExecution, err := ApplyIdentifierRepairPlan(
				context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings,
				Options{
					Fix: true, NonInteractive: true,
					// Invalid current selection must not override recovery metadata
					// or the canonical reviewed transaction check union.
					Checks:             []string{"not-a-real-check"},
					ReplanCommand:      "rzm validate fix identifiers",
					PostApplyRefresher: secondRefresh,
				},
			)
			require.ErrorContains(t, err, "terminal repair cleanup recovered; replan before applying new transactions")
			require.NotErrorIs(t, err, interrupted)
			require.NotContains(t, err.Error(), "source inventory changed", "cleanup recovery must not invoke the held mapper")
			require.NotNil(t, secondExecution)
			if tt.wantEvidenceChecks != nil {
				require.Equal(t, []string{fixture.bindings[0].Action.ID}, secondExecution.Applied)
				require.Empty(t, secondExecution.Failed)
				require.NotContains(t, secondExecution.RemainingIssueKeys, fixture.bindings[0].Action.IssueKeys[0])
			} else {
				require.Empty(t, secondExecution.Applied)
				require.Equal(t, []string{fixture.bindings[0].Action.ID}, secondExecution.Failed)
				require.Contains(t, secondExecution.RemainingIssueKeys, fixture.bindings[0].Action.IssueKeys[0])
			}
			require.Equal(t, "rzm validate fix identifiers", secondExecution.ReplanCommand)
			require.Equal(t, 1, secondRefresh.calls)
			require.Empty(t, secondRefresh.changed[0], "cleanup-only recovery refreshes with zero vault delta")
			require.Empty(t, secondRefresh.renamed[0], "cleanup-only recovery refreshes with zero vault delta")
			require.Empty(t, secondRefresh.deleted[0], "cleanup-only recovery refreshes with zero vault delta")
			requireIdentifierCleanupRecoveryStatuses(t, secondExecution)

			destinationAfterRecovery, readErr := os.ReadFile(destinationPath)
			require.NoError(t, readErr)
			require.Equal(t, destinationAfterFirstApply, destinationAfterRecovery, "cleanup recovery must not reapply the committed write")
			inboundAfterRecovery, readErr := os.ReadFile(inboundPath)
			require.NoError(t, readErr)
			require.Equal(t, inboundAfterFirstApply, inboundAfterRecovery, "cleanup recovery must not rewrite inbound links twice")
			evidence, detectErr = DetectPendingRepairJournals(fixture.runCtx)
			require.NoError(t, detectErr)
			require.Empty(t, evidence, "successful cleanup-only refresh and postcheck must converge the tombstone")

			// The recovery barrier consumed this call. Reusing the stale sealed
			// assembly on a later call cannot silently replay the repair; the
			// caller must build and review a fresh plan.
			thirdRefresh := &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx}
			thirdExecution, err := ApplyIdentifierRepairPlan(
				context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings,
				Options{Fix: true, NonInteractive: true, PostApplyRefresher: thirdRefresh},
			)
			require.ErrorContains(t, err, "source inventory changed")
			require.NotNil(t, thirdExecution)
			require.Empty(t, thirdExecution.Applied)
			require.Zero(t, thirdRefresh.calls)
		})
	}
}

func TestIdentifierRepairAdapterCleanupRecoveryPropagatesRefreshAndPostcheckErrors(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*testing.T, identifierRepairAdapterFixture) (RunContext, PostApplyRefresher)
		wantErr   string
	}{
		{
			name: "refresh failure",
			configure: func(_ *testing.T, fixture identifierRepairAdapterFixture) (RunContext, PostApplyRefresher) {
				return fixture.runCtx, identifierFailingRefresher{err: errors.New("injected cleanup refresh failure")}
			},
			wantErr: "injected cleanup refresh failure",
		},
		{
			name: "held postcheck failure",
			configure: func(t *testing.T, fixture identifierRepairAdapterFixture) (RunContext, PostApplyRefresher) {
				runCtx := fixture.runCtx
				runCtx.NoteReader = identifierErrorNoteReader{
					NoteReader: runCtx.NoteReader,
					err:        errors.New("injected cleanup check failure"),
				}
				return runCtx, &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx}
			},
			wantErr: "injected cleanup check failure",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := buildIdentifierRepairAdapterFixture(t)
			plan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
			require.NoError(t, err)
			interrupted := errors.New("injected cleanup interruption")
			_, err = ApplyIdentifierRepairPlan(context.Background(), fixture.runCtx, plan, fixture.assembly, fixture.bindings, Options{
				Fix: true, NonInteractive: true,
				PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
				repairHooks: &repairExecutionHooks{AfterCleanupBoundary: func(boundary, _ string) error {
					if boundary == cleanupAfterManifestRemoved {
						return interrupted
					}
					return nil
				}},
			})
			require.ErrorIs(t, err, interrupted)
			destination := mustReadFile(t, filepath.Join(fixture.root, filepath.FromSlash(fixture.newPath)))
			inbound := mustReadFile(t, filepath.Join(fixture.root, "notes", "inbound.md"))

			runCtx, refresher := tt.configure(t, fixture)
			execution, err := ApplyIdentifierRepairPlan(context.Background(), runCtx, plan, fixture.assembly, fixture.bindings, Options{
				Fix: true, NonInteractive: true, PostApplyRefresher: refresher,
			})
			require.ErrorContains(t, err, tt.wantErr)
			require.NotContains(t, err.Error(), "source inventory changed", "terminal cleanup recovery must not invoke the held mapper")
			require.NotNil(t, execution)
			require.Equal(t, []string{fixture.bindings[0].Action.ID}, execution.Failed)
			require.Contains(t, execution.RemainingIssueKeys, fixture.bindings[0].Action.IssueKeys[0])
			require.Equal(t, destination, mustReadFile(t, filepath.Join(fixture.root, filepath.FromSlash(fixture.newPath))))
			require.Equal(t, inbound, mustReadFile(t, filepath.Join(fixture.root, "notes", "inbound.md")))
			evidence, detectErr := DetectPendingRepairJournals(fixture.runCtx)
			require.NoError(t, detectErr)
			require.Empty(t, evidence, "terminal cleanup evidence was already durably removed before refresh/postcheck")
		})
	}
}

type identifierFailingRefresher struct{ err error }

func (r identifierFailingRefresher) Refresh(
	context.Context,
	*IndexLockLease,
	[]string,
	[]PathRename,
	[]string,
) (PostApplyRefreshResult, error) {
	return PostApplyRefreshResult{}, r.err
}

type identifierErrorNoteReader struct {
	obsidian.NoteReader
	err error
}

func (r identifierErrorNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return nil, r.err
}

var _ PostApplyRefresher = identifierFailingRefresher{}
var _ obsidian.NoteReader = identifierErrorNoteReader{}

func requireIdentifierCleanupRecoveryStatuses(t *testing.T, execution *FixExecution) {
	t.Helper()
	statuses := make([]string, 0, len(execution.Transactions))
	for _, transaction := range execution.Transactions {
		statuses = append(statuses, transaction.Status)
	}
	require.Contains(t, statuses, "recovered_cleanup")
	require.Contains(t, statuses, "not_attempted")
}
