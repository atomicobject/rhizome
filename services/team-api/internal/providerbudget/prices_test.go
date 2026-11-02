package providerbudget

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewedRequestReservations(t *testing.T) {
	voyage, n, ok := Reserve(Voyage, "voyage-4-lite", 1000)
	require.True(t, ok)
	require.Equal(t, int64(21_300_000), n)
	require.Equal(t, int64(1_065_000), voyage.MaxBillableTokens)
	require.Equal(t, ReviewedAt, voyage.ReviewedAt)
	require.Equal(t, PriceVersion, voyage.Version)
	jev, n, ok := Reserve(TypeSafe, "jev-latest", 2000)
	require.True(t, ok)
	require.Equal(t, "jev-latest", jev.Model)
	require.Equal(t, int64(2_752_512), n, "questions share the total request context")
	_, _, ok = Reserve(TypeSafe, "jev-preview", 1)
	require.False(t, ok)
	_, _, ok = Reserve(Voyage, "voyage-4-lite", 1001)
	require.False(t, ok)
}

func TestBillingCostUsesExactIntegerArithmetic(t *testing.T) {
	prices := ReviewedPrices()
	n, ok := Cost(prices[TypeSafe], 7)
	require.True(t, ok)
	require.Equal(t, int64(294), n)
	n, ok = Cost(prices[Voyage], 0)
	require.True(t, ok)
	require.Zero(t, n)
	_, ok = Cost(prices[TypeSafe], math.MaxInt64)
	require.False(t, ok)
	_, ok = Cost(prices[Voyage], -1)
	require.False(t, ok)
	_, ok = Cost(Price{}, 7)
	require.False(t, ok)
}
