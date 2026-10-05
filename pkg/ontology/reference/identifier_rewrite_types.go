package reference

import "github.com/atomicobject/rhizome/pkg/ontology"

// IdentifierRewriteMode distinguishes a preferred-identifier rekey from an
// alias-only collision repair. The zero value preserves preferred-rekey
// behavior for callers constructing the original contract.
type IdentifierRewriteMode string

const (
	// StructuredFieldBlockLocator is the reserved pseudo-field used for an
	// embedded node's parser-owned Markdown block locator. It cannot collide
	// with a GraphQL field name.
	StructuredFieldBlockLocator                           = "$block-locator"
	IdentifierRewritePreferredRekey IdentifierRewriteMode = "PREFERRED_REKEY"
	IdentifierRewriteAliasRemoval   IdentifierRewriteMode = "ALIAS_REMOVAL"
)

// StructuredFieldKind identifies why a discovered value is safe (or unsafe)
// for automatic identifier reconciliation. The planner never scans markdown;
// callers must supply occurrences produced by ontology projection or a bounded
// structured-reference scanner.
type StructuredFieldKind string

const (
	StructuredFieldPreferredIdentifier      StructuredFieldKind = "PREFERRED_IDENTIFIER"
	StructuredFieldAliasIdentifier          StructuredFieldKind = "ALIAS_IDENTIFIER"
	StructuredFieldTypedIdentifierReference StructuredFieldKind = "TYPED_IDENTIFIER_REFERENCE"
	StructuredFieldCanonicalNodeRef         StructuredFieldKind = "CANONICAL_NODE_REF"
	StructuredFieldIdentifierBackedLocator  StructuredFieldKind = "IDENTIFIER_BACKED_LOCATOR"
	StructuredFieldReviewCandidate          StructuredFieldKind = "REVIEW_CANDIDATE"
)

// StructuredFieldEditOperation is a semantic field edit. REPLACE and REMOVE
// own an exact authored value span; APPEND names the exact typed collection and
// leaves source-preserving insertion to the later edit-session adapter.
type StructuredFieldEditOperation string

const (
	StructuredFieldEditReplace StructuredFieldEditOperation = "REPLACE"
	StructuredFieldEditRemove  StructuredFieldEditOperation = "REMOVE"
	StructuredFieldEditAppend  StructuredFieldEditOperation = "APPEND"
)

// IdentifierRewrite declares one canonical identity transition. DerivedFrom
// is the old canonical ref of its owning parent; a non-zero value makes the
// planner validate that the child's old/new identifiers preserve one suffix
// beneath the parent's old/new identifier prefixes. AliasRemoval keeps the
// canonical ref and preferred field unchanged while dropping OldIdentifier.
type IdentifierRewrite struct {
	Mode           IdentifierRewriteMode `json:"mode,omitempty"`
	OldRef         ontology.NodeRef      `json:"oldRef"`
	NewRef         ontology.NodeRef      `json:"newRef"`
	OldIdentifier  string                `json:"oldIdentifier"`
	NewIdentifier  string                `json:"newIdentifier"`
	PreferredField string                `json:"preferredField"`
	AliasesField   string                `json:"aliasesField"`
	// AdditionalAliasesFields owns retiring-value removals; AliasesField alone
	// remains the destination for a replacement preferred-identifier mirror.
	AdditionalAliasesFields []string         `json:"additionalAliasesFields,omitempty"`
	DerivedFrom             ontology.NodeRef `json:"derivedFrom,omitempty"`
}

// StructuredFieldOccurrence is one explicitly typed authored value. Candidates
// contains every canonical target from bounded resolution; automatic inbound
// edits require exactly one candidate.
type StructuredFieldOccurrence struct {
	OwnerRef   ontology.NodeRef    `json:"ownerRef"`
	FieldName  string              `json:"fieldName,omitempty"`
	Kind       StructuredFieldKind `json:"kind"`
	Value      string              `json:"value"`
	Range      ontology.ByteRange  `json:"range"`
	Candidates []ontology.NodeRef  `json:"candidates,omitempty"`
}

// StructuredFieldEdit is a deterministic, non-mutating edit intent.
type StructuredFieldEdit struct {
	OwnerRef     ontology.NodeRef             `json:"ownerRef"`
	FieldName    string                       `json:"fieldName"`
	Kind         StructuredFieldKind          `json:"kind"`
	Operation    StructuredFieldEditOperation `json:"operation"`
	Range        ontology.ByteRange           `json:"range,omitempty"`
	Expected     string                       `json:"expected,omitempty"`
	Replacement  string                       `json:"replacement,omitempty"`
	TargetOldRef ontology.NodeRef             `json:"targetOldRef"`
	TargetNewRef ontology.NodeRef             `json:"targetNewRef"`
}

type IdentifierRewriteDiagnosticKind string

const (
	IdentifierRewriteDiagnosticAmbiguousTarget       IdentifierRewriteDiagnosticKind = "AMBIGUOUS_TARGET"
	IdentifierRewriteDiagnosticUnresolvedTarget      IdentifierRewriteDiagnosticKind = "UNRESOLVED_TARGET"
	IdentifierRewriteDiagnosticReviewOnly            IdentifierRewriteDiagnosticKind = "REVIEW_ONLY"
	IdentifierRewriteDiagnosticInvalidInput          IdentifierRewriteDiagnosticKind = "INVALID_INPUT"
	IdentifierRewriteDiagnosticInvalidDerivedCascade IdentifierRewriteDiagnosticKind = "INVALID_DERIVED_CASCADE"
	IdentifierRewriteDiagnosticConflictingEdit       IdentifierRewriteDiagnosticKind = "CONFLICTING_EDIT"
	IdentifierRewriteDiagnosticMissingPreferred      IdentifierRewriteDiagnosticKind = "MISSING_PREFERRED_IDENTIFIER"
	IdentifierRewriteDiagnosticMissingAlias          IdentifierRewriteDiagnosticKind = "MISSING_ALIAS_IDENTIFIER"
)

// IdentifierRewriteDiagnostic is explicit review/blocking evidence. Candidates
// are sorted by canonical identity so diagnostics are stable across store order.
type IdentifierRewriteDiagnostic struct {
	Kind       IdentifierRewriteDiagnosticKind `json:"kind"`
	OwnerRef   ontology.NodeRef                `json:"ownerRef,omitempty"`
	FieldName  string                          `json:"fieldName,omitempty"`
	Range      ontology.ByteRange              `json:"range,omitempty"`
	Value      string                          `json:"value,omitempty"`
	Candidates []ontology.NodeRef              `json:"candidates,omitempty"`
	Message    string                          `json:"message"`
}

type IdentifierFieldRewriteInput struct {
	Rewrites    []IdentifierRewrite         `json:"rewrites"`
	Occurrences []StructuredFieldOccurrence `json:"occurrences"`
}

type IdentifierFieldRewritePlan struct {
	Fingerprint string                        `json:"fingerprint"`
	Edits       []StructuredFieldEdit         `json:"edits"`
	Diagnostics []IdentifierRewriteDiagnostic `json:"diagnostics,omitempty"`
	sealed      string
}
