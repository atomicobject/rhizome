// Package ledgertest is the shared contract for process-owned ledgers.
package ledgertest

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/ledger"
)

// Factory allocates a fresh backing store and returns a constructor for
// independent process instances sharing it. Tests own the injected clock.
type Factory func(*testing.T, func() time.Time) func() ledger.Ledger

var roomy = wire.DailyLimits{Requests: 100, InputBytes: 1 << 20, ChargeCredits: 1000}
var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func openLedger(t *testing.T, open Factory) (ledger.Ledger, func() ledger.Ledger) {
	newInstance := open(t, func() time.Time { return t0 })
	return newInstance(), newInstance
}
func Run(t *testing.T, open Factory) {
	runProviders(t, open)
	t.Run("SettleAppliesOnceAndRefundsOnlyUnused", func(t *testing.T) { testSettleAppliesOnceAndRefundsOnlyUnused(t, open) })
	t.Run("OutcomesAndTheirReservations", func(t *testing.T) { testOutcomesAndTheirReservations(t, open) })
	t.Run("PreconditionsOnMissingAndReservedReceipts", func(t *testing.T) { testPreconditionsOnMissingAndReservedReceipts(t, open) })
	t.Run("BudgetRejectionChangesNothing", func(t *testing.T) { testBudgetRejectionChangesNothing(t, open) })
	t.Run("SettlementAfterMidnightUpdatesAdmissionDay", func(t *testing.T) { testSettlementAfterMidnightUpdatesAdmissionDay(t, open) })
	t.Run("CanaryRecovery", func(t *testing.T) { testCanaryRecovery(t, open) })
	t.Run("ConcurrentBudgets", func(t *testing.T) { testConcurrentBudgets(t, open) })
	t.Run("ConcurrentIdempotency", func(t *testing.T) { testConcurrentIdempotency(t, open) })
	t.Run("RecoveryAndSettlementRace", func(t *testing.T) { testRecoveryAndSettlementRace(t, open) })
	t.Run("HeartbeatAndRecoveryRace", func(t *testing.T) { testHeartbeatAndRecoveryRace(t, open) })
	t.Run("ForeignOwnerAndExpiredLease", func(t *testing.T) { testForeignOwnerAndExpiredLease(t, open) })
}
func receipt(key string) ledger.Receipt {
	return ledger.Receipt{Sub: "s1", Key: key, Route: "/r", BodyHash: "h", Model: "m", InputBytes: 100, Credits: 10}
}

func testSettleAppliesOnceAndRefundsOnlyUnused(t *testing.T, open Factory) {
	ctx := context.Background()
	l, _ := openLedger(t, open)
	code, err := l.Reserve(ctx, receipt("k1"), roomy, roomy)
	require.NoError(t, err)
	require.Empty(t, code)
	require.NoError(t, l.MarkDispatched(ctx, "s1", "k1"))
	require.NoError(t, l.Settle(ctx, "s1", "k1", ledger.Succeeded, 3))
	require.ErrorIs(t, l.Settle(ctx, "s1", "k1", ledger.Succeeded, 0), ledger.ErrPrecondition, "a duplicate settlement changes nothing")
	require.ErrorIs(t, l.Settle(ctx, "s1", "k1", ledger.NotDispatched, -1), ledger.ErrPrecondition, "a late release changes nothing")
	require.ErrorIs(t, l.MarkUnknown(ctx, "s1", "k1"), ledger.ErrPrecondition)
	require.ErrorIs(t, l.MarkDispatched(ctx, "s1", "k1"), ledger.ErrPrecondition)

	everyone, people, err := l.Usage(ctx, "2026-10-31")
	require.NoError(t, err)
	want := wire.DailyUsage{Requests: 1, InputBytes: 100, ReservedCredits: 3, SettledCredits: 3}
	require.Equal(t, want, everyone)
	require.Equal(t, want, people["s1"])

	code, err = l.Reserve(ctx, receipt("k1"), roomy, roomy)
	require.NoError(t, err)
	require.Equal(t, wire.CodeResultLost, code)
}

func testOutcomesAndTheirReservations(t *testing.T, open Factory) {
	ctx := context.Background()
	l, _ := openLedger(t, open)
	for _, key := range []string{"ok", "rejected", "lost", "unsent"} {
		_, err := l.Reserve(ctx, receipt(key), roomy, roomy)
		require.NoError(t, err)
		require.NoError(t, l.MarkDispatched(ctx, "s1", key))
	}
	require.NoError(t, l.Settle(ctx, "s1", "ok", ledger.Succeeded, -1))         // unknown usage keeps 10
	require.NoError(t, l.Settle(ctx, "s1", "rejected", ledger.Rejected, -1))    // releases its credits
	require.NoError(t, l.MarkUnknown(ctx, "s1", "lost"))                        // keeps 10
	require.NoError(t, l.Settle(ctx, "s1", "unsent", ledger.NotDispatched, -1)) // releases everything
	everyone, _, err := l.Usage(ctx, "2026-10-31")
	require.NoError(t, err)
	require.Equal(t, wire.DailyUsage{Requests: 3, InputBytes: 300, ReservedCredits: 20, Unknown: 1}, everyone)

	for key, want := range map[string]wire.ErrorCode{"ok": wire.CodeResultLost, "rejected": wire.CodeResultLost, "lost": wire.CodeOutcomeUnknown, "unsent": ""} {
		code, err := l.Reserve(ctx, receipt(key), roomy, roomy)
		require.NoError(t, err)
		require.Equal(t, want, code, key)
	}
}

func testPreconditionsOnMissingAndReservedReceipts(t *testing.T, open Factory) {
	ctx := context.Background()
	l, _ := openLedger(t, open)
	require.ErrorIs(t, l.MarkDispatched(ctx, "s1", "missing"), ledger.ErrPrecondition)
	require.ErrorIs(t, l.MarkUnknown(ctx, "s1", "missing"), ledger.ErrPrecondition)
	require.ErrorIs(t, l.Settle(ctx, "s1", "missing", ledger.Succeeded, 1), ledger.ErrPrecondition)
	_, err := l.Reserve(ctx, receipt("r"), roomy, roomy)
	require.NoError(t, err)
	require.ErrorIs(t, l.Settle(ctx, "s1", "r", ledger.Succeeded, 1), ledger.ErrPrecondition, "settlement needs dispatch first")
	require.ErrorIs(t, l.MarkUnknown(ctx, "s1", "r"), ledger.ErrPrecondition)
	code, err := l.Reserve(ctx, receipt("r"), roomy, roomy)
	require.NoError(t, err)
	require.Equal(t, wire.CodeInProgress, code)
	other := receipt("r")
	other.BodyHash = "different"
	code, err = l.Reserve(ctx, other, roomy, roomy)
	require.NoError(t, err)
	require.Equal(t, wire.CodeIdempotencyClash, code)
}

func testBudgetRejectionChangesNothing(t *testing.T, open Factory) {
	ctx := context.Background()
	l, _ := openLedger(t, open)
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

func testSettlementAfterMidnightUpdatesAdmissionDay(t *testing.T, open Factory) {
	ctx := context.Background()
	clock := time.Date(2026, 10, 31, 23, 59, 59, 0, time.UTC)
	l := open(t, func() time.Time { return clock })()
	_, err := l.Reserve(ctx, receipt("late"), roomy, roomy)
	require.NoError(t, err)
	require.NoError(t, l.MarkDispatched(ctx, "s1", "late"))
	clock = clock.Add(2 * time.Second)
	require.NoError(t, l.Settle(ctx, "s1", "late", ledger.Succeeded, 4))

	day1, _, err := l.Usage(ctx, "2026-10-31")
	require.NoError(t, err)
	require.Equal(t, wire.DailyUsage{Requests: 1, InputBytes: 100, ReservedCredits: 4, SettledCredits: 4}, day1)
	day2, people, err := l.Usage(ctx, "2026-11-01")
	require.NoError(t, err)
	require.Zero(t, day2)
	require.Empty(t, people)
}

func testCanaryRecovery(t *testing.T, open Factory) {
	ctx := context.Background()
	var clock atomic.Int64
	clock.Store(t0.UnixNano())
	instance := open(t, func() time.Time { return time.Unix(0, clock.Load()).UTC() })
	old := instance()
	for _, key := range []string{"reserved", "dispatched", "done"} {
		code, err := old.Reserve(ctx, receipt(key), roomy, roomy)
		require.NoError(t, err)
		require.Empty(t, code)
	}
	require.NoError(t, old.MarkDispatched(ctx, "s1", "dispatched"))
	require.NoError(t, old.MarkDispatched(ctx, "s1", "done"))
	require.NoError(t, old.Settle(ctx, "s1", "done", ledger.Succeeded, 2))
	clock.Store(t0.Add(ledger.LeaseDuration / 2).UnixNano())
	next := instance()
	require.NoError(t, next.Recover(ctx))
	require.NoError(t, old.Heartbeat(ctx))
	require.NoError(t, old.Settle(ctx, "s1", "dispatched", ledger.Succeeded, 3), "canary startup preserves the old task's active receipt")
	// Move beyond the original lease deadline: a heartbeat extended it.
	clock.Store(t0.Add(ledger.LeaseDuration + time.Second).UnixNano())
	require.NoError(t, next.Recover(ctx))
	u, _, err := next.Usage(ctx, "2026-10-31")
	require.NoError(t, err)
	require.Zero(t, u.Unknown)
	clock.Store(t0.Add(2 * ledger.LeaseDuration).UnixNano())
	survivor := instance()
	require.NoError(t, survivor.Recover(ctx))
	require.NoError(t, survivor.Recover(ctx), "recovery is idempotent")
	u, people, err := survivor.Usage(ctx, "2026-10-31")
	require.NoError(t, err)
	require.Equal(t, wire.DailyUsage{Requests: 3, InputBytes: 300, ReservedCredits: 15, SettledCredits: 5, Unknown: 1}, u)
	require.Equal(t, u, people["s1"])
	require.ErrorIs(t, old.Heartbeat(ctx), ledger.ErrPrecondition)
	require.ErrorIs(t, old.MarkDispatched(ctx, "s1", "reserved"), ledger.ErrPrecondition)
	require.ErrorIs(t, old.Settle(ctx, "s1", "reserved", ledger.NotDispatched, -1), ledger.ErrPrecondition)
	code, err := survivor.Reserve(ctx, receipt("reserved"), roomy, roomy)
	require.NoError(t, err)
	require.Equal(t, wire.CodeOutcomeUnknown, code)
}

func testConcurrentBudgets(t *testing.T, open Factory) {
	for _, dimension := range []string{"requests", "bytes", "credits"} {
		for _, global := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/global=%v", dimension, global), func(t *testing.T) {
				a, newInstance := openLedger(t, open)
				b := newInstance()
				tight := roomy
				switch dimension {
				case "requests":
					tight.Requests = 7
				case "bytes":
					tight.InputBytes = 700
				case "credits":
					tight.ChargeCredits = 70
				}
				type result struct {
					code wire.ErrorCode
					err  error
				}
				results := make(chan result, 30)
				var wg sync.WaitGroup
				for i := range 30 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						l := a
						if i%2 == 1 {
							l = b
						}
						r := receipt(fmt.Sprint(i))
						person, everyone := tight, roomy
						if global {
							r.Sub = fmt.Sprint("person", i%3)
							person, everyone = roomy, tight
						}
						code, err := l.Reserve(context.Background(), r, person, everyone)
						results <- result{code, err}
					}()
				}
				wg.Wait()
				close(results)
				admitted := 0
				for r := range results {
					require.NoError(t, r.err)
					if r.code == "" {
						admitted++
					} else {
						require.Equal(t, wire.CodeBudgetExhausted, r.code)
					}
				}
				require.Equal(t, 7, admitted)
				u, people, err := a.Usage(context.Background(), "2026-10-31")
				require.NoError(t, err)
				require.Equal(t, wire.DailyUsage{Requests: 7, InputBytes: 700, ReservedCredits: 70}, u)
				var sum wire.DailyUsage
				for _, p := range people {
					sum.Requests += p.Requests
					sum.InputBytes += p.InputBytes
					sum.ReservedCredits += p.ReservedCredits
				}
				require.Equal(t, u, sum)
			})
		}
	}
}
func testConcurrentIdempotency(t *testing.T, open Factory) {
	a, newInstance := openLedger(t, open)
	b := newInstance()
	type result struct {
		code wire.ErrorCode
		err  error
	}
	out := make(chan result, 2)
	for _, l := range []ledger.Ledger{a, b} {
		go func() {
			code, err := l.Reserve(context.Background(), receipt("same"), roomy, roomy)
			out <- result{code, err}
		}()
	}
	counts := map[wire.ErrorCode]int{}
	for range 2 {
		r := <-out
		require.NoError(t, r.err)
		counts[r.code]++
	}
	require.Equal(t, map[wire.ErrorCode]int{"": 1, wire.CodeInProgress: 1}, counts)
	u, _, err := a.Usage(context.Background(), "2026-10-31")
	require.NoError(t, err)
	require.Equal(t, int64(1), u.Requests)
}
func testRecoveryAndSettlementRace(t *testing.T, open Factory) {
	ctx := context.Background()
	var clock atomic.Int64
	clock.Store(t0.UnixNano())
	instance := open(t, func() time.Time { return time.Unix(0, clock.Load()).UTC() })
	old := instance()
	_, err := old.Reserve(ctx, receipt("race"), roomy, roomy)
	require.NoError(t, err)
	require.NoError(t, old.MarkDispatched(ctx, "s1", "race"))
	clock.Store(t0.Add(ledger.LeaseDuration).UnixNano())
	next := instance()
	errs := make(chan error, 3)
	go func() { errs <- old.Settle(ctx, "s1", "race", ledger.Succeeded, 1) }()
	for range 2 {
		go func() { errs <- next.Recover(ctx) }()
	}
	precondition := 0
	for range 3 {
		err := <-errs
		if err == ledger.ErrPrecondition {
			precondition++
		} else {
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, precondition)
	u, _, err := next.Usage(ctx, "2026-10-31")
	require.NoError(t, err)
	require.Equal(t, wire.DailyUsage{Requests: 1, InputBytes: 100, ReservedCredits: 10, Unknown: 1}, u)
}
func testHeartbeatAndRecoveryRace(t *testing.T, open Factory) {
	ctx := context.Background()
	var clock atomic.Int64
	clock.Store(t0.UnixNano())
	instance := open(t, func() time.Time { return time.Unix(0, clock.Load()).UTC() })
	a, b := instance(), instance()
	_, err := a.Reserve(ctx, receipt("live"), roomy, roomy)
	require.NoError(t, err)
	clock.Store(t0.Add(ledger.LeaseDuration - time.Second).UnixNano())
	done := make(chan error, 2)
	go func() { done <- a.Heartbeat(ctx) }()
	go func() { done <- b.Recover(ctx) }()
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	clock.Store(t0.Add(ledger.LeaseDuration + time.Second).UnixNano())
	require.NoError(t, b.Recover(ctx))
	require.NoError(t, a.MarkDispatched(ctx, "s1", "live"))
	require.NoError(t, a.Settle(ctx, "s1", "live", ledger.Succeeded, 2))
	u, _, err := a.Usage(ctx, "2026-10-31")
	require.NoError(t, err)
	require.Zero(t, u.Unknown)
}
func testForeignOwnerAndExpiredLease(t *testing.T, open Factory) {
	ctx := context.Background()
	var clock atomic.Int64
	clock.Store(t0.UnixNano())
	instance := open(t, func() time.Time { return time.Unix(0, clock.Load()).UTC() })
	a, b := instance(), instance()
	_, err := a.Reserve(ctx, receipt("owned"), roomy, roomy)
	require.NoError(t, err)
	require.ErrorIs(t, b.MarkDispatched(ctx, "s1", "owned"), ledger.ErrPrecondition)
	require.NoError(t, a.MarkDispatched(ctx, "s1", "owned"))
	require.ErrorIs(t, b.MarkUnknown(ctx, "s1", "owned"), ledger.ErrPrecondition)
	require.ErrorIs(t, b.Settle(ctx, "s1", "owned", ledger.Succeeded, 0), ledger.ErrPrecondition)
	require.NoError(t, a.Settle(ctx, "s1", "owned", ledger.NotDispatched, -1))
	code, err := b.Reserve(ctx, receipt("owned"), roomy, roomy)
	require.NoError(t, err)
	require.Empty(t, code)
	require.ErrorIs(t, a.MarkDispatched(ctx, "s1", "owned"), ledger.ErrPrecondition, "released key now belongs to another process")
	clock.Store(t0.Add(ledger.LeaseDuration).UnixNano())
	require.ErrorIs(t, b.Heartbeat(ctx), ledger.ErrPrecondition, "expiry boundary cannot renew")
	_, err = b.Reserve(ctx, receipt("expired"), roomy, roomy)
	require.ErrorIs(t, err, ledger.ErrPrecondition)
	require.ErrorIs(t, b.MarkDispatched(ctx, "s1", "owned"), ledger.ErrPrecondition)
	c := instance()
	require.NoError(t, c.Recover(ctx))
	require.ErrorIs(t, b.Heartbeat(ctx), ledger.ErrPrecondition)
}
