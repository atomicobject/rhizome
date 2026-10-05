package identifierreconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/atomicobject/rhizome/pkg/ontology/reference"
)

// IdentifierLinkDiscovery is an opaque, source-bound inventory of structured
// link plans for one complete vault snapshot and one union of collision
// rewrites. DiscoverIdentifierLinks is the only production sealer; callers
// cannot inject partial source, scan, resolution, or review inventories.
type IdentifierLinkDiscovery struct {
	sourceFingerprint   string
	sourcePreconditions []SourcePrecondition
	schemaHash          string
	rewriteFingerprint  string
	plans               []reference.StructuredLinkRewritePlan
	reviewPlans         []reference.IdentifierReviewCandidatePlan
	provenanceDecisions []AmbiguousLinkProvenanceDecision
	sealed              string
}

type identifierLinkDiscoverySnapshot struct {
	SourceFingerprint   string                                    `json:"sourceFingerprint"`
	SourcePreconditions []SourcePrecondition                      `json:"sourcePreconditions"`
	SchemaHash          string                                    `json:"schemaHash"`
	RewriteFingerprint  string                                    `json:"rewriteFingerprint"`
	Plans               []reference.StructuredLinkRewritePlan     `json:"plans"`
	ReviewPlans         []reference.IdentifierReviewCandidatePlan `json:"reviewPlans"`
	ProvenanceDecisions []AmbiguousLinkProvenanceDecision         `json:"provenanceDecisions,omitempty"`
}

// validatedSnapshotFor returns a detached canonical snapshot only when this
// discovery still binds the complete source inventory, rewrite union, and
// sealed per-note plans with which it was created.
func (d *IdentifierLinkDiscovery) validatedSnapshotFor(rewrites []reference.IdentifierRewrite) (*identifierLinkDiscoverySnapshot, error) {
	if d == nil || d.sealed == "" {
		return nil, fmt.Errorf("complete identifier link discovery is required")
	}
	canonical, err := canonicalRepairRewriteSet(rewrites)
	if err != nil {
		return nil, err
	}
	wantedRewriteFingerprint, err := repairRewriteSetFingerprint(canonical)
	if err != nil {
		return nil, err
	}
	if d.rewriteFingerprint != wantedRewriteFingerprint {
		return nil, fmt.Errorf("identifier link discovery does not match rewrite union")
	}

	snapshot := &identifierLinkDiscoverySnapshot{
		SourceFingerprint:   d.sourceFingerprint,
		SourcePreconditions: append([]SourcePrecondition(nil), d.sourcePreconditions...),
		SchemaHash:          d.schemaHash,
		RewriteFingerprint:  d.rewriteFingerprint,
		Plans:               make([]reference.StructuredLinkRewritePlan, 0, len(d.plans)),
		ReviewPlans:         make([]reference.IdentifierReviewCandidatePlan, 0, len(d.reviewPlans)),
		ProvenanceDecisions: make([]AmbiguousLinkProvenanceDecision, 0, len(d.provenanceDecisions)),
	}
	if d.plans == nil {
		snapshot.Plans = nil
	}
	if d.reviewPlans == nil {
		snapshot.ReviewPlans = nil
	}
	if d.provenanceDecisions == nil {
		snapshot.ProvenanceDecisions = nil
	}
	if snapshot.SchemaHash == "" {
		return nil, fmt.Errorf("identifier link discovery requires schema hash")
	}
	if err := validateIdentifierLinkSources(snapshot.SourcePreconditions); err != nil {
		return nil, err
	}
	if sourcePreconditionFingerprint(snapshot.SourcePreconditions) != snapshot.SourceFingerprint {
		return nil, fmt.Errorf("identifier link discovery source fingerprint does not match complete source inventory")
	}

	sources := make(map[string]string, len(snapshot.SourcePreconditions))
	for _, source := range snapshot.SourcePreconditions {
		sources[source.NotePath] = source.SourceHash
	}
	previousPath := ""
	for _, raw := range d.plans {
		plan, err := raw.ValidatedSnapshot()
		if err != nil {
			return nil, err
		}
		notePath, err := canonicalRepairPath(plan.NotePath)
		if err != nil || notePath != plan.NotePath {
			return nil, fmt.Errorf("structured link plan requires canonical vault-relative note path")
		}
		if plan.NotePath == previousPath {
			return nil, fmt.Errorf("identifier link discovery contains duplicate plan for %s", plan.NotePath)
		}
		if plan.NotePath < previousPath {
			return nil, fmt.Errorf("identifier link discovery plans must be canonically sorted")
		}
		previousPath = plan.NotePath
		sourceHash, found := sources[plan.NotePath]
		if !found {
			return nil, fmt.Errorf("structured link plan path %s is absent from complete source inventory", plan.NotePath)
		}
		if plan.SourceFingerprint != sourceHash {
			return nil, fmt.Errorf("structured link plan source for %s does not match complete source inventory", plan.NotePath)
		}
		for _, edit := range plan.Edits {
			if _, ok := linkEditRewrite(edit, canonical); !ok {
				return nil, fmt.Errorf("structured link plan edit does not match rewrite union")
			}
		}
		for _, diagnostic := range plan.Diagnostics {
			if !linkDiagnosticMatchesRewrites(diagnostic, canonical) {
				return nil, fmt.Errorf("structured link plan diagnostic does not match rewrite union")
			}
		}
		snapshot.Plans = append(snapshot.Plans, *plan)
	}
	if err := appendValidatedReviewPlans(snapshot, d.reviewPlans, canonical, sources); err != nil {
		return nil, err
	}
	previousDecision := ""
	for index := range d.provenanceDecisions {
		decision, err := d.provenanceDecisions[index].ValidatedSnapshot()
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s\x00%012d", decision.NotePath, decision.LinkIndex)
		if key <= previousDecision {
			return nil, fmt.Errorf("identifier link provenance decisions must be unique and canonically sorted")
		}
		previousDecision = key
		if sourceHash, found := sources[decision.NotePath]; !found || sourceHash != decision.SourceHash {
			return nil, fmt.Errorf("identifier link provenance decision source does not match complete inventory")
		}
		snapshot.ProvenanceDecisions = append(snapshot.ProvenanceDecisions, *decision)
	}

	actual, err := identifierLinkDiscoveryFingerprint(*snapshot)
	if err != nil {
		return nil, err
	}
	if actual != d.sealed {
		return nil, fmt.Errorf("identifier link discovery changed after sealing")
	}
	return snapshot, nil
}

func appendValidatedReviewPlans(snapshot *identifierLinkDiscoverySnapshot, rawPlans []reference.IdentifierReviewCandidatePlan, rewrites []reference.IdentifierRewrite, sources map[string]string) error {
	previousPath := ""
	for _, raw := range rawPlans {
		plan, err := raw.ValidatedSnapshot()
		if err != nil {
			return err
		}
		notePath, err := canonicalRepairPath(plan.NotePath)
		if err != nil || notePath != plan.NotePath {
			return fmt.Errorf("identifier review plan requires canonical vault-relative note path")
		}
		if plan.NotePath == previousPath {
			return fmt.Errorf("identifier link discovery contains duplicate review plan for %s", plan.NotePath)
		}
		if plan.NotePath < previousPath {
			return fmt.Errorf("identifier review plans must be canonically sorted")
		}
		previousPath = plan.NotePath
		sourceHash, found := sources[plan.NotePath]
		if !found || sourceHash != plan.SourceFingerprint {
			return fmt.Errorf("identifier review plan source for %s does not match complete source inventory", plan.NotePath)
		}
		if len(plan.BlockingDiagnostics) > 0 || !sameCanonicalRewriteUnion(plan.Rewrites, rewrites) {
			return fmt.Errorf("identifier review plan does not match rewrite union")
		}
		if len(plan.Diagnostics) != len(plan.Candidates) {
			return fmt.Errorf("identifier review plan candidate diagnostics are incomplete")
		}
		for index, candidate := range plan.Candidates {
			if candidate.NotePath != plan.NotePath || !sameCanonicalRewriteSubset(candidate.Rewrites, rewrites) {
				return fmt.Errorf("identifier review candidate does not match source or rewrite union")
			}
			diagnostic := plan.Diagnostics[index]
			if diagnostic.Range != candidate.Range || diagnostic.Value != candidate.Value {
				return fmt.Errorf("identifier review candidate diagnostic does not match source evidence")
			}
			if diagnostic.Kind != reference.IdentifierRewriteDiagnosticReviewOnly || !fieldDiagnosticMatchesRewrites(diagnostic, rewrites) {
				return fmt.Errorf("identifier review diagnostic does not match rewrite union")
			}
		}
		snapshot.ReviewPlans = append(snapshot.ReviewPlans, *plan)
	}
	return nil
}

func sameCanonicalRewriteUnion(left, right []reference.IdentifierRewrite) bool {
	canonicalLeft, err := canonicalRepairRewriteSet(left)
	if err != nil || len(canonicalLeft) != len(right) {
		return false
	}
	for index := range canonicalLeft {
		if repairRewriteKey(canonicalLeft[index]) != repairRewriteKey(right[index]) || jsonKey(canonicalLeft[index]) != jsonKey(right[index]) {
			return false
		}
	}
	return true
}

func sameCanonicalRewriteSubset(subset, union []reference.IdentifierRewrite) bool {
	canonicalSubset, err := canonicalRepairRewriteSet(subset)
	if err != nil || len(canonicalSubset) == 0 {
		return false
	}
	available := make(map[string]struct{}, len(union))
	for _, rewrite := range union {
		available[jsonKey(rewrite)] = struct{}{}
	}
	for _, rewrite := range canonicalSubset {
		if _, found := available[jsonKey(rewrite)]; !found {
			return false
		}
	}
	return true
}

func validateIdentifierLinkSources(sources []SourcePrecondition) error {
	seen := make(map[string]string, len(sources))
	previousPath := ""
	for _, source := range sources {
		notePath, err := canonicalRepairPath(source.NotePath)
		if err != nil || notePath != source.NotePath {
			return fmt.Errorf("identifier link source path must be canonical vault-relative")
		}
		if !validSHA256Fingerprint(source.SourceHash) {
			return fmt.Errorf("identifier link source for %s requires a lowercase SHA-256 fingerprint", source.NotePath)
		}
		if prior, found := seen[source.NotePath]; found {
			if prior != source.SourceHash {
				return fmt.Errorf("identifier link source inventory has conflicting hashes for %s", source.NotePath)
			}
			return fmt.Errorf("identifier link source inventory duplicated %s", source.NotePath)
		}
		if source.NotePath < previousPath {
			return fmt.Errorf("identifier link source inventory must be canonically sorted")
		}
		seen[source.NotePath] = source.SourceHash
		previousPath = source.NotePath
	}
	return nil
}

func validSHA256Fingerprint(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return false
	}
	return hex.EncodeToString(decoded) == value
}

// sourcePreconditionFingerprint is the stable aggregate identity shared by
// complete field and link discovery over the same canonical vault snapshot.
func sourcePreconditionFingerprint(sources []SourcePrecondition) string {
	canonical := append([]SourcePrecondition(nil), sources...)
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].NotePath != canonical[j].NotePath {
			return canonical[i].NotePath < canonical[j].NotePath
		}
		return canonical[i].SourceHash < canonical[j].SourceHash
	})
	encoded, _ := json.Marshal(canonical)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func identifierLinkDiscoveryFingerprint(snapshot identifierLinkDiscoverySnapshot) (string, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
