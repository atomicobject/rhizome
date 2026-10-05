package identifierreconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

const planVersion = "identifier-reconciliation/v1"

// BuildPlan deterministically selects keepers and allocates replacements from
// the frozen inventory. Provenance is all-or-nothing within each collision.
func BuildPlan(inventory *Inventory, evidence map[string]ProvenanceEvidence) (*Plan, error) {
	if inventory == nil {
		return nil, fmt.Errorf("identifier inventory is required")
	}
	plan := &Plan{Version: planVersion, Collisions: make([]PlannedCollision, 0, len(inventory.collisions))}
	reservationsByPool := make(map[PoolKey]ontology.IdentifierReservationInventory)
	for _, collision := range inventory.collisions {
		keeper, basis, fallback, keeperEvidence := selectKeeper(collision, evidence)
		claimants := append([]Claim(nil), collision.Claimants...)
		sortClaims(claimants)
		losers := make([]PlannedLoser, 0, len(claimants)-1)
		for _, claimant := range claimants {
			if claimant.ID() == keeper.ID() {
				continue
			}
			loser := PlannedLoser{Claim: claimant}
			if claimant.Kind == ClaimPreferred {
				strategy, err := collision.Pool.format().StrategyContract()
				if err != nil {
					return nil, fmt.Errorf("identifier pool %s strategy contract: %w", collision.Pool.String(), err)
				}
				current, ok := strategy.Parse(claimant.Value)
				if !ok {
					current = ontology.IdentifierValue{Strategy: strategy.Strategy()}
				}
				reservations, exists := reservationsByPool[collision.Pool]
				if !exists {
					reservations = inventory.ReservationInventory(collision.Pool)
				}
				replacement, err := strategy.Replacement(current, reservations)
				if err != nil {
					if !ok {
						return nil, fmt.Errorf("identifier pool %s losing preferred claim %s has value %q that does not parse as %s and cannot be replaced: %w", collision.Pool.String(), claimant.ID(), claimant.Value, collision.Pool.Strategy, err)
					}
					return nil, fmt.Errorf("identifier pool %s replacement for losing preferred claim %s: %w", collision.Pool.String(), claimant.ID(), err)
				}
				candidate, err := strategy.Format(replacement)
				if err != nil {
					return nil, fmt.Errorf("identifier pool %s format replacement for losing preferred claim %s: %w", collision.Pool.String(), claimant.ID(), err)
				}
				for _, reservation := range reservations.Reservations {
					if IdentifierComparisonKey(reservation.Value) == IdentifierComparisonKey(candidate) {
						return nil, fmt.Errorf("identifier pool %s strategy returned reserved replacement %q for losing preferred claim %s", collision.Pool.String(), candidate, claimant.ID())
					}
				}
				loser.Replacement = candidate
				reservations.Reservations = append(reservations.Reservations, ontology.IdentifierReservation{
					Value: candidate,
					Owner: "planned:" + claimant.ID(),
				})
				reservationsByPool[collision.Pool] = reservations
			}
			losers = append(losers, loser)
		}
		plan.Collisions = append(plan.Collisions, PlannedCollision{
			Key: stableCollisionKey(collision), Pool: collision.Pool, Value: collision.Value,
			Kind: collision.Kind, Keeper: keeper, KeeperBasis: basis,
			KeeperEvidence: keeperEvidence, FallbackReason: fallback, Losers: losers,
		})
	}
	payload, err := plan.fingerprintJSON()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	plan.Fingerprint = hex.EncodeToString(sum[:])
	plan.sealed = plan.Fingerprint
	return plan, nil
}

func selectKeeper(collision Collision, evidence map[string]ProvenanceEvidence) (Claim, KeeperBasis, string, *ProvenanceEvidence) {
	claimants := append([]Claim(nil), collision.Claimants...)
	sortClaims(claimants)
	missing := make([]string, 0)
	complete := true
	for _, claimant := range claimants {
		item, ok := evidence[claimant.ID()]
		if !ok || !item.Complete || item.AuthorDate.IsZero() || !isFullGitOID(item.FullOID) {
			complete = false
			reason := "provenance unavailable"
			if ok && strings.TrimSpace(item.Reason) != "" {
				reason = strings.TrimSpace(item.Reason)
			}
			missing = append(missing, claimant.Node.String()+": "+reason)
		}
	}
	if !complete {
		return claimants[0], KeeperByCanonicalKey, strings.Join(missing, "; "), nil
	}
	sort.Slice(claimants, func(i, j int) bool {
		left, right := evidence[claimants[i].ID()], evidence[claimants[j].ID()]
		if !left.AuthorDate.Equal(right.AuthorDate) {
			return left.AuthorDate.Before(right.AuthorDate)
		}
		leftOID, rightOID := strings.ToLower(left.FullOID), strings.ToLower(right.FullOID)
		if leftOID != rightOID {
			return leftOID < rightOID
		}
		return claimants[i].Node.String() < claimants[j].Node.String()
	})
	selected := evidence[claimants[0].ID()]
	selected.AuthorDate = selected.AuthorDate.UTC()
	selected.FullOID = strings.ToLower(strings.TrimSpace(selected.FullOID))
	selected.Reason = ""
	return claimants[0], KeeperByGit, "", &selected
}

func isFullGitOID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

func stableCollisionKey(collision Collision) string {
	claimants := make([]string, 0, len(collision.Claimants))
	for _, claimant := range collision.Claimants {
		claimants = append(claimants, claimant.membershipID())
	}
	sort.Strings(claimants)
	encoded, _ := json.Marshal([]any{collision.Pool.String(), collision.Value, claimants})
	sum := sha256.Sum256(encoded)
	return "identifier-collision:v1:" + hex.EncodeToString(sum[:])
}

func (p *Plan) fingerprintJSON() ([]byte, error) {
	return json.Marshal(struct {
		Version    string             `json:"version"`
		Collisions []PlannedCollision `json:"collisions"`
	}{Version: p.Version, Collisions: p.Collisions})
}

// ValidatedSnapshot returns a canonical defensive copy only when exported plan
// members still agree with the fingerprint sealed by BuildPlan. Operation
// adapters must map this snapshot, never caller-mutable Plan fields directly.
func (p *Plan) ValidatedSnapshot() (*Plan, error) {
	if p == nil {
		return nil, fmt.Errorf("identifier reconciliation plan is required")
	}
	expected := p.sealed
	if expected == "" {
		expected = p.Fingerprint
	}
	if expected == "" || p.Fingerprint != expected {
		return nil, fmt.Errorf("identifier reconciliation plan fingerprint was mutated")
	}
	canonical := &Plan{Version: p.Version, Collisions: append([]PlannedCollision(nil), p.Collisions...), Fingerprint: expected, sealed: expected}
	for index := range canonical.Collisions {
		canonical.Collisions[index].Losers = append([]PlannedLoser(nil), canonical.Collisions[index].Losers...)
		if canonical.Collisions[index].KeeperEvidence != nil {
			evidence := *canonical.Collisions[index].KeeperEvidence
			canonical.Collisions[index].KeeperEvidence = &evidence
		}
		sort.Slice(canonical.Collisions[index].Losers, func(i, j int) bool {
			return canonical.Collisions[index].Losers[i].Claim.Node.String() < canonical.Collisions[index].Losers[j].Claim.Node.String()
		})
		claimants := []Claim{canonical.Collisions[index].Keeper}
		for _, loser := range canonical.Collisions[index].Losers {
			claimants = append(claimants, loser.Claim)
		}
		wantKey := stableCollisionKey(Collision{
			Pool: canonical.Collisions[index].Pool, Value: canonical.Collisions[index].Value,
			Kind: canonical.Collisions[index].Kind, Claimants: claimants,
		})
		if canonical.Collisions[index].Key != wantKey {
			return nil, fmt.Errorf("identifier collision key was mutated")
		}
	}
	sort.Slice(canonical.Collisions, func(i, j int) bool {
		if canonical.Collisions[i].Pool.String() != canonical.Collisions[j].Pool.String() {
			return canonical.Collisions[i].Pool.String() < canonical.Collisions[j].Pool.String()
		}
		if canonical.Collisions[i].Value != canonical.Collisions[j].Value {
			return canonical.Collisions[i].Value < canonical.Collisions[j].Value
		}
		return canonical.Collisions[i].Key < canonical.Collisions[j].Key
	})
	payload, err := canonical.fingerprintJSON()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	actual := hex.EncodeToString(sum[:])
	if actual != expected {
		return nil, fmt.Errorf("identifier reconciliation plan members do not match fingerprint")
	}
	return canonical, nil
}

// CanonicalJSON returns the stable externally reviewable validated snapshot.
func (p *Plan) CanonicalJSON() ([]byte, error) {
	canonical, err := p.ValidatedSnapshot()
	if err != nil {
		return nil, err
	}
	return json.Marshal(canonical)
}
