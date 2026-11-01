package ledger_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/dynamotest"
	"github.com/atomicobject/rhizome/services/team-api/internal/ledger"
	"github.com/atomicobject/rhizome/services/team-api/internal/ledgertest"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
)

func TestSQLiteContract(t *testing.T) {
	ledgertest.Run(t, func(t *testing.T, now func() time.Time) func() ledger.Ledger {
		path := filepath.Join(t.TempDir(), "ledger.db")
		return func() ledger.Ledger {
			db, err := store.Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			l, err := ledger.Open(context.Background(), db, now)
			require.NoError(t, err)
			return l
		}
	})
}
func TestDynamoDBContract(t *testing.T) {
	ledgertest.Run(t, func(t *testing.T, now func() time.Time) func() ledger.Ledger {
		table := dynamotest.Open(t)
		return func() ledger.Ledger {
			l, err := ledger.OpenDynamoDB(context.Background(), table, now)
			require.NoError(t, err)
			return l
		}
	})
}

// A lease that expires between an operation's reads and its write stays
// expired: after expireDuring, the next clock reading sees the lease live and
// every later one is past its expiry.
func TestDynamoDBWritesRefuseALeaseThatExpiredDuringTheOperation(t *testing.T) {
	ctx := context.Background()
	for name, op := range map[string]func(ledger.Ledger) error{
		"heartbeat": func(l ledger.Ledger) error { return l.Heartbeat(ctx) },
		"reserve": func(l ledger.Ledger) error {
			_, err := l.Reserve(ctx, ledger.Receipt{Sub: "s1", Key: "new", Route: "r", BodyHash: "h", InputBytes: 1, Credits: 1}, roomy, roomy)
			return err
		},
		"dispatch": func(l ledger.Ledger) error { return l.MarkDispatched(ctx, "s1", "held") },
	} {
		t.Run(name, func(t *testing.T) {
			table := dynamotest.Open(t)
			base := time.Now().UTC()
			var armed atomic.Bool
			var calls atomic.Int64
			now := func() time.Time {
				switch {
				case !armed.Load():
					return base
				case calls.Add(1) == 1:
					return base.Add(ledger.LeaseDuration - time.Second)
				default:
					return base.Add(ledger.LeaseDuration + time.Second)
				}
			}
			l, err := ledger.OpenDynamoDB(ctx, table, now)
			require.NoError(t, err)
			code, err := l.Reserve(ctx, ledger.Receipt{Sub: "s1", Key: "held", Route: "r", BodyHash: "h", InputBytes: 1, Credits: 1}, roomy, roomy)
			require.NoError(t, err)
			require.Empty(t, code)
			armed.Store(true)
			require.ErrorIs(t, op(l), ledger.ErrPrecondition)
			require.ErrorIs(t, l.Heartbeat(ctx), ledger.ErrPrecondition, "the lease was not extended")
			everyone, _, err := l.Usage(ctx, base.Format(time.DateOnly))
			require.NoError(t, err)
			require.Equal(t, int64(1), everyone.Requests, "only the reservation made while the lease was live counts")
		})
	}
}

var roomy = wire.DailyLimits{Requests: 100, InputBytes: 1 << 20, ChargeCredits: 1 << 20}

func TestSQLiteUpgradeRecoversOwnerlessReceipts(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "legacy.db"))
	require.NoError(t, err)
	defer db.Close()
	now := func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) }
	old, err := ledger.Open(ctx, db, now)
	require.NoError(t, err)
	limits := wire.DailyLimits{Requests: 10, InputBytes: 1000, ChargeCredits: 100}
	for _, key := range []string{"unfinished", "settled"} {
		code, err := old.Reserve(ctx, ledger.Receipt{Sub: "s", Key: key, InputBytes: 10, Credits: 5}, limits, limits)
		require.NoError(t, err)
		require.Empty(t, code)
		require.NoError(t, old.MarkDispatched(ctx, "s", key))
	}
	require.NoError(t, old.Settle(ctx, "s", "settled", ledger.Succeeded, 2))
	// Reproduce the pre-lease layout while keeping actual legacy data.
	_, err = db.ExecContext(ctx, "ALTER TABLE ledger_receipts DROP COLUMN owner")
	require.NoError(t, err)
	upgraded, err := ledger.Open(ctx, db, now)
	require.NoError(t, err)
	require.NoError(t, upgraded.Recover(ctx))
	require.NoError(t, upgraded.Recover(ctx))
	again, err := ledger.Open(ctx, db, now)
	require.NoError(t, err)
	u, people, err := again.Usage(ctx, "2026-10-31")
	require.NoError(t, err)
	require.Equal(t, wire.DailyUsage{Requests: 2, InputBytes: 20, ReservedCredits: 7, SettledCredits: 2, Unknown: 1}, u)
	require.Equal(t, u, people["s"])
	code, err := again.Reserve(ctx, ledger.Receipt{Sub: "s", Key: "unfinished", InputBytes: 10, Credits: 5}, limits, limits)
	require.NoError(t, err)
	require.Equal(t, wire.CodeOutcomeUnknown, code)
}
