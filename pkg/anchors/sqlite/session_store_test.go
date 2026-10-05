package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/require"
)

func TestSessionStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "session.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	sessionID := "sess-1"
	require.NoError(t, store.EnsureSession(ctx, sessionID))

	fingerprint, ok, err := store.SessionItemFingerprint(ctx, sessionID, "item:one")
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, fingerprint)

	require.NoError(t, store.MarkSessionItem(ctx, sessionID, "item:one", "fp1"))
	fingerprint, ok, err = store.SessionItemFingerprint(ctx, sessionID, "item:one")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "fp1", fingerprint)

	require.NoError(t, store.MarkSessionItem(ctx, sessionID, "item:one", "fp2"))
	fingerprint, ok, err = store.SessionItemFingerprint(ctx, sessionID, "item:one")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "fp2", fingerprint)
}

func TestSessionStoreCleanup(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "cleanup.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	sessionID := "stale-session"
	require.NoError(t, store.EnsureSession(ctx, sessionID))
	require.NoError(t, store.MarkSessionItem(ctx, sessionID, "item:one", "fp1"))
	reserved, err := store.ReserveSessionItems(ctx, sessionID, "owner", []SessionItem{{Key: "item:two", Fingerprint: "fp2"}})
	require.NoError(t, err)
	require.Len(t, reserved, 1)

	_, err = store.db.ExecContext(ctx, `UPDATE mcp_sessions SET last_seen_at = ? WHERE session_id = ?`, 0, sessionID)
	require.NoError(t, err)

	removed, err := store.CleanupSessions(ctx, time.Unix(10, 0))
	require.NoError(t, err)
	require.EqualValues(t, 1, removed)

	fingerprint, ok, err := store.SessionItemFingerprint(ctx, sessionID, "item:one")
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, fingerprint)
	var reservations int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_session_item_reservations WHERE session_id = ?`, sessionID).Scan(&reservations))
	require.Zero(t, reservations)
}

func TestAllowSessionItem(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "allow.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	sessionID := "sess-allow"
	require.NoError(t, store.EnsureSession(ctx, sessionID))

	allowed, err := store.AllowSessionItem(ctx, sessionID, "item:one", "fp1")
	require.NoError(t, err)
	require.True(t, allowed)

	fingerprint, ok, err := store.SessionItemFingerprint(ctx, sessionID, "item:one")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "fp1", fingerprint)

	allowed, err = store.AllowSessionItem(ctx, sessionID, "item:one", "fp1")
	require.NoError(t, err)
	require.False(t, allowed)

	allowed, err = store.AllowSessionItem(ctx, sessionID, "item:one", "fp2")
	require.NoError(t, err)
	require.True(t, allowed)

	fingerprint, ok, err = store.SessionItemFingerprint(ctx, sessionID, "item:one")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "fp2", fingerprint)
}

func TestLegacySessionMethodsReportTransactions(t *testing.T) {
	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	store, err := Open(currentSchemaTestDBPath(t, "legacy-metrics.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.EnsureSession(ctx, "sess-metrics"))
	allowed, err := store.AllowSessionItem(ctx, "sess-metrics", "item:one", "fp1")
	require.NoError(t, err)
	require.True(t, allowed)
	require.NoError(t, store.MarkSessionItem(ctx, "sess-metrics", "item:one", "fp1"))
	_, err = store.CleanupSessions(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)

	diagnostics := collector.AgentStartDiagnostics()
	counts := make(map[string]int64, len(diagnostics.Operations))
	for _, operation := range diagnostics.Operations {
		counts[operation.Label] = operation.Count
	}
	require.Equal(t, int64(4), counts[indexingperf.AgentStartOpTransactions])
	require.Equal(t, int64(1), counts[indexingperf.AgentStartOpSessionReserves])
	require.Equal(t, int64(1), counts[indexingperf.AgentStartOpSessionCommits])
	require.Equal(t, int64(1), counts[indexingperf.AgentStartOpSessionCleanups])
}

func TestAllowSessionItem_Concurrent(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "allow-concurrent.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	sessionID := "sess-concurrent"
	require.NoError(t, store.EnsureSession(ctx, sessionID))

	var allowedCount int32
	var wg sync.WaitGroup
	errCh := make(chan error, 25)
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed, err := store.AllowSessionItem(ctx, sessionID, "item:one", "fp1")
			if err != nil {
				errCh <- err
				return
			}
			if allowed {
				atomic.AddInt32(&allowedCount, 1)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, allowedCount)
}

func TestReserveSessionItemsPreservesCommittedUntilOwnedCommit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "reserve.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.MarkSessionItem(ctx, "sess", "changed", "old"))
	reserved, err := store.ReserveSessionItems(ctx, "sess", "owner-a", []SessionItem{
		{Key: "changed", Fingerprint: "new"},
		{Key: "fresh", Fingerprint: "fp"},
		{Key: "changed", Fingerprint: "new"},
	})
	require.NoError(t, err)
	require.Equal(t, []SessionItem{{Key: "changed", Fingerprint: "new"}, {Key: "fresh", Fingerprint: "fp"}}, reserved)

	fingerprint, ok, err := store.SessionItemFingerprint(ctx, "sess", "changed")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "old", fingerprint)

	require.NoError(t, store.CommitSessionItems(ctx, "sess", "owner-a", []SessionItem{{Key: "changed", Fingerprint: "new"}}))
	fingerprint, ok, err = store.SessionItemFingerprint(ctx, "sess", "changed")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "new", fingerprint)

	// The omitted item was released rather than marked seen.
	reserved, err = store.ReserveSessionItems(ctx, "sess", "owner-b", []SessionItem{{Key: "fresh", Fingerprint: "fp"}})
	require.NoError(t, err)
	require.Len(t, reserved, 1)
}

func TestReserveSessionItemsExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "reserve-concurrent.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	var winners int32
	var wg sync.WaitGroup
	errCh := make(chan error, 25)
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			reserved, reserveErr := store.ReserveSessionItems(ctx, "sess", owner, []SessionItem{{Key: "item", Fingerprint: "fp"}})
			if reserveErr != nil {
				errCh <- reserveErr
				return
			}
			if len(reserved) == 1 {
				atomic.AddInt32(&winners, 1)
			}
		}(fmt.Sprintf("owner-%d", i))
	}
	wg.Wait()
	close(errCh)
	for reserveErr := range errCh {
		require.NoError(t, reserveErr)
	}
	require.EqualValues(t, 1, winners)
}

func TestReserveSessionItemsReclaimsOnlyStaleReservations(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "reserve-stale.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	item := []SessionItem{{Key: "item", Fingerprint: "fp"}}
	now := time.Unix(10_000, 0)
	reserved, err := store.reserveSessionItemsAt(ctx, "sess", "owner-a", item, now)
	require.NoError(t, err)
	require.Len(t, reserved, 1)
	reserved, err = store.reserveSessionItemsAt(ctx, "sess", "owner-b", item, now)
	require.NoError(t, err)
	require.Empty(t, reserved)

	_, err = store.db.ExecContext(ctx, `UPDATE mcp_session_item_reservations SET reserved_at = ?`, now.Add(SessionReservationFutureSkew).Unix())
	require.NoError(t, err)
	reserved, err = store.reserveSessionItemsAt(ctx, "sess", "owner-b", item, now)
	require.NoError(t, err)
	require.Empty(t, reserved, "future timestamps inside the skew bound must not be reclaimed")

	_, err = store.db.ExecContext(ctx, `UPDATE mcp_session_item_reservations SET reserved_at = ?`, now.Add(SessionReservationFutureSkew+time.Second).Unix())
	require.NoError(t, err)
	reserved, err = store.reserveSessionItemsAt(ctx, "sess", "owner-b", item, now)
	require.NoError(t, err)
	require.Len(t, reserved, 1, "far-future timestamps must be reclaimable after clock rollback")

	_, err = store.db.ExecContext(ctx, `UPDATE mcp_session_item_reservations SET reservation_id = ?, reserved_at = ?`, "owner-a", now.Add(-SessionReservationTTL-time.Second).Unix())
	require.NoError(t, err)
	reserved, err = store.reserveSessionItemsAt(ctx, "sess", "owner-b", item, now)
	require.NoError(t, err)
	require.Len(t, reserved, 1)
}

func TestReserveAndCommitSessionItemsChunkLargeBatchesInOneTransaction(t *testing.T) {
	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	store, err := Open(currentSchemaTestDBPath(t, "reserve-large.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	items := make([]SessionItem, 0, sessionItemSQLChunkSize+17)
	for i := 0; i < cap(items); i++ {
		items = append(items, SessionItem{Key: fmt.Sprintf("item-%04d", i), Fingerprint: fmt.Sprintf("fp-%04d", i)})
	}
	reserved, err := store.ReserveSessionItems(ctx, "sess", "owner", items)
	require.NoError(t, err)
	require.Equal(t, items, reserved, "results must retain input order across SQL chunks")
	require.NoError(t, store.CommitSessionItems(ctx, "sess", "owner", items))

	summary := collector.RenderSummary()
	require.Contains(t, summary, "intel.reserve_session_items count=1")
	require.Contains(t, summary, "intel.commit_session_items count=1")
}

func TestSessionReservationMigrationPreservesCommittedRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reservation-migration.db")
	store, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, store.MarkSessionItem(ctx, "sess", "item", "committed"))
	_, err = store.db.ExecContext(ctx, `
		DROP TABLE mcp_session_item_reservations;
		DROP TABLE mcp_session_maintenance;
		UPDATE schema_version SET version = 59;
		UPDATE rzm_migration_state SET version = 59 WHERE domain = 'intel';
		DELETE FROM rzm_migration_log WHERE domain = 'intel' AND from_version >= 59;
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	fingerprint, ok, err := store.SessionItemFingerprint(ctx, "sess", "item")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "committed", fingerprint)
	for _, table := range []string{"mcp_session_item_reservations", "mcp_session_maintenance"} {
		exists, tableErr := store.tableExists(ctx, table)
		require.NoError(t, tableErr)
		require.True(t, exists, table)
	}
}

func TestCommitSessionItemsCannotCommitAnotherOwnersReservation(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "reserve-owner.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	item := SessionItem{Key: "item", Fingerprint: "fp"}
	reserved, err := store.ReserveSessionItems(ctx, "sess", "owner-a", []SessionItem{item})
	require.NoError(t, err)
	require.Len(t, reserved, 1)
	require.NoError(t, store.CommitSessionItems(ctx, "sess", "owner-b", []SessionItem{item}))
	_, ok, err := store.SessionItemFingerprint(ctx, "sess", "item")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestSessionCleanupDueClaimIsConditional(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "cleanup-due.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	now := time.Unix(1000, 0)
	due, err := store.SessionCleanupDue(ctx, now)
	require.NoError(t, err)
	require.True(t, due)
	claimed, err := store.ClaimSessionCleanup(ctx, now, now.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = store.ClaimSessionCleanup(ctx, now, now.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, claimed)
	due, err = store.SessionCleanupDue(ctx, now)
	require.NoError(t, err)
	require.False(t, due)
}

func TestRelinquishSessionCleanupMakesFailedClaimDueAgain(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "cleanup-relinquish.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	now := time.Unix(1000, 0)
	claimedUntil := now.Add(time.Hour)
	claimed, err := store.ClaimSessionCleanup(ctx, now, claimedUntil)
	require.NoError(t, err)
	require.True(t, claimed)

	relinquished, err := store.RelinquishSessionCleanup(ctx, claimedUntil, now)
	require.NoError(t, err)
	require.True(t, relinquished)
	due, err := store.SessionCleanupDue(ctx, now)
	require.NoError(t, err)
	require.True(t, due)
}

func TestRelinquishSessionCleanupCannotClobberSuccessorClaim(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "cleanup-fenced-relinquish.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	firstNow := time.Unix(1000, 0)
	firstClaimUntil := firstNow.Add(time.Hour)
	claimed, err := store.ClaimSessionCleanup(ctx, firstNow, firstClaimUntil)
	require.NoError(t, err)
	require.True(t, claimed)

	secondClaimUntil := firstClaimUntil.Add(time.Hour)
	claimed, err = store.ClaimSessionCleanup(ctx, firstClaimUntil, secondClaimUntil)
	require.NoError(t, err)
	require.True(t, claimed)

	relinquished, err := store.RelinquishSessionCleanup(ctx, firstClaimUntil, firstNow)
	require.NoError(t, err)
	require.False(t, relinquished)
	due, err := store.SessionCleanupDue(ctx, firstClaimUntil)
	require.NoError(t, err)
	require.False(t, due, "a stale owner must not reset a successor's claim")
}
