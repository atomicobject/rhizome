package ledger

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

var roomy = wire.DailyLimits{Requests: 100, InputBytes: 1 << 20, ChargeCredits: 1000}

func openLedger(t *testing.T) (*Ledger, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "l.db")+"?_journal_mode=WAL&_busy_timeout=5000")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	now := func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) }
	l, err := Open(context.Background(), db, now)
	require.NoError(t, err)
	return l, db
}

func receipt(key string) Receipt {
	return Receipt{Sub: "s1", Key: key, Route: "/r", BodyHash: "h", Model: "m", InputBytes: 100, Credits: 10}
}

func TestSettleAppliesOnceAndRefundsOnlyUnused(t *testing.T) {
	ctx := context.Background()
	l, _ := openLedger(t)
	code, err := l.Reserve(ctx, receipt("k1"), roomy, roomy)
	require.NoError(t, err)
	require.Empty(t, code)
	require.NoError(t, l.MarkDispatched(ctx, "s1", "k1"))
	require.NoError(t, l.Settle(ctx, "s1", "k1", Succeeded, 3))
	require.ErrorIs(t, l.Settle(ctx, "s1", "k1", Succeeded, 0), ErrPrecondition, "a duplicate settlement changes nothing")
	require.ErrorIs(t, l.Settle(ctx, "s1", "k1", NotDispatched, -1), ErrPrecondition, "a late release changes nothing")
	require.ErrorIs(t, l.MarkUnknown(ctx, "s1", "k1"), ErrPrecondition)
	require.ErrorIs(t, l.MarkDispatched(ctx, "s1", "k1"), ErrPrecondition)

	everyone, people, err := l.Usage(ctx, "2026-10-31")
	require.NoError(t, err)
	want := wire.DailyUsage{Requests: 1, InputBytes: 100, ReservedCredits: 3, SettledCredits: 3}
	require.Equal(t, want, everyone)
	require.Equal(t, want, people["s1"])

	code, err = l.Reserve(ctx, receipt("k1"), roomy, roomy)
	require.NoError(t, err)
	require.Equal(t, wire.CodeResultLost, code)
}

func TestOutcomesAndTheirReservations(t *testing.T) {
	ctx := context.Background()
	l, _ := openLedger(t)
	for _, key := range []string{"ok", "rejected", "lost", "unsent"} {
		_, err := l.Reserve(ctx, receipt(key), roomy, roomy)
		require.NoError(t, err)
		require.NoError(t, l.MarkDispatched(ctx, "s1", key))
	}
	require.NoError(t, l.Settle(ctx, "s1", "ok", Succeeded, -1))         // unknown usage keeps 10
	require.NoError(t, l.Settle(ctx, "s1", "rejected", Rejected, -1))    // releases its credits
	require.NoError(t, l.MarkUnknown(ctx, "s1", "lost"))                 // keeps 10
	require.NoError(t, l.Settle(ctx, "s1", "unsent", NotDispatched, -1)) // releases everything
	everyone, _, err := l.Usage(ctx, "2026-10-31")
	require.NoError(t, err)
	require.Equal(t, wire.DailyUsage{Requests: 3, InputBytes: 300, ReservedCredits: 20, Unknown: 1}, everyone)

	for key, want := range map[string]wire.ErrorCode{"ok": wire.CodeResultLost, "rejected": wire.CodeResultLost, "lost": wire.CodeOutcomeUnknown, "unsent": ""} {
		code, err := l.Reserve(ctx, receipt(key), roomy, roomy)
		require.NoError(t, err)
		require.Equal(t, want, code, key)
	}
}

func TestPreconditionsOnMissingAndReservedReceipts(t *testing.T) {
	ctx := context.Background()
	l, _ := openLedger(t)
	require.ErrorIs(t, l.MarkDispatched(ctx, "s1", "missing"), ErrPrecondition)
	require.ErrorIs(t, l.MarkUnknown(ctx, "s1", "missing"), ErrPrecondition)
	require.ErrorIs(t, l.Settle(ctx, "s1", "missing", Succeeded, 1), ErrPrecondition)
	_, err := l.Reserve(ctx, receipt("r"), roomy, roomy)
	require.NoError(t, err)
	require.ErrorIs(t, l.Settle(ctx, "s1", "r", Succeeded, 1), ErrPrecondition, "settlement needs dispatch first")
	require.ErrorIs(t, l.MarkUnknown(ctx, "s1", "r"), ErrPrecondition)
	code, err := l.Reserve(ctx, receipt("r"), roomy, roomy)
	require.NoError(t, err)
	require.Equal(t, wire.CodeInProgress, code)
	other := receipt("r")
	other.BodyHash = "different"
	code, err = l.Reserve(ctx, other, roomy, roomy)
	require.NoError(t, err)
	require.Equal(t, wire.CodeIdempotencyClash, code)
}

func TestRecoverMarksInFlightReceiptsUnknown(t *testing.T) {
	ctx := context.Background()
	l, _ := openLedger(t)
	_, err := l.Reserve(ctx, receipt("crashed"), roomy, roomy)
	require.NoError(t, err)
	require.NoError(t, l.MarkDispatched(ctx, "s1", "crashed"))

	require.NoError(t, l.Recover(ctx))
	everyone, people, err := l.Usage(ctx, "2026-10-31")
	require.NoError(t, err)
	require.Equal(t, int64(1), everyone.Unknown)
	require.Equal(t, int64(1), people["s1"].Unknown)
	require.Equal(t, int64(10), everyone.ReservedCredits)

	code, err := l.Reserve(ctx, receipt("crashed"), roomy, roomy)
	require.NoError(t, err)
	require.Equal(t, wire.CodeOutcomeUnknown, code)
}

func TestBudgetRejectionChangesNothing(t *testing.T) {
	ctx := context.Background()
	l, _ := openLedger(t)
	tight := wire.DailyLimits{Requests: 1, InputBytes: 1 << 20, ChargeCredits: 1000}
	code, err := l.Reserve(ctx, receipt("a"), tight, roomy)
	require.NoError(t, err)
	require.Empty(t, code)
	code, err = l.Reserve(ctx, receipt("b"), tight, roomy)
	require.NoError(t, err)
	require.Equal(t, wire.CodeBudgetExhausted, code)
	code, err = l.Reserve(ctx, receipt("b"), roomy, roomy)
	require.NoError(t, err)
	require.Empty(t, code, "a rejected key left no receipt behind")
	_, _, err = l.Usage(ctx, "10/31/2026")
	require.Error(t, err)
}

func TestSettlementAfterMidnightUpdatesAdmissionDay(t *testing.T) {
	ctx := context.Background()
	_, db := openLedger(t)
	clock := time.Date(2026, 10, 31, 23, 59, 59, 0, time.UTC)
	l, err := Open(ctx, db, func() time.Time { return clock })
	require.NoError(t, err)
	_, err = l.Reserve(ctx, receipt("late"), roomy, roomy)
	require.NoError(t, err)
	require.NoError(t, l.MarkDispatched(ctx, "s1", "late"))
	clock = clock.Add(2 * time.Second)
	require.NoError(t, l.Settle(ctx, "s1", "late", Succeeded, 4))

	day1, _, err := l.Usage(ctx, "2026-10-31")
	require.NoError(t, err)
	require.Equal(t, wire.DailyUsage{Requests: 1, InputBytes: 100, ReservedCredits: 4, SettledCredits: 4}, day1)
	day2, people, err := l.Usage(ctx, "2026-11-01")
	require.NoError(t, err)
	require.Zero(t, day2)
	require.Empty(t, people)
}
