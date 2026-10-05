package web

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/stretchr/testify/require"
)

type recordingValidationRefreshRequester struct {
	calls   atomic.Int32
	pending atomic.Bool
}

func (r *recordingValidationRefreshRequester) RequestValidationRefresh() {
	r.calls.Add(1)
	r.pending.Store(true)
}
func (r *recordingValidationRefreshRequester) ValidationRefreshPending() bool {
	return r.pending.Load()
}

func TestManualEditReservationsReleaseAndRequestValidationAfterCommit(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	paths := validate.NewRepairPathCoordinator()
	refresh := &recordingValidationRefreshRequester{}
	srv.runtime.SetRepairPathCoordinator(paths)
	srv.runtime.SetValidationRefreshRequester(refresh)
	ctx := context.Background()
	notePath := "specs/100-demo/plan.md"
	op := OntologyEditOp{Kind: "setField", Path: notePath, Field: "summary", Value: "Reserved summary"}

	created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{op}})
	require.NoError(t, err)
	_, err = paths.ReserveRepairPaths(ctx, srv.validationVaultIdentity(), []string{notePath})
	requireRepairManualOverlap(t, err)

	committed, err := srv.commitOntologyEditSessionResponse(ctx, created.SessionID, OntologyEditSessionCommitRequest{
		RequestID:        "commit-reservation",
		ExpectedRevision: created.Revision,
	})
	require.NoError(t, err)
	require.Equal(t, OntologyEditSessionStatusClean, committed.Status)
	require.Equal(t, int32(1), refresh.calls.Load())
	reservation, err := paths.ReserveRepairPaths(ctx, srv.validationVaultIdentity(), []string{notePath})
	require.NoError(t, err)
	reservation.Release()
}

func TestCommittedEditRequestsValidationWhenCacheRefreshFails(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	cacheSvc, err := cache.NewService(fixture.root, cache.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cacheSvc.Close() })
	require.NoError(t, cacheSvc.EnsureReady(t.Context()))
	srv.cfg.Cache = cacheSvc
	refresh := &recordingValidationRefreshRequester{}
	srv.runtime.SetValidationRefreshRequester(refresh)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	committed := OntologyEditSessionResponse{
		SessionID: "saved-session", Revision: 2, Status: OntologyEditSessionStatusClean,
		Outcome:      ontology.CommitOutcomeCommitted,
		TouchedPaths: []string{"specs/100-demo/plan.md"},
		Warnings:     []string{"existing warning"},
	}

	events, unsubscribe := srv.globalEvents.Subscribe()
	defer unsubscribe()
	refreshed := srv.refreshCommittedOntologyEdit(ctx, committed, &validate.PostApplyRefreshResult{Paths: committed.TouchedPaths, Domains: []string{"metadata", "ontology"}})
	select {
	case event := <-events:
		t.Fatalf("cache failure published freshness: %+v", event)
	default:
	}

	require.Equal(t, int32(1), refresh.calls.Load())
	require.Nil(t, refreshed.Workspaces)
	require.Equal(t, []string{"existing warning", "refresh committed note cache: context canceled"}, refreshed.Warnings)
	committed.Warnings = refreshed.Warnings
	require.Equal(t, committed, refreshed)
}

func TestRepairReservationBlocksManualCreateAndStageWithoutLosingDraft(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	paths := validate.NewRepairPathCoordinator()
	srv.runtime.SetRepairPathCoordinator(paths)
	ctx := context.Background()
	vault := srv.validationVaultIdentity()
	notePath := "specs/100-demo/plan.md"
	reservation, err := paths.ReserveRepairPaths(ctx, vault, []string{notePath})
	require.NoError(t, err)

	_, err = srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{{
		Kind: "setField", Path: notePath, Field: "summary", Value: "Blocked create",
	}}})
	requireRepairManualOverlap(t, err)
	clean, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{})
	require.NoError(t, err)
	_, err = srv.stageOntologyEditSessionResponse(ctx, clean.SessionID, OntologyEditSessionStageRequest{Ops: []OntologyEditOp{{
		Kind: "setField", Path: notePath, Field: "summary", Value: "Blocked stage",
	}}})
	requireRepairManualOverlap(t, err)
	current, err := srv.getOntologyEditSessionResponse(ctx, clean.SessionID)
	require.NoError(t, err)
	require.Empty(t, current.Ops)

	reservation.Release()
	staged, err := srv.stageOntologyEditSessionResponse(ctx, clean.SessionID, OntologyEditSessionStageRequest{Ops: []OntologyEditOp{{
		Kind: "setField", Path: notePath, Field: "summary", Value: "Allowed stage",
	}}})
	require.NoError(t, err)
	require.Equal(t, OntologyEditSessionStatusDirty, staged.Status)
}

func TestDeletingEditSessionRejectsAnOperationWaitingOnItsOldPointer(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	paths := validate.NewRepairPathCoordinator()
	srv.runtime.SetRepairPathCoordinator(paths)
	ctx := context.Background()
	notePath := "specs/100-demo/plan.md"
	created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{
		Ops: []OntologyEditOp{{Kind: "setField", Path: notePath, Field: "summary", Value: "Reserved summary"}},
	})
	require.NoError(t, err)
	session, ok := srv.ontologyEditSession(created.SessionID)
	require.True(t, ok)

	session.mu.Lock()
	operationReady := make(chan struct{})
	operationResult := make(chan error, 1)
	go func() {
		close(operationReady)
		lockErr := session.lockLive()
		if lockErr == nil {
			session.mu.Unlock()
		}
		operationResult <- lockErr
	}()
	<-operationReady
	deleted := make(chan bool, 1)
	go func() { deleted <- srv.deleteOntologyEditSession(created.SessionID) }()
	require.Eventually(t, session.deleted.Load, time.Second, time.Millisecond)
	session.mu.Unlock()

	require.ErrorContains(t, <-operationResult, "not found")
	require.True(t, <-deleted)
	reservation, err := paths.ReserveRepairPaths(ctx, srv.validationVaultIdentity(), []string{notePath})
	require.NoError(t, err)
	reservation.Release()
}

func TestRestoredEditSessionReservesSnapshotPaths(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	paths := validate.NewRepairPathCoordinator()
	srv.runtime.SetRepairPathCoordinator(paths)
	ctx := context.Background()
	notePath := "specs/100-demo/plan.md"
	source, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(notePath)))
	require.NoError(t, err)
	document, err := ontology.BuildDocumentSnapshot(notePath, string(source), time.Time{})
	require.NoError(t, err)
	snapshot := &OntologyEditSessionSnapshot{
		Version: 3, Revision: 1, SessionID: "restored",
		Ops: []OntologyEditOp{{
			ID: "field:summary", Kind: "setField", Path: notePath, Field: "summary",
			FieldValue: &OntologyEditFieldValue{Kind: "scalar", Scalar: "Recovered summary"},
			Expected: &OntologyEditExpected{
				Field:         &OntologyEditFieldValue{Kind: "scalar", Scalar: "Demo plan summary"},
				SourceHash:    document.ContentFingerprint,
				SourceContent: string(source),
			},
		}},
		BaseDocuments: []ontology.EditBaseDocument{{
			NotePath: notePath, Fingerprint: document.ContentFingerprint, Content: string(source),
		}},
	}

	_, err = srv.previewOntologyEditSessionResponse(ctx, "restored", OntologyEditSessionPreviewRequest{Snapshot: snapshot})
	require.NoError(t, err)
	_, err = paths.ReserveRepairPaths(ctx, srv.validationVaultIdentity(), []string{notePath})
	requireRepairManualOverlap(t, err)
}

func requireRepairManualOverlap(t *testing.T, err error) {
	t.Helper()
	var reviewErr *validate.RepairReviewError
	require.ErrorAs(t, err, &reviewErr)
	require.Equal(t, validate.RepairReviewErrorManualOverlap, reviewErr.Code)
}
