package identifierreconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func (a *RepairAssembly) seal() error {
	payload, err := a.fingerprintJSON()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(payload)
	a.Fingerprint = hex.EncodeToString(sum[:])
	a.sealed = a.Fingerprint
	return nil
}

func (a *RepairAssembly) fingerprintJSON() ([]byte, error) {
	return json.Marshal(struct {
		PlanFingerprint     string                  `json:"planFingerprint"`
		SchemaHash          string                  `json:"schemaHash"`
		SourcePreconditions []SourcePrecondition    `json:"sourcePreconditions"`
		Components          []CollisionRepairIntent `json:"components"`
	}{PlanFingerprint: a.PlanFingerprint, SchemaHash: a.SchemaHash, SourcePreconditions: a.SourcePreconditions, Components: a.Components})
}

// ValidatedSnapshot returns a defensive assembly copy only when all flattened
// intents retain their sealed collision membership and content fingerprint.
// EFF-0047 adapters must map this snapshot rather than caller-mutable fields.
func (a *RepairAssembly) ValidatedSnapshot() (*RepairAssembly, error) {
	if a == nil {
		return nil, fmt.Errorf("identifier repair assembly is required")
	}
	expected := a.sealed
	if expected == "" {
		return nil, fmt.Errorf("identifier repair assembly is not sealed")
	}
	if a.Fingerprint != expected {
		return nil, fmt.Errorf("identifier repair assembly fingerprint was mutated")
	}
	encoded, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	var snapshot RepairAssembly
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return nil, err
	}
	if err := validateSourcePreconditions(snapshot.SourcePreconditions); err != nil {
		return nil, err
	}
	if snapshot.SchemaHash == "" {
		return nil, fmt.Errorf("identifier repair assembly schema hash is required")
	}
	seen := make(map[string]struct{}, len(snapshot.Components))
	for _, component := range snapshot.Components {
		if len(component.MembershipKeys) != 1 || component.MembershipKeys[0] == "" {
			return nil, fmt.Errorf("identifier repair component must retain one collision seed membership")
		}
		if _, duplicate := seen[component.MembershipKeys[0]]; duplicate {
			return nil, fmt.Errorf("identifier repair component membership is duplicated")
		}
		seen[component.MembershipKeys[0]] = struct{}{}
	}
	for _, component := range snapshot.Components {
		if err := validateComponentMembership(component, seen); err != nil {
			return nil, err
		}
	}
	if err := validateRepairSourceCoverage(&snapshot); err != nil {
		return nil, err
	}
	payload, err := snapshot.fingerprintJSON()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	if actual := hex.EncodeToString(sum[:]); actual != expected {
		return nil, fmt.Errorf("identifier repair assembly members do not match fingerprint")
	}
	snapshot.Fingerprint = expected
	snapshot.sealed = expected
	return &snapshot, nil
}

// CanonicalJSON returns the validated mapping snapshot for review or storage.
func (a *RepairAssembly) CanonicalJSON() ([]byte, error) {
	snapshot, err := a.ValidatedSnapshot()
	if err != nil {
		return nil, err
	}
	return json.Marshal(snapshot)
}

func validateComponentMembership(component CollisionRepairIntent, known map[string]struct{}) error {
	key := component.MembershipKeys[0]
	for _, intent := range component.Rewrites {
		if !validIntentMembership(intent.MembershipKeys, key, known) {
			return fmt.Errorf("identifier rewrite membership diverges from collision component")
		}
	}
	for _, intent := range append(append([]FieldRepairIntent(nil), component.FieldEdits...), component.AliasEdits...) {
		if !validIntentMembership(intent.MembershipKeys, key, known) {
			return fmt.Errorf("field edit membership diverges from collision component")
		}
	}
	for _, intent := range component.LinkEdits {
		if !validIntentMembership(intent.MembershipKeys, key, known) {
			return fmt.Errorf("link edit membership diverges from collision component")
		}
	}
	for _, intent := range component.Moves {
		if !validIntentMembership(intent.MembershipKeys, key, known) {
			return fmt.Errorf("move membership diverges from collision component")
		}
	}
	for _, diagnostic := range component.Diagnostics {
		if !validIntentMembership(diagnostic.MembershipKeys, key, known) {
			return fmt.Errorf("diagnostic membership diverges from collision component")
		}
	}
	for _, intent := range component.Postchecks {
		if !validIntentMembership(intent.MembershipKeys, key, known) {
			return fmt.Errorf("postcheck membership diverges from collision component")
		}
	}
	return nil
}

func validIntentMembership(keys []string, required string, known map[string]struct{}) bool {
	if len(keys) == 0 {
		return false
	}
	found := false
	previous := ""
	for _, key := range keys {
		if key == "" || key <= previous {
			return false
		}
		if _, ok := known[key]; !ok {
			return false
		}
		if key == required {
			found = true
		}
		previous = key
	}
	return found
}
