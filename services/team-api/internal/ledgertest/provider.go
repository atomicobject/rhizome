package ledgertest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/ledger"
	"github.com/stretchr/testify/require"
)

// All currency amounts here are synthetic nanodollars, not deployment limits.
func pricedReceipt(key, provider string) ledger.Receipt {
	r := receipt(key)
	r.Provider, r.PriceVersion = provider, "synthetic-v1"
	r.ReservedNanodollars, r.PriceNanodollarsPerToken, r.MaxBillableTokens = 20, 2, 10
	r.ProviderLimitNanodollars = 1000
	return r
}

func runProviders(t *testing.T, open Factory) {
	t.Run("PriceBoundFenceSurvivesMidnightAndProcessRestart", func(t *testing.T) {
		for _, settleAfterMidnight := range []bool{false, true} {
			t.Run(fmt.Sprintf("settleAfterMidnight=%v", settleAfterMidnight), func(t *testing.T) {
				ctx := context.Background()
				clock := time.Date(2026, 10, 31, 23, 59, 59, 0, time.UTC)
				instance := open(t, func() time.Time { return clock })
				old := instance()
				r := pricedReceipt("yesterday", "voyage")
				code, err := old.Reserve(ctx, r, roomy, roomy)
				require.NoError(t, err)
				require.Empty(t, code)
				require.NoError(t, old.MarkDispatched(ctx, r.Sub, r.Key))
				if settleAfterMidnight {
					clock = clock.Add(2 * time.Second)
				}
				require.NoError(t, old.Settle(ctx, r.Sub, r.Key, ledger.Succeeded, 3, 21))
				if !settleAfterMidnight {
					clock = clock.Add(2 * time.Second)
				}
				restarted := instance()
				global, people, err := restarted.ProviderUsage(ctx, restarted.Day())
				require.NoError(t, err)
				require.Equal(t, wire.ProviderUsage{Fenced: true}, global["voyage"], "an empty new day's report exposes the current persistent fence")
				require.Empty(t, people)
				yesterday, _, err := restarted.ProviderUsage(ctx, "2026-10-31")
				require.NoError(t, err)
				require.Equal(t, wire.ProviderUsage{Requests: 1, ReservedNanodollars: 20, Unknown: 1, Fenced: true}, yesterday["voyage"])
				r.Key, r.ProviderLimitNanodollars = "today", 1000000
				code, err = restarted.Reserve(ctx, r, roomy, roomy)
				require.NoError(t, err)
				require.Equal(t, wire.CodeBudgetExhausted, code, "a higher daily cap cannot clear a price-assumption fence")
				r.Provider = "typesafe"
				code, err = restarted.Reserve(ctx, r, roomy, roomy)
				require.NoError(t, err)
				require.Empty(t, code, "the other provider remains independent")
				clock = clock.Add(24 * time.Hour)
				restarted = instance()
				r.Key, r.Provider = "tomorrow", "voyage"
				code, err = restarted.Reserve(ctx, r, roomy, roomy)
				require.NoError(t, err)
				require.Equal(t, wire.CodeBudgetExhausted, code, "further UTC rollovers also preserve the fence")
			})
		}
	})
	t.Run("IndependentProviderBudgetsAndAtomicRejection", func(t *testing.T) {
		ctx := context.Background()
		l, _ := openLedger(t, open)
		r := pricedReceipt("voyage-full", "voyage")
		r.ProviderLimitNanodollars = 20
		code, err := l.Reserve(ctx, r, roomy, roomy)
		require.NoError(t, err)
		require.Empty(t, code)
		r.Key = "voyage-retry"
		code, err = l.Reserve(ctx, r, roomy, roomy)
		require.NoError(t, err)
		require.Equal(t, wire.CodeBudgetExhausted, code)
		r.Key, r.Provider = "typesafe-full", "typesafe"
		code, err = l.Reserve(ctx, r, roomy, roomy)
		require.NoError(t, err)
		require.Empty(t, code)
		r.Key = "disabled"
		r.ProviderLimitNanodollars = 0
		code, err = l.Reserve(ctx, r, roomy, roomy)
		require.NoError(t, err)
		require.Equal(t, wire.CodeBudgetExhausted, code)
		r = pricedReceipt("shared-reject", "voyage")
		code, err = l.Reserve(ctx, r, wire.DailyLimits{}, roomy)
		require.NoError(t, err)
		require.Equal(t, wire.CodeBudgetExhausted, code)
		global, people, err := l.ProviderUsage(ctx, l.Day())
		require.NoError(t, err)
		for _, provider := range []string{"voyage", "typesafe"} {
			require.Equal(t, wire.ProviderUsage{Requests: 1, ReservedNanodollars: 20}, global[provider])
			require.Equal(t, global[provider], people["s1"][provider])
		}
		usage, _, err := l.Usage(ctx, l.Day())
		require.NoError(t, err)
		require.Equal(t, int64(2), usage.Requests)
	})
	t.Run("ProviderCapIsAtomicAcrossOwnersAndPeople", func(t *testing.T) {
		ctx := context.Background()
		a, instance := openLedger(t, open)
		b := instance()
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
				r := pricedReceipt(fmt.Sprint(i), "voyage")
				r.Sub = fmt.Sprint("p", i%3)
				r.ProviderLimitNanodollars = 140
				code, err := l.Reserve(ctx, r, roomy, roomy)
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
		global, people, err := a.ProviderUsage(ctx, a.Day())
		require.NoError(t, err)
		require.Equal(t, wire.ProviderUsage{Requests: 7, ReservedNanodollars: 140}, global["voyage"])
		var total int64
		for _, p := range people {
			total += p["voyage"].ReservedNanodollars
		}
		require.Equal(t, int64(140), total)
	})
	t.Run("ProviderSettlementRefundAndUnknownPolicy", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			outcome ledger.Outcome
			actual  []int64
			want    wire.ProviderUsage
			code    wire.ErrorCode
		}{
			{"known", ledger.Succeeded, []int64{6}, wire.ProviderUsage{Requests: 1, ReservedNanodollars: 6, SettledNanodollars: 6}, wire.CodeResultLost},
			{"zero", ledger.Succeeded, []int64{0}, wire.ProviderUsage{Requests: 1}, wire.CodeResultLost},
			{"missing", ledger.Succeeded, nil, wire.ProviderUsage{Requests: 1, ReservedNanodollars: 20, Unknown: 1}, wire.CodeOutcomeUnknown},
			{"negative", ledger.Succeeded, []int64{-1}, wire.ProviderUsage{Requests: 1, ReservedNanodollars: 20, Unknown: 1}, wire.CodeOutcomeUnknown},
			{"overbound", ledger.Succeeded, []int64{21}, wire.ProviderUsage{Requests: 1, ReservedNanodollars: 20, Unknown: 1, Fenced: true}, wire.CodeOutcomeUnknown},
			{"rejected", ledger.Rejected, nil, wire.ProviderUsage{Requests: 1, ReservedNanodollars: 20, Unknown: 1}, wire.CodeResultLost},
			{"not-dispatched", ledger.NotDispatched, nil, wire.ProviderUsage{}, ""},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ctx := context.Background()
				l, _ := openLedger(t, open)
				r := pricedReceipt("one", "voyage")
				code, err := l.Reserve(ctx, r, roomy, roomy)
				require.NoError(t, err)
				require.Empty(t, code)
				require.NoError(t, l.MarkDispatched(ctx, r.Sub, r.Key))
				require.NoError(t, l.Settle(ctx, r.Sub, r.Key, tc.outcome, 3, tc.actual...))
				require.ErrorIs(t, l.Settle(ctx, r.Sub, r.Key, tc.outcome, 0, 0), ledger.ErrPrecondition)
				global, people, err := l.ProviderUsage(ctx, l.Day())
				require.NoError(t, err)
				require.Equal(t, tc.want, global["voyage"])
				require.Equal(t, tc.want, people[r.Sub]["voyage"])
				code, err = l.Reserve(ctx, r, roomy, roomy)
				require.NoError(t, err)
				require.Equal(t, tc.code, code)
			})
		}
	})
	t.Run("OverboundFencesOtherOwnersButNotOtherProvider", func(t *testing.T) {
		ctx := context.Background()
		a, instance := openLedger(t, open)
		b := instance()
		r := pricedReceipt("bad", "voyage")
		code, err := a.Reserve(ctx, r, roomy, roomy)
		require.NoError(t, err)
		require.Empty(t, code)
		require.NoError(t, a.MarkDispatched(ctx, r.Sub, r.Key))
		require.NoError(t, a.Settle(ctx, r.Sub, r.Key, ledger.Succeeded, 3, 21))
		r.Key = "new"
		r.ProviderLimitNanodollars = 1000000
		code, err = b.Reserve(ctx, r, roomy, roomy)
		require.NoError(t, err)
		require.Equal(t, wire.CodeBudgetExhausted, code)
		r.Provider = "typesafe"
		code, err = b.Reserve(ctx, r, roomy, roomy)
		require.NoError(t, err)
		require.Empty(t, code)
	})
	t.Run("ProviderRecoveryAndUTCAdmissionDay", func(t *testing.T) {
		ctx := context.Background()
		clock := time.Date(2026, 10, 31, 23, 59, 59, 0, time.UTC)
		instance := open(t, func() time.Time { return clock })
		old := instance()
		for _, key := range []string{"reserved", "dispatched", "settle"} {
			r := pricedReceipt(key, "voyage")
			code, err := old.Reserve(ctx, r, roomy, roomy)
			require.NoError(t, err)
			require.Empty(t, code)
			if key != "reserved" {
				require.NoError(t, old.MarkDispatched(ctx, r.Sub, r.Key))
			}
		}
		clock = clock.Add(2 * time.Second)
		require.NoError(t, old.Settle(ctx, "s1", "settle", ledger.Succeeded, 2, 4))
		clock = clock.Add(ledger.LeaseDuration)
		next := instance()
		require.NoError(t, next.Recover(ctx))
		require.NoError(t, next.Recover(ctx))
		global, people, err := next.ProviderUsage(ctx, "2026-10-31")
		require.NoError(t, err)
		require.Equal(t, wire.ProviderUsage{Requests: 3, ReservedNanodollars: 44, SettledNanodollars: 4, Unknown: 2}, global["voyage"])
		require.Equal(t, global["voyage"], people["s1"]["voyage"])
		global, people, err = next.ProviderUsage(ctx, next.Day())
		require.NoError(t, err)
		require.Empty(t, global)
		require.Empty(t, people)
		r := pricedReceipt("new-day", "voyage")
		r.ProviderLimitNanodollars = 20
		code, err := next.Reserve(ctx, r, roomy, roomy)
		require.NoError(t, err)
		require.Empty(t, code)
		require.ErrorIs(t, old.Settle(ctx, "s1", "dispatched", ledger.Succeeded, 0, 0), ledger.ErrPrecondition)
	})
	t.Run("LegacyUsageCannotBeInventedOrRefundProviderUsage", func(t *testing.T) {
		ctx := context.Background()
		clock := t0
		instance := open(t, func() time.Time { return clock })
		l := instance()
		code, err := l.Reserve(ctx, receipt("legacy"), roomy, roomy)
		require.NoError(t, err)
		require.Empty(t, code)
		require.NoError(t, l.MarkDispatched(ctx, "s1", "legacy"))
		require.NoError(t, l.Settle(ctx, "s1", "legacy", ledger.Succeeded, 0, 0))
		r := pricedReceipt("new", "voyage")
		code, err = l.Reserve(ctx, r, roomy, roomy)
		require.NoError(t, err)
		require.Equal(t, wire.CodeBudgetExhausted, code)
		global, people, err := l.ProviderUsage(ctx, l.Day())
		require.NoError(t, err)
		require.Empty(t, global)
		require.Empty(t, people)
		clock = clock.Add(24 * time.Hour)
		l = instance()
		code, err = l.Reserve(ctx, r, roomy, roomy)
		require.NoError(t, err)
		require.Empty(t, code)
	})
}
