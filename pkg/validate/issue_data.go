package validate

import "encoding/json"

// CodeFrontmatterData preserves the parser detail and exact rerun guidance
// that the former count-only code-frontmatter result discarded.
type CodeFrontmatterData struct {
	Diagnostic string `json:"diagnostic"`
	Guidance   string `json:"guidance"`
}

// CodeAnchorData keeps source identity and suffix candidates separate from
// the human message so consumers can distinguish exact misses, unique repair
// candidates, and semantic ambiguity.
type CodeAnchorData struct {
	Label      string   `json:"label"`
	Language   string   `json:"language,omitempty"`
	Status     string   `json:"status"`
	Candidates []string `json:"candidates,omitempty"`
	Guidance   string   `json:"guidance"`
}

// QueryRecipeIssueData promotes the recipe diagnostic identity out of the
// human message so multiple compiler diagnostics at the same source location
// retain distinct stable issue and repair-action identities.
type QueryRecipeIssueData struct {
	Recipe     string `json:"recipe"`
	Line       int    `json:"line,omitempty"`
	Diagnostic string `json:"diagnostic"`
}

// DuplicatePreferredIdentifierData is the structured payload for the
// duplicate_preferred_identifier issue code. It lists every note that
// declares the same preferred identifier so the UI can render a widget
// rather than a flat message string.
type DuplicatePreferredIdentifierData struct {
	Identifier string   `json:"identifier"`
	Owners     []string `json:"owners"`
}

// IdentifierCollisionClaimant identifies one preferred or alias assertion in
// a pool-aware identifier collision. Fragment distinguishes embedded nodes
// from their containing note without degrading canonical identity to a path.
type IdentifierCollisionClaimant struct {
	NodePath    string `json:"nodePath"`
	Fragment    string `json:"fragment,omitempty"`
	TypeName    string `json:"typeName"`
	FieldName   string `json:"fieldName"`
	Kind        string `json:"kind"`
	Value       string `json:"value"`
	Replacement string `json:"replacement,omitempty"`
}

// IdentifierCollisionData is the additive product payload for every
// preferred/alias collision. Materializable false is deliberate evidence:
// callers must render Guidance rather than implying a no-op action is safe.
type IdentifierCollisionData struct {
	Identifier      string                        `json:"identifier"`
	Kind            string                        `json:"kind"`
	Pool            string                        `json:"pool,omitempty"`
	Claimant        IdentifierCollisionClaimant   `json:"claimant"`
	Claimants       []IdentifierCollisionClaimant `json:"claimants"`
	Keeper          IdentifierCollisionClaimant   `json:"keeper"`
	KeeperBasis     string                        `json:"keeperBasis,omitempty"`
	FallbackReason  string                        `json:"fallbackReason,omitempty"`
	Materializable  bool                          `json:"materializable"`
	Guidance        string                        `json:"guidance,omitempty"`
	PlanFingerprint string                        `json:"planFingerprint,omitempty"`
}

// IdentifierStrategyMigrationData is bounded per-member evidence for a
// pool-scoped migration action. The complete private mapping remains sealed in
// identifier repair authority rather than being repeated in every issue.
type IdentifierStrategyMigrationData struct {
	SourcePool      string `json:"sourcePool,omitempty"`
	TargetPool      string `json:"targetPool"`
	OldIdentifier   string `json:"oldIdentifier,omitempty"`
	NewIdentifier   string `json:"newIdentifier,omitempty"`
	MemberCount     int    `json:"memberCount"`
	Materializable  bool   `json:"materializable"`
	BlockReason     string `json:"blockReason,omitempty"`
	Guidance        string `json:"guidance,omitempty"`
	PlanFingerprint string `json:"planFingerprint,omitempty"`
}

// BrokenLinkData is the structured payload for broken-link issues.
// The unresolved target is shown with any candidate retarget the validator
// inferred from ranked title/token matching. Reason discriminates between
// missing-note breakage and fragment-level breakage (block ID or heading
// text typos against an otherwise-resolvable note).
//
// WHY: SPEC-0023's orphan-block-id check needs to know which inbound
// references are typo'd-fragment soft holds vs. fully resolved references,
// and which are plain missing-note breakage that does not protect any
// anchor. Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us4]]
type BrokenLinkData struct {
	Target     string   `json:"target"`
	LinkType   string   `json:"linkType,omitempty"`
	Alias      string   `json:"alias,omitempty"`
	Fragment   string   `json:"fragment,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
	// CandidateConfidence is "high" when candidates come from a normalized
	// title match or git rename and "low" for partial title matches.
	CandidateConfidence string `json:"candidateConfidence,omitempty"`
}

// OntologyIssueData preserves the identity fields that distinguish otherwise
// identical-looking ontology findings. Stable issue keys include this payload,
// so repair suggestions can join by the exact originating issue only.
type OntologyIssueData struct {
	NodeRef      string `json:"nodeRef,omitempty"`
	NodeID       string `json:"nodeId,omitempty"`
	Structural   string `json:"structuralFingerprint,omitempty"`
	LinkKind     string `json:"linkKind,omitempty"`
	FixTarget    string `json:"fixTarget,omitempty"`
	FixFieldName string `json:"fixFieldName,omitempty"`
	// CandidateTypes lists the sorted types a type_ambiguous note matches,
	// which are the choices that resolve it.
	CandidateTypes []string `json:"candidateTypes,omitempty"`
}

// mustMarshal serializes issue payloads. Panics on failure because the
// inputs are always plain structs with JSON-safe fields.
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
