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

const migrationPlanVersion = "identifier-strategy-migration/v1"

// MigrationErrorCode is a stable classification for a fail-closed whole-pool
// migration. Callers may map codes to validation evidence without parsing text.
type MigrationErrorCode string

const (
	MigrationErrorNonallocatablePool     MigrationErrorCode = "nonallocatable_pool"
	MigrationErrorDuplicateNode          MigrationErrorCode = "duplicate_node"
	MigrationErrorDuplicatePreferred     MigrationErrorCode = "duplicate_preferred"
	MigrationErrorMixedStrategy          MigrationErrorCode = "mixed_strategy"
	MigrationErrorInvalidSourceValue     MigrationErrorCode = "invalid_source_value"
	MigrationErrorInvalidProspectivePath MigrationErrorCode = "invalid_prospective_path"
	MigrationErrorTargetReserved         MigrationErrorCode = "target_reserved"
	MigrationErrorNotRequired            MigrationErrorCode = "migration_not_required"
)

// MigrationError carries deterministic, pool-scoped classification evidence.
type MigrationError struct {
	Code    MigrationErrorCode `json:"code"`
	Node    CanonicalNodeKey   `json:"node,omitempty"`
	Value   string             `json:"value,omitempty"`
	Message string             `json:"message"`
}

func (e *MigrationError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("identifier strategy migration %s: %s", e.Code, e.Message)
}

// MigrationMember is one preferred identifier in a complete allocatable pool.
// ProspectivePath is used only when the target strategy is DATETIME.
type MigrationMember struct {
	Node            CanonicalNodeKey `json:"node"`
	PreferredValue  string           `json:"preferredValue"`
	ProspectivePath string           `json:"prospectivePath,omitempty"`
}

// MigrationInput is the complete filesystem-free authority for pure mapping.
// TargetReservations contains retained claims outside Members in the rendered
// target namespace. Only an exact proposed-target collision blocks mapping.
type MigrationInput struct {
	TargetFormat       ontology.IdentifierFormat        `json:"targetFormat"`
	Members            []MigrationMember                `json:"members"`
	TargetReservations []ontology.IdentifierReservation `json:"targetReservations,omitempty"`
}

// MigrationRewrite is one root preferred-identifier transition. Discovery and
// repair adapters expand it to aliases, descendants, locators, and references.
type MigrationRewrite struct {
	Node            CanonicalNodeKey `json:"node"`
	OldIdentifier   string           `json:"oldIdentifier"`
	NewIdentifier   string           `json:"newIdentifier"`
	ProspectivePath string           `json:"prospectivePath,omitempty"`
}

// MigrationPlan is an immutable deterministic whole-pool mapping. It contains
// no Git, filesystem, lifecycle, lease, or transaction state.
type MigrationPlan struct {
	Version     string             `json:"version"`
	Key         string             `json:"key"`
	SourcePool  PoolKey            `json:"sourcePool"`
	TargetPool  PoolKey            `json:"targetPool"`
	Rewrites    []MigrationRewrite `json:"rewrites"`
	Fingerprint string             `json:"fingerprint"`
	sealed      string
}

type normalizedMigrationMember struct {
	MigrationMember
	parsed ontology.IdentifierValue
	stamp  string
}

// BuildMigrationPlan classifies a complete pool against the declared target
// strategy and maps a coherent opposite-strategy pool in deterministic order.
func BuildMigrationPlan(input MigrationInput) (*MigrationPlan, error) {
	targetPool, err := NewPoolKey(&input.TargetFormat)
	if err != nil {
		return nil, migrationError(MigrationErrorNonallocatablePool, CanonicalNodeKey{}, "", err.Error())
	}
	targetContract, err := targetPool.format().StrategyContract()
	if err != nil {
		return nil, migrationError(MigrationErrorNonallocatablePool, CanonicalNodeKey{}, "", err.Error())
	}

	members, err := normalizeMigrationMembers(input.Members)
	if err != nil {
		return nil, err
	}
	sourcePool := oppositeMigrationPool(targetPool, members)
	sourceContract, err := sourcePool.format().StrategyContract()
	if err != nil {
		return nil, migrationError(MigrationErrorNonallocatablePool, CanonicalNodeKey{}, "", err.Error())
	}

	if len(members) == 0 {
		return sealMigrationPlan(sourcePool, targetPool, nil)
	}

	sourceCount, targetCount, invalidCount := 0, 0, 0
	for index := range members {
		if parsed, ok := migrationSourceValue(sourcePool, sourceContract, members[index].PreferredValue); ok {
			members[index].parsed = parsed
			sourceCount++
			continue
		}
		if _, ok := targetContract.Parse(members[index].PreferredValue); ok {
			targetCount++
			continue
		}
		invalidCount++
	}
	if targetCount == len(members) {
		return nil, migrationError(MigrationErrorNotRequired, members[0].Node, members[0].PreferredValue,
			"every preferred value already matches the declared target strategy")
	}
	if (sourceCount > 0 && (targetCount > 0 || invalidCount > 0)) || targetCount > 0 {
		member := firstMemberOutsideStrategy(members, sourcePool, sourceContract)
		return nil, migrationError(MigrationErrorMixedStrategy, member.Node, member.PreferredValue,
			"preferred values do not form one coherent opposite-strategy pool")
	}
	if invalidCount > 0 {
		member := firstMemberOutsideStrategy(members, sourcePool, sourceContract)
		return nil, migrationError(MigrationErrorInvalidSourceValue, member.Node, member.PreferredValue,
			fmt.Sprintf("preferred value does not match %s", sourcePool.Strategy))
	}

	var rewrites []MigrationRewrite
	switch targetPool.Strategy {
	case ontology.IdentifierStrategyDateTime:
		rewrites, err = mapSequentialToDateTime(members, targetContract)
	case ontology.IdentifierStrategySequential:
		rewrites, err = mapDateTimeToSequential(members, targetContract)
	default:
		err = migrationError(MigrationErrorNonallocatablePool, CanonicalNodeKey{}, "", "unsupported target strategy")
	}
	if err != nil {
		return nil, err
	}
	if err := validateMigrationTargetReservations(rewrites, input.TargetReservations); err != nil {
		return nil, err
	}
	return sealMigrationPlan(sourcePool, targetPool, rewrites)
}

func validateMigrationTargetReservations(rewrites []MigrationRewrite, input []ontology.IdentifierReservation) error {
	reservations := append([]ontology.IdentifierReservation(nil), input...)
	sort.Slice(reservations, func(i, j int) bool {
		left, right := IdentifierComparisonKey(reservations[i].Value), IdentifierComparisonKey(reservations[j].Value)
		if left != right {
			return left < right
		}
		return reservations[i].Owner < reservations[j].Owner
	})
	byValue := make(map[string]ontology.IdentifierReservation, len(reservations))
	for _, reservation := range reservations {
		key := IdentifierComparisonKey(reservation.Value)
		if _, exists := byValue[key]; !exists {
			byValue[key] = reservation
		}
	}
	for _, rewrite := range rewrites {
		reservation, exists := byValue[IdentifierComparisonKey(rewrite.NewIdentifier)]
		if !exists {
			continue
		}
		return migrationError(MigrationErrorTargetReserved, rewrite.Node, rewrite.NewIdentifier,
			fmt.Sprintf("proposed target is already reserved by %q", strings.TrimSpace(reservation.Owner)))
	}
	return nil
}

func normalizeMigrationMembers(input []MigrationMember) ([]normalizedMigrationMember, error) {
	members := make([]normalizedMigrationMember, 0, len(input))
	for _, raw := range input {
		node, err := NewCanonicalNodeKey(raw.Node.NotePath, raw.Node.Fragment, raw.Node.TypeName, raw.Node.IdentifierField)
		if err != nil {
			return nil, migrationError(MigrationErrorNonallocatablePool, raw.Node, raw.PreferredValue, err.Error())
		}
		members = append(members, normalizedMigrationMember{MigrationMember: MigrationMember{
			Node: node, PreferredValue: NormalizeIdentifierValue(raw.PreferredValue), ProspectivePath: raw.ProspectivePath,
		}})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Node.String() < members[j].Node.String() })
	seenNodes := make(map[string]struct{}, len(members))
	seenValues := make(map[string]CanonicalNodeKey, len(members))
	for _, member := range members {
		nodeKey := member.Node.String()
		if _, exists := seenNodes[nodeKey]; exists {
			return nil, migrationError(MigrationErrorDuplicateNode, member.Node, member.PreferredValue, "node appears more than once in migration membership")
		}
		seenNodes[nodeKey] = struct{}{}
		valueKey := IdentifierComparisonKey(member.PreferredValue)
		if owner, exists := seenValues[valueKey]; exists {
			return nil, migrationError(MigrationErrorDuplicatePreferred, member.Node, member.PreferredValue,
				fmt.Sprintf("preferred value is also claimed by %s", owner.String()))
		}
		seenValues[valueKey] = member.Node
	}
	return members, nil
}

func oppositeMigrationPool(target PoolKey, members []normalizedMigrationMember) PoolKey {
	source := PoolKey{Prefix: target.Prefix, Separator: target.Separator}
	if target.Strategy == ontology.IdentifierStrategySequential {
		source.Strategy = ontology.IdentifierStrategyDateTime
		return source
	}
	source.Strategy = ontology.IdentifierStrategySequential
	source.Pad = inferredSequentialPad(target, members)
	return source
}

// The prior sequential pad disappears when the schema changes to DATETIME.
// Infer the greatest minimum width that reproduces the frozen authored values;
// this is deterministic and affects only source-plan evidence, not allocation.
func inferredSequentialPad(target PoolKey, members []normalizedMigrationMember) int {
	pad := 0
	head := target.Prefix + target.Separator
	for _, member := range members {
		value := member.PreferredValue
		if len(value) <= len(head) || !strings.EqualFold(value[:len(head)], head) {
			continue
		}
		width := len(value) - len(head)
		if pad == 0 || width < pad {
			pad = width
		}
	}
	if pad < 1 {
		return 4
	}
	return pad
}

func migrationSourceValue(pool PoolKey, contract ontology.IdentifierStrategyContract, value string) (ontology.IdentifierValue, bool) {
	parsed, ok := contract.Parse(value)
	if !ok {
		return ontology.IdentifierValue{}, false
	}
	if pool.Strategy != ontology.IdentifierStrategySequential {
		return parsed, true
	}
	canonical, err := contract.Format(parsed)
	if err != nil || !strings.EqualFold(canonical, value) {
		return ontology.IdentifierValue{}, false
	}
	return parsed, true
}

func firstMemberOutsideStrategy(members []normalizedMigrationMember, pool PoolKey, contract ontology.IdentifierStrategyContract) normalizedMigrationMember {
	for _, member := range members {
		if _, ok := migrationSourceValue(pool, contract, member.PreferredValue); !ok {
			return member
		}
	}
	return members[0]
}

func mapSequentialToDateTime(members []normalizedMigrationMember, target ontology.IdentifierStrategyContract) ([]MigrationRewrite, error) {
	for index := range members {
		if members[index].ProspectivePath != members[index].Node.NotePath {
			return nil, migrationError(MigrationErrorInvalidProspectivePath, members[index].Node, members[index].ProspectivePath,
				"prospective path must equal the member's canonical note path")
		}
		probe, err := target.Allocate(ontology.IdentifierAllocationRequest{Paths: []string{members[index].ProspectivePath}}, ontology.IdentifierReservationInventory{})
		if err != nil || len(probe.Values) != 1 {
			message := "prospective path cannot supply a canonical filename minute"
			if err != nil {
				message = err.Error()
			}
			return nil, migrationError(MigrationErrorInvalidProspectivePath, members[index].Node, members[index].ProspectivePath, message)
		}
		members[index].stamp = probe.Values[0].Value.DateTimeStamp
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].stamp != members[j].stamp {
			return members[i].stamp < members[j].stamp
		}
		if members[i].parsed.Number != members[j].parsed.Number {
			return members[i].parsed.Number < members[j].parsed.Number
		}
		return members[i].Node.String() < members[j].Node.String()
	})
	paths := make([]string, len(members))
	for index := range members {
		paths[index] = members[index].ProspectivePath
	}
	allocated, err := target.Allocate(ontology.IdentifierAllocationRequest{Paths: paths}, ontology.IdentifierReservationInventory{})
	if err != nil || len(allocated.Values) != len(members) {
		if err == nil {
			err = fmt.Errorf("strategy returned %d values for %d members", len(allocated.Values), len(members))
		}
		return nil, migrationError(MigrationErrorInvalidProspectivePath, CanonicalNodeKey{}, "", err.Error())
	}
	rewrites := make([]MigrationRewrite, 0, len(members))
	for index, member := range members {
		newIdentifier, err := target.Format(allocated.Values[index].Value)
		if err != nil {
			return nil, migrationError(MigrationErrorNonallocatablePool, member.Node, member.PreferredValue, err.Error())
		}
		rewrites = append(rewrites, MigrationRewrite{
			Node: member.Node, OldIdentifier: member.PreferredValue, NewIdentifier: newIdentifier, ProspectivePath: member.ProspectivePath,
		})
	}
	return rewrites, nil
}

func mapDateTimeToSequential(members []normalizedMigrationMember, target ontology.IdentifierStrategyContract) ([]MigrationRewrite, error) {
	sort.Slice(members, func(i, j int) bool {
		if members[i].parsed.DateTimeStamp != members[j].parsed.DateTimeStamp {
			return members[i].parsed.DateTimeStamp < members[j].parsed.DateTimeStamp
		}
		if members[i].parsed.Ordinal != members[j].parsed.Ordinal {
			return members[i].parsed.Ordinal < members[j].parsed.Ordinal
		}
		return members[i].Node.String() < members[j].Node.String()
	})
	rewrites := make([]MigrationRewrite, 0, len(members))
	for index, member := range members {
		newIdentifier, err := target.Format(ontology.IdentifierValue{Strategy: ontology.IdentifierStrategySequential, Number: index + 1})
		if err != nil {
			return nil, migrationError(MigrationErrorNonallocatablePool, member.Node, member.PreferredValue, err.Error())
		}
		rewrites = append(rewrites, MigrationRewrite{
			Node: member.Node, OldIdentifier: member.PreferredValue, NewIdentifier: newIdentifier, ProspectivePath: member.ProspectivePath,
		})
	}
	return rewrites, nil
}

func sealMigrationPlan(source, target PoolKey, rewrites []MigrationRewrite) (*MigrationPlan, error) {
	canonical := append([]MigrationRewrite(nil), rewrites...)
	keyPayload, err := json.Marshal(struct {
		Source   PoolKey            `json:"source"`
		Target   PoolKey            `json:"target"`
		Rewrites []MigrationRewrite `json:"rewrites"`
	}{Source: source, Target: target, Rewrites: canonical})
	if err != nil {
		return nil, err
	}
	keySum := sha256.Sum256(keyPayload)
	plan := &MigrationPlan{
		Version:    migrationPlanVersion,
		Key:        "identifier-migration:v1:" + hex.EncodeToString(keySum[:]),
		SourcePool: source, TargetPool: target, Rewrites: canonical,
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

func (p *MigrationPlan) fingerprintJSON() ([]byte, error) {
	return json.Marshal(struct {
		Version    string             `json:"version"`
		Key        string             `json:"key"`
		SourcePool PoolKey            `json:"sourcePool"`
		TargetPool PoolKey            `json:"targetPool"`
		Rewrites   []MigrationRewrite `json:"rewrites"`
	}{Version: p.Version, Key: p.Key, SourcePool: p.SourcePool, TargetPool: p.TargetPool, Rewrites: p.Rewrites})
}

// ValidatedSnapshot returns a defensive copy only when every exported member
// still agrees with the fingerprint sealed by BuildMigrationPlan.
func (p *MigrationPlan) ValidatedSnapshot() (*MigrationPlan, error) {
	if p == nil {
		return nil, fmt.Errorf("identifier migration plan is required")
	}
	expected := p.sealed
	if expected == "" {
		expected = p.Fingerprint
	}
	if expected == "" || p.Fingerprint != expected {
		return nil, fmt.Errorf("identifier migration plan fingerprint was mutated")
	}
	canonical := &MigrationPlan{
		Version: p.Version, Key: p.Key, SourcePool: p.SourcePool, TargetPool: p.TargetPool,
		Rewrites: append([]MigrationRewrite(nil), p.Rewrites...), Fingerprint: expected, sealed: expected,
	}
	payload, err := canonical.fingerprintJSON()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != expected {
		return nil, fmt.Errorf("identifier migration plan members do not match fingerprint")
	}
	return canonical, nil
}

func migrationError(code MigrationErrorCode, node CanonicalNodeKey, value, message string) error {
	return &MigrationError{Code: code, Node: node, Value: value, Message: message}
}
