package identifierreconcile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

type poolValueKey struct {
	Pool  PoolKey
	Value string
}

type poolReservationKey struct {
	Pool  PoolKey
	Value string
	Owner string
}

// Inventory is an immutable snapshot of all preferred and alias claims used by
// one planning run.
type Inventory struct {
	claims       []Claim
	reservations map[PoolKey][]ontology.IdentifierReservation
	collisions   []Collision
}

// BuildInventory freezes claims, strategy reservations, and collision groups.
func BuildInventory(input []Claim) (*Inventory, error) {
	claims := make([]Claim, 0, len(input))
	seenExact := make(map[string]struct{}, len(input))
	for _, raw := range input {
		claim, err := normalizeClaim(raw)
		if err != nil {
			return nil, err
		}
		if _, exists := seenExact[claim.ID()]; exists {
			continue
		}
		seenExact[claim.ID()] = struct{}{}
		claims = append(claims, claim)
	}
	sortClaims(claims)
	reservations := buildReservationInventories(claims)

	groups := make(map[poolValueKey][]Claim)
	for _, claim := range claims {
		key := poolValueKey{Pool: claim.Pool, Value: IdentifierComparisonKey(claim.Value)}
		groups[key] = append(groups[key], claim)
	}
	collisions := make([]Collision, 0)
	for key, group := range groups {
		claimants := distinctNodeClaimants(group)
		if len(claimants) < 2 {
			continue
		}
		collisions = append(collisions, Collision{
			Pool: key.Pool, Value: key.Value, Kind: collisionKind(claimants), Claimants: claimants,
		})
	}
	sort.Slice(collisions, func(i, j int) bool {
		if collisions[i].Pool.String() != collisions[j].Pool.String() {
			return collisions[i].Pool.String() < collisions[j].Pool.String()
		}
		return collisions[i].Value < collisions[j].Value
	})
	return &Inventory{claims: claims, reservations: reservations, collisions: collisions}, nil
}

func buildReservationInventories(claims []Claim) map[PoolKey][]ontology.IdentifierReservation {
	reservations := make(map[PoolKey][]ontology.IdentifierReservation)
	seen := make(map[poolReservationKey]struct{}, len(claims))
	for _, claim := range claims {
		owner := claim.Node.String()
		key := poolReservationKey{Pool: claim.Pool, Value: IdentifierComparisonKey(claim.Value), Owner: owner}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		reservations[claim.Pool] = append(reservations[claim.Pool], ontology.IdentifierReservation{
			Value: claim.Value,
			Owner: owner,
		})
	}
	for pool := range reservations {
		sort.Slice(reservations[pool], func(left, right int) bool {
			leftValue := IdentifierComparisonKey(reservations[pool][left].Value)
			rightValue := IdentifierComparisonKey(reservations[pool][right].Value)
			if leftValue != rightValue {
				return leftValue < rightValue
			}
			if reservations[pool][left].Value != reservations[pool][right].Value {
				return reservations[pool][left].Value < reservations[pool][right].Value
			}
			return reservations[pool][left].Owner < reservations[pool][right].Owner
		})
	}
	return reservations
}

// NormalizeIdentifierValue is the shared semantic normalization contract for
// inventory and history resolvers.
func NormalizeIdentifierValue(value string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "^"))
}

// IdentifierComparisonKey matches noderead's case-folded semantic identity
// while Claim.Value retains the authored spelling needed for exact edits.
func IdentifierComparisonKey(value string) string {
	return ontology.NormalizeIdentifierSemanticValue(value)
}

func normalizeClaim(raw Claim) (Claim, error) {
	node, err := NewCanonicalNodeKey(raw.Node.NotePath, raw.Node.Fragment, raw.Node.TypeName, raw.Node.IdentifierField)
	if err != nil {
		return Claim{}, err
	}
	pool, err := NewPoolKey(raw.Pool.format())
	if err != nil {
		return Claim{}, err
	}
	claim := raw
	claim.Node = node
	claim.Pool = pool
	claim.Value = NormalizeIdentifierValue(claim.Value)
	if claim.Value == "" {
		return Claim{}, fmt.Errorf("identifier claim value is required")
	}
	if claim.Kind != ClaimPreferred && claim.Kind != ClaimAlias {
		return Claim{}, fmt.Errorf("unsupported claim kind %q", claim.Kind)
	}
	return claim, nil
}

// Collisions returns a defensive copy in canonical pool/value order.
func (i *Inventory) Collisions() []Collision {
	if i == nil {
		return nil
	}
	out := make([]Collision, len(i.collisions))
	for idx, collision := range i.collisions {
		out[idx] = collision
		out[idx].Claimants = append([]Claim(nil), collision.Claimants...)
	}
	return out
}

// CollisionClaims returns only canonical claimants whose shared identifier is
// ambiguous. Git keeper provenance is irrelevant to non-colliding claims.
func (i *Inventory) CollisionClaims() []Claim {
	if i == nil {
		return nil
	}
	var out []Claim
	for _, collision := range i.collisions {
		out = append(out, collision.Claimants...)
	}
	return out
}

// ReservationInventory returns a defensive copy of the canonical frozen
// preferred and alias reservations for one strategy pool.
func (i *Inventory) ReservationInventory(pool PoolKey) ontology.IdentifierReservationInventory {
	if i == nil {
		return ontology.IdentifierReservationInventory{}
	}
	return ontology.IdentifierReservationInventory{
		Reservations: append([]ontology.IdentifierReservation(nil), i.reservations[pool]...),
	}
}

func distinctNodeClaimants(group []Claim) []Claim {
	byNode := make(map[string]Claim, len(group))
	for _, claim := range group {
		key := claim.Node.String()
		existing, ok := byNode[key]
		if !ok || (existing.Kind == ClaimAlias && claim.Kind == ClaimPreferred) {
			byNode[key] = claim
		}
	}
	out := make([]Claim, 0, len(byNode))
	for _, claim := range byNode {
		out = append(out, claim)
	}
	sortClaims(out)
	return out
}

func collisionKind(claims []Claim) CollisionKind {
	preferred := 0
	aliases := 0
	for _, claim := range claims {
		if claim.Kind == ClaimPreferred {
			preferred++
		} else {
			aliases++
		}
	}
	switch {
	case preferred > 0 && aliases > 0:
		return CollisionPreferredAlias
	case preferred > 0:
		return CollisionPreferredPreferred
	default:
		return CollisionAliasAlias
	}
}

func sortClaims(claims []Claim) {
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].Node.String() != claims[j].Node.String() {
			return claims[i].Node.String() < claims[j].Node.String()
		}
		if claims[i].Kind != claims[j].Kind {
			return claims[i].Kind < claims[j].Kind
		}
		if claims[i].Pool.String() != claims[j].Pool.String() {
			return claims[i].Pool.String() < claims[j].Pool.String()
		}
		if IdentifierComparisonKey(claims[i].Value) != IdentifierComparisonKey(claims[j].Value) {
			return IdentifierComparisonKey(claims[i].Value) < IdentifierComparisonKey(claims[j].Value)
		}
		return claims[i].Value < claims[j].Value
	})
}
