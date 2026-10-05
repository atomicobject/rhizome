package identifierreconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// AmbiguousLinkProvenanceRequest identifies one current structured bare-ID
// link and every canonical claimant returned by bounded resolution.
type AmbiguousLinkProvenanceRequest struct {
	NotePath     string                               `json:"notePath"`
	SourceHash   string                               `json:"sourceHash"`
	Content      string                               `json:"-"`
	LinkSnapshot *obsidian.StructuredLinkScanSnapshot `json:"-"`
	LinkIndex    int                                  `json:"linkIndex"`
	RawTarget    string                               `json:"rawTarget"`
	Candidates   []Claim                              `json:"candidates"`
}

// LinkCandidateAncestry is complete bounded evidence for one candidate at the
// source link's introduction commit.
type LinkCandidateAncestry struct {
	Ref             ontology.NodeRef `json:"ref"`
	ClaimID         string           `json:"claimId"`
	IntroductionOID string           `json:"introductionOid"`
	Ancestor        bool             `json:"ancestor"`
	Complete        bool             `json:"complete"`
}

// AmbiguousLinkGitEvidence discloses the source introduction and complete
// candidate ancestry matrix used to derive a unique winner.
type AmbiguousLinkGitEvidence struct {
	SourceIntroductionOID string                  `json:"sourceIntroductionOid"`
	Candidates            []LinkCandidateAncestry `json:"candidates"`
}

// AmbiguousLinkProvenanceDecision is opaque proof, not caller authority. Use
// ValidatedSnapshot before consuming Winner.
type AmbiguousLinkProvenanceDecision struct {
	NotePath      string                   `json:"notePath"`
	SourceHash    string                   `json:"sourceHash"`
	LinkIndex     int                      `json:"linkIndex"`
	RawTarget     string                   `json:"rawTarget"`
	CandidateRefs []ontology.NodeRef       `json:"candidateRefs"`
	Winner        ontology.NodeRef         `json:"winner"`
	Evidence      AmbiguousLinkGitEvidence `json:"evidence"`
	Fingerprint   string                   `json:"fingerprint"`
	sealed        string
}

type ambiguousLinkDecisionPayload struct {
	NotePath      string                   `json:"notePath"`
	SourceHash    string                   `json:"sourceHash"`
	LinkIndex     int                      `json:"linkIndex"`
	RawTarget     string                   `json:"rawTarget"`
	CandidateRefs []ontology.NodeRef       `json:"candidateRefs"`
	Winner        ontology.NodeRef         `json:"winner"`
	Evidence      AmbiguousLinkGitEvidence `json:"evidence"`
}

// ValidatedSnapshot rejects forged or caller-mutated decisions and returns a
// detached canonical proof whose winner is uniquely derived from ancestry.
func (d *AmbiguousLinkProvenanceDecision) ValidatedSnapshot() (*AmbiguousLinkProvenanceDecision, error) {
	if d == nil || d.sealed == "" {
		return nil, fmt.Errorf("ambiguous link provenance decision is not sealed")
	}
	payload := d.payload()
	if err := validateAmbiguousLinkDecision(payload); err != nil {
		return nil, err
	}
	fingerprint := ambiguousLinkDecisionFingerprint(payload)
	if d.Fingerprint != d.sealed || fingerprint != d.sealed {
		return nil, fmt.Errorf("ambiguous link provenance decision changed after sealing")
	}
	encoded, _ := json.Marshal(d)
	var snapshot AmbiguousLinkProvenanceDecision
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return nil, err
	}
	snapshot.sealed = d.sealed
	return &snapshot, nil
}

func sealAmbiguousLinkDecision(payload ambiguousLinkDecisionPayload) *AmbiguousLinkProvenanceDecision {
	fingerprint := ambiguousLinkDecisionFingerprint(payload)
	return &AmbiguousLinkProvenanceDecision{
		NotePath: payload.NotePath, SourceHash: payload.SourceHash, LinkIndex: payload.LinkIndex, RawTarget: payload.RawTarget,
		CandidateRefs: payload.CandidateRefs, Winner: payload.Winner, Evidence: payload.Evidence,
		Fingerprint: fingerprint, sealed: fingerprint,
	}
}

func (d *AmbiguousLinkProvenanceDecision) payload() ambiguousLinkDecisionPayload {
	return ambiguousLinkDecisionPayload{d.NotePath, d.SourceHash, d.LinkIndex, d.RawTarget, d.CandidateRefs, d.Winner, d.Evidence}
}

func ambiguousLinkDecisionFingerprint(payload ambiguousLinkDecisionPayload) string {
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func validateAmbiguousLinkDecision(payload ambiguousLinkDecisionPayload) error {
	if _, err := canonicalRepairPath(payload.NotePath); err != nil || !validSHA256Fingerprint(payload.SourceHash) || payload.LinkIndex < 0 || strings.TrimSpace(payload.RawTarget) == "" {
		return fmt.Errorf("ambiguous link provenance decision binding is invalid")
	}
	if len(payload.CandidateRefs) < 2 || len(payload.Evidence.Candidates) != len(payload.CandidateRefs) || !isFullGitOID(payload.Evidence.SourceIntroductionOID) {
		return fmt.Errorf("ambiguous link provenance evidence is incomplete")
	}
	winners := 0
	previousRef := ""
	for index, evidence := range payload.Evidence.Candidates {
		refKey := repairRefKey(payload.CandidateRefs[index])
		if refKey <= previousRef || !sameRepairRef(evidence.Ref, payload.CandidateRefs[index]) || strings.TrimSpace(evidence.ClaimID) == "" || !evidence.Complete || !isFullGitOID(evidence.IntroductionOID) {
			return fmt.Errorf("ambiguous link candidate evidence is incomplete or unsorted")
		}
		previousRef = refKey
		if evidence.Ancestor {
			winners++
			if !sameRepairRef(evidence.Ref, payload.Winner) {
				return fmt.Errorf("ambiguous link winner was not derived from ancestry")
			}
		}
	}
	if winners != 1 {
		return fmt.Errorf("ambiguous link provenance does not have one unique winner")
	}
	return nil
}

type normalizedAmbiguousLinkRequest struct {
	request       AmbiguousLinkProvenanceRequest
	key           string
	candidateRefs []ontology.NodeRef
	targetOrdinal int
}

func normalizeAmbiguousLinkRequest(raw AmbiguousLinkProvenanceRequest) (normalizedAmbiguousLinkRequest, error) {
	notePath, err := canonicalRepairPath(raw.NotePath)
	if err != nil || notePath != raw.NotePath || !validSHA256Fingerprint(raw.SourceHash) || raw.LinkSnapshot == nil {
		return normalizedAmbiguousLinkRequest{}, fmt.Errorf("ambiguous link source binding is invalid")
	}
	snapshot, err := raw.LinkSnapshot.ValidatedSnapshot()
	if err != nil || snapshot.SourceFingerprint != raw.SourceHash || snapshot.SourceFingerprint != obsidian.StructuredLinkSourceFingerprint(raw.Content) || raw.LinkIndex < 0 || raw.LinkIndex >= len(snapshot.Links) {
		return normalizedAmbiguousLinkRequest{}, fmt.Errorf("ambiguous link index is outside sealed source scan")
	}
	link := snapshot.Links[raw.LinkIndex]
	if link.Target != raw.RawTarget || link.Path != raw.RawTarget || link.Fragment != "" || strings.ContainsAny(raw.RawTarget, "/\\") {
		return normalizedAmbiguousLinkRequest{}, fmt.Errorf("ambiguous provenance requires a structured bare-ID link")
	}
	candidates := make([]Claim, len(raw.Candidates))
	refs := make([]ontology.NodeRef, len(raw.Candidates))
	for index, candidate := range raw.Candidates {
		normalized, err := normalizeClaim(candidate)
		if err != nil || IdentifierComparisonKey(normalized.Value) != IdentifierComparisonKey(raw.RawTarget) {
			return normalizedAmbiguousLinkRequest{}, fmt.Errorf("ambiguous link candidate does not claim raw target")
		}
		candidates[index] = normalized
		refs[index] = nodeRefForCanonicalKey(normalized.Node)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Node.String() < candidates[j].Node.String() })
	for index := range candidates {
		refs[index] = nodeRefForCanonicalKey(candidates[index].Node)
		if index > 0 && sameRepairRef(refs[index-1], refs[index]) {
			return normalizedAmbiguousLinkRequest{}, fmt.Errorf("ambiguous link candidate is duplicated")
		}
	}
	if len(candidates) < 2 {
		return normalizedAmbiguousLinkRequest{}, fmt.Errorf("ambiguous link requires at least two candidates")
	}
	ordinal := 0
	for index := 0; index < raw.LinkIndex; index++ {
		prior := snapshot.Links[index]
		if prior.Path == prior.Target && prior.Fragment == "" && IdentifierComparisonKey(prior.Target) == IdentifierComparisonKey(raw.RawTarget) {
			ordinal++
		}
	}
	raw.Candidates = candidates
	binding, _ := json.Marshal([]any{notePath, raw.SourceHash, raw.LinkIndex, raw.RawTarget, refs})
	sum := sha256.Sum256(binding)
	return normalizedAmbiguousLinkRequest{request: raw, key: hex.EncodeToString(sum[:]), candidateRefs: refs, targetOrdinal: ordinal}, nil
}

func nodeRefForCanonicalKey(key CanonicalNodeKey) ontology.NodeRef {
	kind := ontology.NodeKindNote
	if key.Fragment != "" {
		kind = ontology.NodeKindEmbedded
	}
	return ontology.NodeRef{NotePath: key.NotePath, Fragment: key.Fragment, TypeName: key.TypeName, Kind: kind}
}
