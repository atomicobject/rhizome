package mcp

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/stretchr/testify/require"
)

type failCommitSessionStore struct {
	*semdb.Store
	commitErr          error
	releaseErr         error
	releaseErrAtCall   error
	releaseHasDeadline bool
}

func (s *failCommitSessionStore) CommitSessionItems(context.Context, string, string, []semdb.SessionItem) error {
	return s.commitErr
}

func (s *failCommitSessionStore) ReleaseSessionReservations(ctx context.Context, sessionID, reservationID string) error {
	s.releaseErrAtCall = ctx.Err()
	_, s.releaseHasDeadline = ctx.Deadline()
	if s.releaseErr != nil {
		return s.releaseErr
	}
	return s.Store.ReleaseSessionReservations(ctx, sessionID, reservationID)
}

func TestSessionReservationReportsCommitAndReleaseFailures(t *testing.T) {
	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	store := &failCommitSessionStore{
		commitErr:  errors.New("commit failed"),
		releaseErr: errors.New("release failed"),
	}
	reservation := &sessionReservation{
		ctx:           ctx,
		store:         store,
		sessionID:     "session",
		reservationID: "owner",
		allowed:       map[string]string{"emitted": "fp-emitted"},
	}

	reservation.Commit([]actions.DedupeItem{{Key: "emitted", Fingerprint: "fp-emitted"}})

	counts := make(map[string]int64)
	for _, operation := range collector.AgentStartDiagnostics().Operations {
		counts[operation.Label] = operation.Count
	}
	require.Equal(t, int64(1), counts[indexingperf.AgentStartOpSessionCommitFailures])
	require.Equal(t, int64(1), counts[indexingperf.AgentStartOpSessionReleaseFailures])
}

func TestSessionTrackerBatchCommitsOnlyEmittedAndUsesTwoTransactions(t *testing.T) {
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "session.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	tracker := &sessionTracker{ctx: ctx, store: store, sessionID: "session"}
	items := []actions.DedupeItem{
		{Key: "emitted", Fingerprint: "fp-emitted"},
		{Key: "omitted", Fingerprint: "fp-omitted"},
	}
	reservation := tracker.Reserve(items)
	require.True(t, reservation.Allowed("emitted", "fp-emitted"))
	require.True(t, reservation.Allowed("omitted", "fp-omitted"))
	reservation.Commit(items[:1])

	next := tracker.Reserve(items)
	require.False(t, next.Allowed("emitted", "fp-emitted"))
	require.True(t, next.Allowed("omitted", "fp-omitted"))
	next.Commit(nil)

	var transactions int64
	for _, operation := range collector.AgentStartDiagnostics().Operations {
		if operation.Label == indexingperf.AgentStartOpTransactions {
			transactions = operation.Count
		}
	}
	require.Equal(t, int64(4), transactions, "each invocation should reserve and commit once")
}

func TestSessionReservationCommitFailureReleasesOwnershipWithFreshBoundedContext(t *testing.T) {
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "session.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	collector := indexingperf.New()
	requestCtx, cancel := context.WithCancel(indexingperf.WithCollector(context.Background(), collector))
	failingStore := &failCommitSessionStore{Store: store, commitErr: errors.New("commit failed")}
	tracker := &sessionTracker{ctx: requestCtx, store: failingStore, sessionID: "session"}
	items := []actions.DedupeItem{{Key: "emitted", Fingerprint: "fp-emitted"}}
	reservation := tracker.Reserve(items)
	require.True(t, reservation.Allowed("emitted", "fp-emitted"))

	cancel()
	reservation.Commit(items)

	require.NoError(t, failingStore.releaseErrAtCall, "release must not inherit request cancellation")
	require.True(t, failingStore.releaseHasDeadline, "best-effort release must be bounded")

	next := (&sessionTracker{ctx: context.Background(), store: store, sessionID: "session"}).Reserve(items)
	require.True(t, next.Allowed("emitted", "fp-emitted"), "failed persistence must not suppress later context")
	next.Commit(nil)

	counts := make(map[string]int64)
	for _, operation := range collector.AgentStartDiagnostics().Operations {
		counts[operation.Label] = operation.Count
	}
	require.Equal(t, int64(1), counts[indexingperf.AgentStartOpSessionCommitFailures])
	require.Equal(t, int64(1), counts[indexingperf.AgentStartOpSessionReleases])
	require.Zero(t, counts[indexingperf.AgentStartOpSessionReleaseFailures])
}
