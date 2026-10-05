package validationproduct

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/validationrun"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRefreshCoordinatorPublishesMatchingAuthority(t *testing.T) {
	store := openCoordinatorStore(t)
	coordinator := NewRefreshCoordinator(RefreshCoordinatorOptions{
		Store: store, VaultDef: obsidian.VaultDefinition{Name: "test", Path: t.TempDir()},
		Run: func(context.Context) (AuthoritativeRun, error) { return cleanAuthoritativeRun(), nil },
	})

	require.NoError(t, coordinator.Refresh(context.Background()))
	state, err := store.GetValidationState(context.Background())
	require.NoError(t, err)
	require.Equal(t, state.Generation, state.PublishedGeneration)
	require.Equal(t, semdb.ValidationStatusOK, state.Status)
	_, err = coordinator.CreateRepairReview(context.Background(), state.PublishedGeneration+1, "", nil)
	require.ErrorContains(t, err, "no longer has live repair authority")
}

func TestRefreshCoordinatorCreatesReviewFromPublishedInMemoryAuthority(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o644))
	plan, err := validate.FinalizeRepairPlan(validate.RepairPlan{
		Actions: []validate.FixAction{{
			ID: "action", Check: validate.CheckLinkHygiene, Kind: validate.FixKindRewriteLinkGroup,
			Safety: validate.FixSafetySafe, Title: "Repair note", IssueKeys: []string{"issue"},
		}},
		Operations: []validate.RepairOperation{{
			ID: "operation", ActionIDs: []string{"action"}, IssueKeys: []string{"issue"},
			RequiredChecks: []string{validate.CheckLinkHygiene}, Kind: validate.RepairOperationWrite,
			Path: "note.md", SourceHash: validate.SourceHash(before), Content: []byte("after\n"),
		}},
	})
	require.NoError(t, err)
	run := cleanAuthoritativeRun()
	run.Result.Result.OK = false
	run.Result.Result.IssueCount = 1
	run.Result.Result.SelectedChecks = []string{validate.CheckLinkHygiene}
	run.Result.Result.Checks = []validate.CheckResult{{
		Name: validate.CheckLinkHygiene, IssueCount: 1,
		Issues: []validate.Issue{{Key: "issue", Code: "broken", Path: "note.md", Message: "Broken link"}},
	}}
	run.Result.EffectiveChecks = []string{validate.CheckLinkHygiene}
	run.Result.Outcomes = []validationrun.ValidationCheckOutcome{{Check: validate.CheckLinkHygiene, Outcome: validate.CheckOutcomeCompleted}}
	run.Result.Result.FixPlan = &plan
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: root}
	run.RunContext = validate.RunContext{
		VaultDef: vaultDef, VaultPath: root, VaultMgr: &liveVaultManager{def: vaultDef},
		NoteReader: &obsidian.Note{}, NoteMetadata: testProjectionNoteMetadata(t), MaxIssues: 20,
	}
	store := openCoordinatorStore(t)
	opts := optionsForCoordinatorTest(store, root, func(context.Context) (AuthoritativeRun, error) { return run, nil })
	opts.ApplyOptions = validate.Options{PostApplyRefresher: coordinatorPostApplyRefresher{}}
	coordinator := NewRefreshCoordinator(opts)

	require.NoError(t, coordinator.Refresh(context.Background()))
	review, err := coordinator.CreateRepairReview(context.Background(), 1, plan.Fingerprint, []string{"action"})
	require.NoError(t, err)
	require.Equal(t, validate.RepairReviewPending, review.State)
	require.Equal(t, []string{"note.md"}, review.AffectedPaths)
	require.Len(t, review.Preview, 1)
	require.NoError(t, coordinator.Refresh(context.Background()))
	stale, err := coordinator.GetRepairReview(review.ID)
	require.NoError(t, err)
	require.Equal(t, validate.RepairReviewStale, stale.State)
	_, _, err = coordinator.ApplyRepairReview(context.Background(), review.ID, validate.RepairReviewApplyRequest{
		Generation: 1, PlanFingerprint: review.PlanFingerprint, SelectionFingerprint: review.SelectionFingerprint,
	})
	var reviewErr *validate.RepairReviewError
	require.ErrorAs(t, err, &reviewErr)
	require.Equal(t, validate.RepairReviewErrorRevalidationRequired, reviewErr.Code)
}

func TestRefreshCoordinatorOlderAcceptedPublicationCannotReplaceNewerAuthority(t *testing.T) {
	store := openCoordinatorStore(t)
	coordinator := NewRefreshCoordinator(optionsForCoordinatorTest(store, t.TempDir(), func(context.Context) (AuthoritativeRun, error) {
		return cleanAuthoritativeRun(), nil
	}))
	firstPublished := make(chan struct{})
	releaseFirst := make(chan struct{})
	coordinator.afterPublish = func(generation int64) {
		if generation == 1 {
			close(firstPublished)
			<-releaseFirst
		}
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- coordinator.Refresh(context.Background()) }()
	<-firstPublished
	require.NoError(t, coordinator.Refresh(context.Background()))
	close(releaseFirst)
	require.ErrorIs(t, <-firstDone, ErrRefreshSuperseded)

	coordinator.authorityMu.RLock()
	require.NotNil(t, coordinator.authority)
	require.Equal(t, int64(2), coordinator.authority.generation)
	coordinator.authorityMu.RUnlock()
}

func TestRefreshCoordinatorReturnsAppliedTerminalOutcomeAfterLaterRefresh(t *testing.T) {
	root := t.TempDir()
	before := []byte("before\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0o644))
	plan, err := validate.FinalizeRepairPlan(validate.RepairPlan{
		Actions: []validate.FixAction{{
			ID: "action", Check: validate.CheckLinkHygiene, Kind: validate.FixKindRewriteLinkGroup,
			Safety: validate.FixSafetySafe, Title: "Repair note", IssueKeys: []string{"issue"},
		}},
		Operations: []validate.RepairOperation{{
			ID: "operation", ActionIDs: []string{"action"}, IssueKeys: []string{"issue"},
			RequiredChecks: []string{validate.CheckLinkHygiene}, Kind: validate.RepairOperationWrite,
			Path: "note.md", SourceHash: validate.SourceHash(before), Content: []byte("after\n"),
		}},
	})
	require.NoError(t, err)
	run := cleanAuthoritativeRun()
	run.Result.Result.OK = false
	run.Result.Result.IssueCount = 1
	run.Result.Result.SelectedChecks = []string{validate.CheckLinkHygiene}
	run.Result.Result.Checks = []validate.CheckResult{{
		Name: validate.CheckLinkHygiene, IssueCount: 1,
		Issues: []validate.Issue{{Key: "issue", Code: "broken", Path: "note.md", Message: "Broken link"}},
	}}
	run.Result.EffectiveChecks = []string{validate.CheckLinkHygiene}
	run.Result.Outcomes = []validationrun.ValidationCheckOutcome{{Check: validate.CheckLinkHygiene, Outcome: validate.CheckOutcomeCompleted}}
	run.Result.Result.FixPlan = &plan
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: root}
	run.RunContext = validate.RunContext{
		VaultDef: vaultDef, VaultPath: root, VaultMgr: &liveVaultManager{def: vaultDef},
		NoteReader: &obsidian.Note{}, NoteMetadata: testProjectionNoteMetadata(t), MaxIssues: 20,
	}
	store := openCoordinatorStore(t)
	opts := optionsForCoordinatorTest(store, root, func(context.Context) (AuthoritativeRun, error) { return run, nil })
	opts.ApplyOptions = validate.Options{PostApplyRefresher: coordinatorPostApplyRefresher{}}
	coordinator := NewRefreshCoordinator(opts)
	require.NoError(t, coordinator.Refresh(context.Background()))
	review, err := coordinator.CreateRepairReview(context.Background(), 1, plan.Fingerprint, []string{"action"})
	require.NoError(t, err)
	request := validate.RepairReviewApplyRequest{
		Generation: 1, PlanFingerprint: review.PlanFingerprint, SelectionFingerprint: review.SelectionFingerprint,
	}
	result, execution, err := coordinator.ApplyRepairReview(context.Background(), review.ID, request)
	require.NoError(t, err)
	require.NotNil(t, execution)
	require.NoError(t, coordinator.Refresh(context.Background()))
	retried, retryExecution, err := coordinator.ApplyRepairReview(context.Background(), review.ID, request)
	require.NoError(t, err)
	require.Equal(t, result.IssueCount, retried.IssueCount)
	require.Equal(t, execution.PlanFingerprint, retryExecution.PlanFingerprint)
}

func TestRunLiveRetainsProductionResultAndRunContext(t *testing.T) {
	root := writeProjectionVault(t)
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: root}
	noteMetadata := testProjectionNoteMetadata(t)

	run, err := RunLive(context.Background(), noteMetadata, vaultDef, 500)
	require.NoError(t, err)
	require.Equal(t, root, run.RunContext.VaultPath)
	require.Equal(t, vaultDef, run.RunContext.VaultDef)
	require.Equal(t, 500, run.RunContext.MaxIssues)
	require.NotNil(t, run.RunContext.NoteReader)
	require.NotEmpty(t, run.Result.EffectiveChecks)
	require.Len(t, run.Result.Outcomes, len(run.Result.EffectiveChecks))
	require.Contains(t, run.NotePaths, "notes/one.md")
}

func TestRefreshCoordinatorCancelsSupersededRun(t *testing.T) {
	store := openCoordinatorStore(t)
	firstStarted := make(chan struct{})
	var calls atomic.Int32
	coordinator := NewRefreshCoordinator(RefreshCoordinatorOptions{
		Store: store, VaultDef: obsidian.VaultDefinition{Name: "test", Path: t.TempDir()},
		Run: func(ctx context.Context) (AuthoritativeRun, error) {
			if calls.Add(1) == 1 {
				close(firstStarted)
				<-ctx.Done()
				return AuthoritativeRun{}, ctx.Err()
			}
			return cleanAuthoritativeRun(), nil
		},
	})
	firstDone := make(chan error, 1)
	go func() { firstDone <- coordinator.Refresh(context.Background()) }()
	<-firstStarted
	require.NoError(t, coordinator.Refresh(context.Background()))
	require.ErrorIs(t, <-firstDone, ErrRefreshSuperseded)
	state, err := store.GetValidationState(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), state.PublishedGeneration)
}

func TestRefreshCoordinatorAdmitsGenerationBeforeCancelingPreviousRun(t *testing.T) {
	store := openCoordinatorStore(t)
	firstStarted := make(chan struct{})
	var calls atomic.Int32
	coordinator := NewRefreshCoordinator(optionsForCoordinatorTest(store, t.TempDir(), func(ctx context.Context) (AuthoritativeRun, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-ctx.Done()
			return AuthoritativeRun{}, ctx.Err()
		}
		return cleanAuthoritativeRun(), nil
	}))
	firstDone := make(chan error, 1)
	go func() { firstDone <- coordinator.Refresh(context.Background()) }()
	<-firstStarted

	// Let the canceled run finish before the replacement's admission continues.
	// This forces the transaction ordering that previously raced in CI.
	var firstErr error
	coordinator.startMu.Lock()
	cancelFirst := coordinator.cancel
	coordinator.cancel = func() {
		cancelFirst()
		firstErr = <-firstDone
	}
	coordinator.startMu.Unlock()

	require.NoError(t, coordinator.Refresh(context.Background()))
	require.ErrorIs(t, firstErr, ErrRefreshSuperseded)
	state, err := store.GetValidationState(context.Background())
	require.NoError(t, err)
	require.Equal(t, semdb.ValidationStatusOK, state.Status)
	require.Equal(t, int64(2), state.PublishedGeneration)
}

func TestRefreshCoordinatorFailedAdmissionPreservesPreviousRun(t *testing.T) {
	store := openCoordinatorStore(t)
	firstStarted := make(chan context.Context, 1)
	releaseFirst := make(chan struct{})
	coordinator := NewRefreshCoordinator(optionsForCoordinatorTest(store, t.TempDir(), func(ctx context.Context) (AuthoritativeRun, error) {
		firstStarted <- ctx
		<-releaseFirst
		return cleanAuthoritativeRun(), nil
	}))
	firstDone := make(chan error, 1)
	go func() { firstDone <- coordinator.Refresh(context.Background()) }()
	firstCtx := <-firstStarted

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := coordinator.Refresh(ctx)
	firstRunErr := firstCtx.Err()
	close(releaseFirst)
	completionErr := <-firstDone
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, firstRunErr)
	require.NoError(t, completionErr)
	state, err := store.GetValidationState(context.Background())
	require.NoError(t, err)
	require.Equal(t, semdb.ValidationStatusOK, state.Status)
	require.Equal(t, int64(1), state.PublishedGeneration)
}

func TestRefreshCoordinatorDoesNotPublishAfterCallerCancellation(t *testing.T) {
	store := openCoordinatorStore(t)
	release := make(chan struct{})
	coordinator := NewRefreshCoordinator(optionsForCoordinatorTest(store, t.TempDir(), func(context.Context) (AuthoritativeRun, error) {
		<-release
		return cleanAuthoritativeRun(), nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- coordinator.Refresh(ctx) }()
	require.Eventually(t, func() bool {
		state, err := store.GetValidationState(context.Background())
		return err == nil && state.Status == semdb.ValidationStatusRunning
	}, time.Second, time.Millisecond)
	cancel()
	close(release)
	require.ErrorIs(t, <-done, context.Canceled)
	state, err := store.GetValidationState(context.Background())
	require.NoError(t, err)
	require.Equal(t, semdb.ValidationStatusError, state.Status)
	require.Zero(t, state.PublishedGeneration)
}

func TestRefreshCoordinatorRejectsStaleCompletionFromRunnerIgnoringCancellation(t *testing.T) {
	store := openCoordinatorStore(t)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	coordinator := NewRefreshCoordinator(optionsForCoordinatorTest(store, t.TempDir(), func(context.Context) (AuthoritativeRun, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		return cleanAuthoritativeRun(), nil
	}))
	firstDone := make(chan error, 1)
	go func() { firstDone <- coordinator.Refresh(context.Background()) }()
	<-firstStarted
	require.NoError(t, coordinator.Refresh(context.Background()))
	close(releaseFirst)
	require.ErrorIs(t, <-firstDone, ErrRefreshSuperseded)
	state, err := store.GetValidationState(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), state.PublishedGeneration)
}

func TestRefreshCoordinatorFailedRunRetainsPublishedSnapshot(t *testing.T) {
	store := openCoordinatorStore(t)
	var fail atomic.Bool
	coordinator := NewRefreshCoordinator(optionsForCoordinatorTest(store, t.TempDir(), func(context.Context) (AuthoritativeRun, error) {
		if fail.Load() {
			return AuthoritativeRun{}, errors.New("read failed")
		}
		return cleanAuthoritativeRun(), nil
	}))
	require.NoError(t, coordinator.Refresh(context.Background()))
	fail.Store(true)
	require.ErrorContains(t, coordinator.Refresh(context.Background()), "read failed")
	state, err := store.GetValidationState(context.Background())
	require.NoError(t, err)
	require.Equal(t, semdb.ValidationStatusError, state.Status)
	require.Equal(t, int64(1), state.PublishedGeneration)
	snapshot, ok, err := store.GetPublishedValidationSnapshot(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(1), snapshot.Generation)
}

func optionsForCoordinatorTest(store *semdb.Store, root string, run RefreshRun) RefreshCoordinatorOptions {
	return RefreshCoordinatorOptions{Store: store, VaultDef: obsidian.VaultDefinition{Name: "test", Path: root}, Run: run}
}

func cleanAuthoritativeRun() AuthoritativeRun {
	return AuthoritativeRun{Result: validationrun.ValidationResult{
		Result:   validate.Result{OK: true, SelectedChecks: []string{validate.CheckOntology}, Checks: []validate.CheckResult{{Name: validate.CheckOntology}}},
		Selector: "default", EffectiveChecks: []string{validate.CheckOntology},
		Outcomes: []validationrun.ValidationCheckOutcome{{Check: validate.CheckOntology, Outcome: validate.CheckOutcomeCompleted}},
	}}
}

func openCoordinatorStore(t *testing.T) *semdb.Store {
	t.Helper()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "coordinator.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

type coordinatorPostApplyRefresher struct{}

func (coordinatorPostApplyRefresher) Refresh(context.Context, *validate.IndexLockLease, []string, []validate.PathRename, []string) (validate.PostApplyRefreshResult, error) {
	return validate.NewPostApplyRefreshResult(&ontology.Runtime{}, coordinatorCloseOwner{}), nil
}

type coordinatorCloseOwner struct{}

func (coordinatorCloseOwner) Close() error { return nil }
