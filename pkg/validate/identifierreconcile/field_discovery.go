package identifierreconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
)

// IdentifierFieldDiscovery is an opaque, source-bound inventory of every
// structured field obligation for one union of collision rewrites. EFF-0048
// owns the production constructor over EFF-0046's complete projection.
type IdentifierFieldDiscovery struct {
	source              *identifierRepairSource
	rewrites            []reference.IdentifierRewrite
	synthesizedDerived  []synthesizedDerivedField
	sourcePreconditions []SourcePrecondition
	schemaHash          string
	rewriteFingerprint  string
	occurrences         []reference.StructuredFieldOccurrence
	edits               []reference.StructuredFieldEdit
	diagnostics         []reference.IdentifierRewriteDiagnostic
	sealed              string
}

type identifierFieldDiscoverySnapshot struct {
	Rewrites            []reference.IdentifierRewrite           `json:"rewrites"`
	SynthesizedDerived  []synthesizedDerivedField               `json:"synthesizedDerived,omitempty"`
	SourcePreconditions []SourcePrecondition                    `json:"sourcePreconditions"`
	SchemaHash          string                                  `json:"schemaHash"`
	RewriteFingerprint  string                                  `json:"rewriteFingerprint"`
	Occurrences         []reference.StructuredFieldOccurrence   `json:"occurrences"`
	Edits               []reference.StructuredFieldEdit         `json:"edits"`
	Diagnostics         []reference.IdentifierRewriteDiagnostic `json:"diagnostics,omitempty"`
}

func (d *IdentifierFieldDiscovery) validatedSnapshotFor(rewrites []reference.IdentifierRewrite) (*identifierFieldDiscoverySnapshot, error) {
	if d == nil || d.sealed == "" {
		return nil, fmt.Errorf("complete identifier field discovery is required")
	}
	canonical, err := canonicalRepairRewriteSet(rewrites)
	if err != nil {
		return nil, err
	}
	wanted, err := repairRewriteSetFingerprint(canonical)
	if err != nil {
		return nil, err
	}
	snapshot := &identifierFieldDiscoverySnapshot{
		Rewrites:            cloneIdentifierRewrites(d.rewrites),
		SynthesizedDerived:  append([]synthesizedDerivedField(nil), d.synthesizedDerived...),
		SourcePreconditions: append([]SourcePrecondition(nil), d.sourcePreconditions...), SchemaHash: d.schemaHash, RewriteFingerprint: d.rewriteFingerprint,
		Occurrences: cloneStructuredFieldOccurrences(d.occurrences),
		Edits:       append([]reference.StructuredFieldEdit(nil), d.edits...), Diagnostics: append([]reference.IdentifierRewriteDiagnostic(nil), d.diagnostics...),
	}
	if !sameCanonicalRewriteUnion(snapshot.Rewrites, canonical) {
		return nil, fmt.Errorf("identifier field discovery does not match complete rewrite union")
	}
	for index := range snapshot.Diagnostics {
		snapshot.Diagnostics[index].Candidates = append([]ontology.NodeRef(nil), d.diagnostics[index].Candidates...)
	}
	if strings.TrimSpace(snapshot.SchemaHash) == "" || snapshot.RewriteFingerprint != wanted {
		return nil, fmt.Errorf("identifier field discovery does not match source, schema, or rewrite union")
	}
	if err := validateSourcePreconditions(snapshot.SourcePreconditions); err != nil {
		return nil, fmt.Errorf("identifier field discovery: %w", err)
	}
	actual, err := identifierFieldDiscoveryFingerprint(*snapshot)
	if err != nil {
		return nil, err
	}
	if actual != d.sealed {
		return nil, fmt.Errorf("identifier field discovery changed after sealing")
	}
	return snapshot, nil
}

func cloneStructuredFieldOccurrences(input []reference.StructuredFieldOccurrence) []reference.StructuredFieldOccurrence {
	out := append([]reference.StructuredFieldOccurrence(nil), input...)
	for index := range out {
		out[index].Candidates = append([]ontology.NodeRef(nil), input[index].Candidates...)
	}
	return out
}

func canonicalRepairRewriteSet(rewrites []reference.IdentifierRewrite) ([]reference.IdentifierRewrite, error) {
	out := cloneIdentifierRewrites(rewrites)
	for index := range out {
		if out[index].Mode == "" {
			out[index].Mode = reference.IdentifierRewritePreferredRekey
		}
		if err := canonicalizeRepairRewrite(&out[index]); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return repairRewriteKey(out[i]) < repairRewriteKey(out[j]) })
	return out, nil
}

func repairRewriteSetFingerprint(rewrites []reference.IdentifierRewrite) (string, error) {
	encoded, err := json.Marshal(rewrites)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func identifierFieldDiscoveryFingerprint(snapshot identifierFieldDiscoverySnapshot) (string, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func cloneIdentifierRewrites(input []reference.IdentifierRewrite) []reference.IdentifierRewrite {
	out := append([]reference.IdentifierRewrite(nil), input...)
	for index := range out {
		out[index].AdditionalAliasesFields = append([]string(nil), input[index].AdditionalAliasesFields...)
	}
	return out
}
