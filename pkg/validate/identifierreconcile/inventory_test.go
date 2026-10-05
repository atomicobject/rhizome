package identifierreconcile

import (
	"math"
	"math/rand"
	"strconv"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestBuildInventoryDetectsEveryCollisionKindWithinPool(t *testing.T) {
	t.Parallel()

	specPool := mustPool(t, "SPEC")
	effortPool := mustPool(t, "EFF")
	claims := []Claim{
		claim(t, specPool, "SPEC-0001", ClaimPreferred, "z.md"),
		claim(t, specPool, "SPEC-0001", ClaimPreferred, "a.md"),
		claim(t, specPool, "SPEC-0002", ClaimPreferred, "b.md"),
		claim(t, specPool, "SPEC-0002", ClaimAlias, "c.md"),
		claim(t, specPool, "SPEC-0003", ClaimAlias, "e.md"),
		claim(t, specPool, "SPEC-0003", ClaimAlias, "d.md"),
		// The required alias mirror on the same node is not another claimant.
		claim(t, specPool, "SPEC-0001", ClaimAlias, "a.md"),
		// Identical values in distinct pools are independent.
		claim(t, effortPool, "SPEC-0001", ClaimPreferred, "effort.md"),
	}

	inventory, err := BuildInventory(claims)
	require.NoError(t, err)
	collisions := inventory.Collisions()
	require.Len(t, collisions, 3)
	require.Equal(t, []CollisionKind{
		CollisionPreferredPreferred,
		CollisionPreferredAlias,
		CollisionAliasAlias,
	}, []CollisionKind{collisions[0].Kind, collisions[1].Kind, collisions[2].Kind})
	require.Equal(t, []string{"a.md", "z.md"}, []string{
		collisions[0].Claimants[0].Node.NotePath,
		collisions[0].Claimants[1].Node.NotePath,
	})
	require.Equal(t, []ontology.IdentifierReservation{
		reservation(t, "SPEC-0001", "a.md"), reservation(t, "SPEC-0001", "z.md"),
		reservation(t, "SPEC-0002", "b.md"), reservation(t, "SPEC-0002", "c.md"),
		reservation(t, "SPEC-0003", "d.md"), reservation(t, "SPEC-0003", "e.md"),
	}, inventory.ReservationInventory(specPool).Reservations, "the same-node alias mirror reserves once")
	require.Equal(t, []ontology.IdentifierReservation{reservation(t, "SPEC-0001", "effort.md")}, inventory.ReservationInventory(effortPool).Reservations)
}

func TestInventoryCollisionClaimsExcludeUnrelatedAndSameNodeAliasClaims(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md"),
		claim(t, pool, "SPEC-0001", ClaimAlias, "a.md"),
		claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md"),
		claim(t, pool, "SPEC-0002", ClaimPreferred, "unrelated.md"),
	})
	require.NoError(t, err)

	claims := inventory.CollisionClaims()
	require.Len(t, claims, 2)
	require.Equal(t, []string{"a.md", "b.md"}, []string{claims[0].Node.NotePath, claims[1].Node.NotePath})
	require.Equal(t, ClaimPreferred, claims[0].Kind)

	claims[0].Value = "mutated"
	require.Equal(t, "SPEC-0001", inventory.CollisionClaims()[0].Value)
}

func TestBuildInventoryRecanonicalizesExportedClaimFields(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		{Node: CanonicalNodeKey{NotePath: ` ./docs\a.md `, Fragment: " #^x ", TypeName: " Spec ", IdentifierField: " id "}, Pool: pool, Value: " SPEC-0001 ", Kind: ClaimPreferred},
		claim(t, pool, "SPEC-0001", ClaimPreferred, "docs/b.md"),
	})
	require.NoError(t, err)
	require.Equal(t, "docs/a.md", inventory.Collisions()[0].Claimants[0].Node.NotePath)
	require.Equal(t, "^x", inventory.Collisions()[0].Claimants[0].Node.Fragment)
	require.Equal(t, "Spec", inventory.Collisions()[0].Claimants[0].Node.TypeName)
	require.Equal(t, "id", inventory.Collisions()[0].Claimants[0].Node.IdentifierField)
}

func TestBuildInventoryNormalizesIdentifierBackedLocatorCaret(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md"),
		claim(t, pool, "^SPEC-0001", ClaimPreferred, "b.md"),
	})
	require.NoError(t, err)
	require.Len(t, inventory.Collisions(), 1)
	require.Equal(t, "spec-0001", inventory.Collisions()[0].Value)
	require.Equal(t, []ontology.IdentifierReservation{
		reservation(t, "SPEC-0001", "a.md"), reservation(t, "SPEC-0001", "b.md"),
	}, inventory.ReservationInventory(pool).Reservations, "the locator caret is not part of the reserved identifier")
}

func TestBuildInventoryCaseFoldsSemanticIdentityButPreservesAuthoredValues(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "SPEC-0005", ClaimPreferred, "a.md"),
		claim(t, pool, "spec-0005", ClaimPreferred, "b.md"),
		claim(t, pool, "SpEc-0006", ClaimAlias, "c.md"),
		claim(t, pool, "SPEC-0006", ClaimPreferred, "d.md"),
		claim(t, pool, "spec-0009", ClaimAlias, "reserved.md"),
	})
	require.NoError(t, err)
	require.Len(t, inventory.Collisions(), 2)
	require.Equal(t, "spec-0005", inventory.Collisions()[0].Value)
	require.Equal(t, []string{"SPEC-0005", "spec-0005"}, []string{
		inventory.Collisions()[0].Claimants[0].Value,
		inventory.Collisions()[0].Claimants[1].Value,
	})
	require.Equal(t, CollisionPreferredAlias, inventory.Collisions()[1].Kind)
	require.Equal(t, []ontology.IdentifierReservation{
		reservation(t, "SPEC-0005", "a.md"), reservation(t, "spec-0005", "b.md"),
		reservation(t, "SPEC-0006", "d.md"), reservation(t, "SpEc-0006", "c.md"),
		reservation(t, "spec-0009", "reserved.md"),
	}, inventory.ReservationInventory(pool).Reservations, "reservations keep authored case")

	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Equal(t, "SPEC-0010", plan.Collisions[0].Losers[0].Replacement)
	require.Equal(t, "SPEC-0011", plan.Collisions[1].Losers[0].Replacement)
}

func TestBuildPlanIsStableForSameNodeAuthoredCaseVariants(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	upper := claim(t, pool, "SPEC-0005", ClaimPreferred, "a.md")
	lower := claim(t, pool, "spec-0005", ClaimPreferred, "a.md")
	other := claim(t, pool, "SPEC-0005", ClaimPreferred, "b.md")

	planJSON := func(input []Claim) []byte {
		inventory, err := BuildInventory(input)
		require.NoError(t, err)
		plan, err := BuildPlan(inventory, nil)
		require.NoError(t, err)
		encoded, err := plan.CanonicalJSON()
		require.NoError(t, err)
		return encoded
	}

	require.Equal(t, planJSON([]Claim{upper, lower, other}), planJSON([]Claim{lower, upper, other}))
}

func TestBuildInventoryClassifiesMixedPreferredAndAliasGroup(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md"),
		claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md"),
		claim(t, pool, "SPEC-0001", ClaimAlias, "c.md"),
	})
	require.NoError(t, err)
	require.Equal(t, CollisionPreferredAlias, inventory.Collisions()[0].Kind)
}

func TestBuildPlanRejectsExhaustedSequentialPool(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	maxValue := "SPEC-" + strconv.Itoa(math.MaxInt)
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, maxValue, ClaimPreferred, "a.md"),
		claim(t, pool, maxValue, ClaimPreferred, "b.md"),
	})
	require.NoError(t, err)
	_, err = BuildPlan(inventory, nil)
	require.ErrorContains(t, err, "exhausted")
}

func TestNewPoolKeyAcceptsDateTimeAndRejectsStrategySpecificPad(t *testing.T) {
	t.Parallel()

	pool, err := NewPoolKey(&ontology.IdentifierFormat{
		Strategy:  ontology.IdentifierStrategyDateTime,
		Prefix:    "EFF",
		Separator: "-",
	})
	require.NoError(t, err)
	require.Equal(t, ontology.IdentifierStrategyDateTime, pool.Strategy)
	require.Zero(t, pool.Pad)

	_, err = NewPoolKey(&ontology.IdentifierFormat{
		Strategy:  ontology.IdentifierStrategyDateTime,
		Prefix:    "EFF",
		Separator: "-",
		Pad:       4,
	})
	require.ErrorContains(t, err, "zero pad")
}

func TestInventoryFreezesCanonicalReservationSnapshotsAcrossInputOrder(t *testing.T) {
	t.Parallel()

	pool := mustDateTimePool(t, "EFF")
	base := []Claim{
		claim(t, pool, "EFF-2026-08-05-14-32", ClaimAlias, "b.md"),
		claim(t, pool, "eff-2026-08-05-14-32", ClaimPreferred, "b.md"),
		claim(t, pool, "EFF-2026-08-05-14-32-2", ClaimAlias, "a.md"),
		claim(t, pool, "legacy", ClaimAlias, "legacy.md"),
	}

	var want ontology.IdentifierReservationInventory
	for seed := int64(0); seed < 20; seed++ {
		shuffled := append([]Claim(nil), base...)
		rand.New(rand.NewSource(seed)).Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		inventory, err := BuildInventory(shuffled)
		require.NoError(t, err)
		got := inventory.ReservationInventory(pool)
		if seed == 0 {
			want = got
		} else {
			require.Equal(t, want, got)
		}
	}
	values := make([]string, 0, len(want.Reservations))
	for _, reservation := range want.Reservations {
		values = append(values, reservation.Value)
	}
	require.Equal(t, []string{"EFF-2026-08-05-14-32", "EFF-2026-08-05-14-32-2", "legacy"}, values,
		"preferred/alias mirrors on one node reserve once; nonnumeric history stays reserved")

	inventory, err := BuildInventory(base)
	require.NoError(t, err)
	returned := inventory.ReservationInventory(pool)
	require.Equal(t, want, returned)
	returned.Reservations[0].Value = "mutated"
	returned.Reservations[1].Owner = "mutated"
	require.Equal(t, want, inventory.ReservationInventory(pool), "a returned snapshot must not alias the frozen reservations")
}

func mustPool(t *testing.T, prefix string) PoolKey {
	t.Helper()
	pool, err := NewPoolKey(&ontology.IdentifierFormat{
		Strategy:  ontology.IdentifierStrategySequential,
		Prefix:    prefix,
		Separator: "-",
		Pad:       4,
	})
	require.NoError(t, err)
	return pool
}

func mustDateTimePool(t *testing.T, prefix string) PoolKey {
	t.Helper()
	pool, err := NewPoolKey(&ontology.IdentifierFormat{
		Strategy:  ontology.IdentifierStrategyDateTime,
		Prefix:    prefix,
		Separator: "-",
	})
	require.NoError(t, err)
	return pool
}

func reservation(t *testing.T, value, notePath string) ontology.IdentifierReservation {
	t.Helper()
	key, err := NewCanonicalNodeKey(notePath, "", "Spec", "id")
	require.NoError(t, err)
	return ontology.IdentifierReservation{Value: value, Owner: key.String()}
}

func claim(t *testing.T, pool PoolKey, value string, kind ClaimKind, notePath string) Claim {
	t.Helper()
	key, err := NewCanonicalNodeKey(notePath, "", "Spec", "id")
	require.NoError(t, err)
	return Claim{Node: key, Pool: pool, Value: value, Kind: kind}
}
