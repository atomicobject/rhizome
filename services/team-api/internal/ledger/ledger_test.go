package ledger_test

import (
	"context"
	"path/filepath"
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
