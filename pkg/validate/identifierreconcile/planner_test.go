package identifierreconcile

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildPlanUsesCompleteGitEvidenceInAuthorDateOIDKeyOrder(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
	b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")
	c := claim(t, pool, "SPEC-0001", ClaimPreferred, "c.md")
	inventory, err := BuildInventory([]Claim{c, b, a})
	require.NoError(t, err)
	when := time.Date(2026, 7, 15, 12, 0, 0, 0, time.FixedZone("offset", -4*60*60))
	evidence := map[string]ProvenanceEvidence{
		a.ID(): usableEvidence(when, strings.Repeat("b", 40)),
		b.ID(): usableEvidence(when, strings.Repeat("a", 40)),
		c.ID(): usableEvidence(when.Add(-time.Hour), strings.Repeat("f", 40)),
	}

	plan, err := BuildPlan(inventory, evidence)
	require.NoError(t, err)
	require.Len(t, plan.Collisions, 1)
	got := plan.Collisions[0]
	require.Equal(t, KeeperByGit, got.KeeperBasis)
	require.Equal(t, "c.md", got.Keeper.Node.NotePath)
	require.NotNil(t, got.KeeperEvidence)
	require.Equal(t, when.Add(-time.Hour).UTC(), got.KeeperEvidence.AuthorDate)
	require.Equal(t, strings.Repeat("f", 40), got.KeeperEvidence.FullOID)
	require.Empty(t, got.FallbackReason)
	require.Equal(t, []string{"a.md", "b.md"}, []string{
		got.Losers[0].Claim.Node.NotePath,
		got.Losers[1].Claim.Node.NotePath,
	})
}

func TestBuildPlanFallsBackWholeCollisionWhenAnyEvidenceIsIncomplete(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
	z := claim(t, pool, "SPEC-0001", ClaimPreferred, "z.md")
	inventory, err := BuildInventory([]Claim{z, a})
	require.NoError(t, err)
	evidence := map[string]ProvenanceEvidence{
		a.ID(): usableEvidence(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC), strings.Repeat("f", 40)),
		z.ID(): {Complete: false, Reason: "shallow history"},
	}

	plan, err := BuildPlan(inventory, evidence)
	require.NoError(t, err)
	got := plan.Collisions[0]
	require.Equal(t, KeeperByCanonicalKey, got.KeeperBasis)
	require.Equal(t, "a.md", got.Keeper.Node.NotePath)
	require.Contains(t, got.FallbackReason, "shallow history")
}

func TestBuildPlanFallsBackForAbbreviatedOID(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	a := claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md")
	b := claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md")
	inventory, err := BuildInventory([]Claim{b, a})
	require.NoError(t, err)
	when := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	plan, err := BuildPlan(inventory, map[string]ProvenanceEvidence{
		a.ID(): usableEvidence(when, "abc1234"),
		b.ID(): usableEvidence(when.Add(time.Hour), strings.Repeat("f", 40)),
	})
	require.NoError(t, err)
	require.Equal(t, KeeperByCanonicalKey, plan.Collisions[0].KeeperBasis)
	require.Nil(t, plan.Collisions[0].KeeperEvidence)
}

func TestBuildPlanFreezesInventoryAndAllocatesMaxPlusOneInStableOrder(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	claims := []Claim{
		claim(t, pool, "SPEC-0003", ClaimPreferred, "f.md"),
		claim(t, pool, "SPEC-0002", ClaimPreferred, "d.md"),
		claim(t, pool, "SPEC-0003", ClaimPreferred, "e.md"),
		claim(t, pool, "SPEC-0002", ClaimPreferred, "c.md"),
		claim(t, pool, "SPEC-0004", ClaimAlias, "reserved.md"),
		claim(t, pool, "legacy", ClaimAlias, "legacy.md"),
		claim(t, pool, "SPEC-0001", ClaimPreferred, "one.md"),
	}
	inventory, err := BuildInventory(claims)
	require.NoError(t, err)

	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"spec-0002", "spec-0003"}, []string{
		plan.Collisions[0].Value,
		plan.Collisions[1].Value,
	})
	require.Equal(t, "SPEC-0005", plan.Collisions[0].Losers[0].Replacement)
	require.Equal(t, "SPEC-0006", plan.Collisions[1].Losers[0].Replacement)
}

func TestValidatedSnapshotCanonicalizesOrderAndRejectsMemberMutation(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md"),
		claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md"),
		claim(t, pool, "SPEC-0002", ClaimPreferred, "c.md"),
		claim(t, pool, "SPEC-0002", ClaimPreferred, "d.md"),
	})
	require.NoError(t, err)
	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	want, err := plan.CanonicalJSON()
	require.NoError(t, err)
	plan.Collisions[0], plan.Collisions[1] = plan.Collisions[1], plan.Collisions[0]
	got, err := plan.CanonicalJSON()
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))

	plan.Collisions[0].Key = "mutated"
	_, err = plan.ValidatedSnapshot()
	require.ErrorContains(t, err, "key was mutated")

	plan.Collisions[0].Key = plan.Collisions[1].Key
	plan.Fingerprint = "stale"
	_, err = plan.ValidatedSnapshot()
	require.ErrorContains(t, err, "fingerprint was mutated")
}

func TestBuildPlanAllocatesOnlyForLosingPreferredClaims(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	preferred := claim(t, pool, "SPEC-0001", ClaimPreferred, "z.md")
	alias := claim(t, pool, "SPEC-0001", ClaimAlias, "a.md")
	inventory, err := BuildInventory([]Claim{preferred, alias})
	require.NoError(t, err)

	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Equal(t, ClaimAlias, plan.Collisions[0].Keeper.Kind)
	require.Equal(t, "SPEC-0002", plan.Collisions[0].Losers[0].Replacement)

	inventory, err = BuildInventory([]Claim{
		claim(t, pool, "SPEC-0002", ClaimAlias, "a.md"),
		claim(t, pool, "SPEC-0002", ClaimAlias, "b.md"),
	})
	require.NoError(t, err)
	plan, err = BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Empty(t, plan.Collisions[0].Losers[0].Replacement)
}

func TestBuildPlanCanonicalJSONAndFingerprintAreStableAcrossInputOrder(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	dateTimePool := mustDateTimePool(t, "EFF")
	datasets := map[string][]Claim{
		"sequential": {
			claim(t, pool, "SPEC-0002", ClaimPreferred, "d.md"),
			claim(t, pool, "SPEC-0001", ClaimPreferred, "b.md"),
			claim(t, pool, "SPEC-0002", ClaimPreferred, "c.md"),
			claim(t, pool, "SPEC-0001", ClaimPreferred, "a.md"),
		},
		"datetime": {
			claim(t, dateTimePool, "EFF-2026-08-05-14-32", ClaimPreferred, "b.md"),
			claim(t, dateTimePool, "EFF-2026-08-05-14-32", ClaimPreferred, "a.md"),
			claim(t, dateTimePool, "EFF-2026-08-05-14-32-2", ClaimAlias, "reserved.md"),
		},
	}
	for name, base := range datasets {
		t.Run(name, func(t *testing.T) {
			var wantJSON []byte
			var wantFingerprint string
			var wantCollisionKeys []string
			for seed := int64(0); seed < 20; seed++ {
				shuffled := append([]Claim(nil), base...)
				rand.New(rand.NewSource(seed)).Shuffle(len(shuffled), func(i, j int) {
					shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
				})
				inventory, err := BuildInventory(shuffled)
				require.NoError(t, err)
				plan, err := BuildPlan(inventory, map[string]ProvenanceEvidence{})
				require.NoError(t, err)
				gotJSON, err := plan.CanonicalJSON()
				require.NoError(t, err)
				gotCollisionKeys := make([]string, 0, len(plan.Collisions))
				for _, collision := range plan.Collisions {
					gotCollisionKeys = append(gotCollisionKeys, collision.Key)
				}
				if seed == 0 {
					wantJSON, wantFingerprint, wantCollisionKeys = gotJSON, plan.Fingerprint, gotCollisionKeys
					continue
				}
				require.Equal(t, string(wantJSON), string(gotJSON))
				require.Equal(t, wantFingerprint, plan.Fingerprint)
				require.Equal(t, wantCollisionKeys, gotCollisionKeys)
			}
			require.Len(t, wantFingerprint, 64)
			require.NotEmpty(t, wantCollisionKeys)
			for _, key := range wantCollisionKeys {
				require.Regexp(t, `^identifier-collision:v1:[0-9a-f]{64}$`, key)
			}
		})
	}
}

func TestCollisionKeyUsesSemanticMembershipWhileFingerprintPreservesAuthoredCase(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	build := func(aValue, bValue string) *Plan {
		inventory, err := BuildInventory([]Claim{
			claim(t, pool, aValue, ClaimPreferred, "a.md"),
			claim(t, pool, bValue, ClaimPreferred, "b.md"),
		})
		require.NoError(t, err)
		plan, err := BuildPlan(inventory, nil)
		require.NoError(t, err)
		return plan
	}
	before := build("SPEC-0001", "spec-0001")
	after := build("spec-0001", "SPEC-0001")
	require.Equal(t, before.Collisions[0].Key, after.Collisions[0].Key)
	require.NotEqual(t, before.Fingerprint, after.Fingerprint)
}

func TestBuildPlanAllocatesDateTimeReplacementWithinFamilyAndFillsLowestHole(t *testing.T) {
	t.Parallel()

	pool := mustDateTimePool(t, "EFF")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "EFF-2026-08-05-14-32", ClaimPreferred, "a.md"),
		claim(t, pool, "EFF-2026-08-05-14-32", ClaimPreferred, "b.md"),
		claim(t, pool, "EFF-2026-08-05-14-32-2", ClaimAlias, "reserved-2.md"),
		claim(t, pool, "EFF-2026-08-05-14-32-4", ClaimAlias, "reserved-4.md"),
	})
	require.NoError(t, err)

	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Equal(t, "EFF-2026-08-05-14-32-3", plan.Collisions[0].Losers[0].Replacement)
}

func TestBuildPlanDoesNotNestDateTimeSuffixes(t *testing.T) {
	t.Parallel()

	pool := mustDateTimePool(t, "EFF")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "EFF-2026-08-05-14-32-7", ClaimPreferred, "a.md"),
		claim(t, pool, "EFF-2026-08-05-14-32-7", ClaimPreferred, "b.md"),
	})
	require.NoError(t, err)

	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Equal(t, "EFF-2026-08-05-14-32-2", plan.Collisions[0].Losers[0].Replacement)
}

func TestBuildPlanReservesEarlierDateTimeReplacementsWithinOneFamily(t *testing.T) {
	t.Parallel()

	pool := mustDateTimePool(t, "EFF")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "EFF-2026-08-05-14-32", ClaimPreferred, "a.md"),
		claim(t, pool, "EFF-2026-08-05-14-32", ClaimPreferred, "b.md"),
		claim(t, pool, "EFF-2026-08-05-14-32-2", ClaimPreferred, "c.md"),
		claim(t, pool, "EFF-2026-08-05-14-32-2", ClaimPreferred, "d.md"),
	})
	require.NoError(t, err)

	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Len(t, plan.Collisions, 2)
	require.Equal(t, "EFF-2026-08-05-14-32-3", plan.Collisions[0].Losers[0].Replacement)
	require.Equal(t, "EFF-2026-08-05-14-32-4", plan.Collisions[1].Losers[0].Replacement)
}

func TestBuildPlanKeepsDateTimeFamiliesIndependent(t *testing.T) {
	t.Parallel()

	pool := mustDateTimePool(t, "EFF")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "EFF-2026-08-05-14-32", ClaimPreferred, "a.md"),
		claim(t, pool, "EFF-2026-08-05-14-32", ClaimPreferred, "b.md"),
		claim(t, pool, "EFF-2026-08-05-14-33", ClaimPreferred, "c.md"),
		claim(t, pool, "EFF-2026-08-05-14-33", ClaimPreferred, "d.md"),
	})
	require.NoError(t, err)

	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Equal(t, "EFF-2026-08-05-14-32-2", plan.Collisions[0].Losers[0].Replacement)
	require.Equal(t, "EFF-2026-08-05-14-33-2", plan.Collisions[1].Losers[0].Replacement)
}

func TestBuildPlanReturnsDeterministicScopedErrorForUnparseableLosingPreferredClaim(t *testing.T) {
	t.Parallel()

	pool := mustDateTimePool(t, "EFF")
	a := claim(t, pool, "legacy", ClaimPreferred, "a.md")
	b := claim(t, pool, "legacy", ClaimPreferred, "b.md")
	buildError := func(input []Claim) string {
		inventory, err := BuildInventory(input)
		require.NoError(t, err)
		_, err = BuildPlan(inventory, nil)
		require.Error(t, err)
		return err.Error()
	}

	forward := buildError([]Claim{a, b})
	require.Equal(t, forward, buildError([]Claim{b, a}))
	require.Contains(t, forward, pool.String())
	require.Contains(t, forward, b.ID())
	require.Contains(t, forward, `value "legacy"`)
	require.Contains(t, forward, "DATETIME")
}

func TestBuildPlanRepairsSequentialCollisionWithOffPatternPreferredValue(t *testing.T) {
	t.Parallel()

	pool := mustPool(t, "SPEC")
	inventory, err := BuildInventory([]Claim{
		claim(t, pool, "legacy", ClaimPreferred, "a.md"),
		claim(t, pool, "legacy", ClaimPreferred, "b.md"),
	})
	require.NoError(t, err)

	plan, err := BuildPlan(inventory, nil)
	require.NoError(t, err)
	require.Equal(t, "SPEC-0001", plan.Collisions[0].Losers[0].Replacement)
}

func usableEvidence(authorDate time.Time, oid string) ProvenanceEvidence {
	return ProvenanceEvidence{AuthorDate: authorDate, FullOID: oid, Complete: true}
}
