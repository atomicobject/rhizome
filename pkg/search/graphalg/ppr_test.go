package graphalg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPersonalizedPageRank_ConcentratesNearSeed(t *testing.T) {
	// A -> B -> C chain, plus isolated D. Seed on A should rank B above C, and D ~ 0.
	adj := map[string]map[string]float64{
		"A": {"B": 1},
		"B": {"C": 1},
		"C": {},
		"D": {},
	}
	seeds := map[string]float64{"A": 1}

	r := PersonalizedPageRank(adj, seeds, PPROptions{Damping: 0.85, Iterations: 20})

	require.NotEmpty(t, r)
	require.Greater(t, r["B"], r["C"])
	require.Greater(t, r["A"], r["D"])
	require.InDelta(t, 0.0, r["D"], 1e-6)
}

func TestPersonalizedPageRank_DanglingRedistributesToSeeds(t *testing.T) {
	// Seed on A, but A is dangling. With dangling redistribution to teleport p,
	// A should retain substantial mass.
	adj := map[string]map[string]float64{
		"A": {},
		"B": {"A": 1},
	}
	seeds := map[string]float64{"A": 1}

	r := PersonalizedPageRank(adj, seeds, PPROptions{Damping: 0.85, Iterations: 30})
	require.NotEmpty(t, r)
	require.Greater(t, r["A"], 0.5)
	require.Less(t, r["B"], 0.5)
}

func TestPersonalizedPageRank_NormalizesSeedWeights(t *testing.T) {
	// A and C are structurally symmetric, so only the seed weights separate them.
	adj := map[string]map[string]float64{
		"A": {"B": 1},
		"B": {},
		"C": {"B": 1},
	}
	opts := PPROptions{Damping: 0.85, Iterations: 10}

	r := PersonalizedPageRank(adj, map[string]float64{"A": 9, "C": 1}, opts)
	scaled := PersonalizedPageRank(adj, map[string]float64{"A": 90, "C": 10}, opts)

	var sum float64
	for _, v := range r {
		sum += v
	}
	require.InDelta(t, 1.0, sum, 1e-9)
	for _, node := range []string{"A", "B", "C"} {
		require.InDelta(t, r[node], scaled[node], 1e-12, node)
	}
	require.Greater(t, r["A"], r["C"])
}

func TestPersonalizedPageRank_SumsToApproximatelyOne(t *testing.T) {
	adj := map[string]map[string]float64{
		"A": {"B": 1},
		"B": {"A": 1},
		"C": {},
	}
	seeds := map[string]float64{"A": 1, "B": 1}

	r := PersonalizedPageRank(adj, seeds, PPROptions{Damping: 0.85, Iterations: 20})
	require.NotEmpty(t, r)

	var sum float64
	for _, v := range r {
		sum += v
	}
	require.InDelta(t, 1.0, sum, 1e-6)
}

func TestPersonalizedPageRank_EmptyOrMissingSeedsReturnEmpty(t *testing.T) {
	adj := map[string]map[string]float64{
		"A": {"B": 1},
		"B": {},
	}

	require.Empty(t, PersonalizedPageRank(adj, map[string]float64{}, PPROptions{}))
	require.Empty(t, PersonalizedPageRank(adj, map[string]float64{"Z": 1}, PPROptions{}))
}
