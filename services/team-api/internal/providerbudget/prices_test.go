package providerbudget

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJevVersionReservations(t *testing.T) {
	for _, model := range []string{"jev-latest", "jev-1.13.0", "jev-1.14.0", "jev-2.0.0", "jev-999.999.999"} {
		t.Run(model, func(t *testing.T) {
			price, reserved, ok := Reserve(TypeSafe, model, 1)
			require.True(t, ok)
			require.Equal(t, model, price.Model)
			require.Equal(t, int64(42), price.NanodollarsPerToken)
			require.Equal(t, int64(65_536), price.MaxBillableTokens)
			require.Equal(t, int64(2_752_512), reserved)
			settled, ok := Cost(price, 7)
			require.True(t, ok)
			require.Equal(t, int64(294), settled)
		})
	}
	for _, model := range []string{"", "jev", "jev-9", "jev-1.13", "jev-1.13.0.1", "jev-01.13.0", "jev-1.-13.0", "jev-1.13.0-preview", "jev-preview", "JEV-1.13.0", "gpt-1.13.0", "jev-1.13.0 ", "jev-" + strings.Repeat("1", 60) + ".0.0"} {
		t.Run(model, func(t *testing.T) {
			_, _, ok := Reserve(TypeSafe, model, 1)
			require.False(t, ok)
		})
	}
	_, _, ok := Reserve(TypeSafe, "jev-1.13.0", 0)
	require.False(t, ok)
	_, _, ok = Reserve(Voyage, "jev-1.13.0", 1)
	require.False(t, ok, "other providers retain their own model policy")
}

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
