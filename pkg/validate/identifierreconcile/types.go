// Package identifierreconcile provides the deterministic, filesystem-free
// planning core for identifier collision repair.
package identifierreconcile

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// CanonicalNodeKey is the stable claimant identity used for deterministic
// ordering. It intentionally excludes titles, mtimes, and transient row IDs.
type CanonicalNodeKey struct {
	NotePath        string `json:"notePath"`
	Fragment        string `json:"fragment,omitempty"`
	TypeName        string `json:"typeName"`
	IdentifierField string `json:"identifierField"`
}

// NewCanonicalNodeKey validates and normalizes a vault-relative claimant key.
func NewCanonicalNodeKey(notePath, fragment, typeName, identifierField string) (CanonicalNodeKey, error) {
	rel, err := paths.CleanRelPath(notePath)
	if err != nil || rel == "" {
		return CanonicalNodeKey{}, fmt.Errorf("canonical node path must be vault-relative: %w", err)
	}
	fragment = strings.TrimSpace(fragment)
	fragment = strings.TrimPrefix(fragment, "#")
	typeName = strings.TrimSpace(typeName)
	identifierField = strings.TrimSpace(identifierField)
	if typeName == "" || identifierField == "" {
		return CanonicalNodeKey{}, fmt.Errorf("canonical node type and identifier field are required")
	}
	return CanonicalNodeKey{
		NotePath:        rel.String(),
		Fragment:        fragment,
		TypeName:        typeName,
		IdentifierField: identifierField,
	}, nil
}

// String returns an unambiguous canonical tuple suitable for lexical ordering.
func (k CanonicalNodeKey) String() string {
	encoded, _ := json.Marshal([]string{k.NotePath, k.Fragment, k.TypeName, k.IdentifierField})
	return string(encoded)
}

// PoolKey is the normalized schema allocation pool. Strategy is part of pool
// identity so future allocators cannot accidentally share a number line.
type PoolKey struct {
	Strategy  ontology.IdentifierStrategy `json:"strategy"`
	Prefix    string                      `json:"prefix"`
	Separator string                      `json:"separator"`
	Pad       int                         `json:"pad"`
}

// NewPoolKey derives a comparable pool key from the public ontology metadata.
func NewPoolKey(format *ontology.IdentifierFormat) (PoolKey, error) {
	if format == nil {
		return PoolKey{}, fmt.Errorf("identifier format is required")
	}
	strategy := format.Strategy
	if strategy == "" {
		strategy = ontology.IdentifierStrategySequential
	}
	pool := PoolKey{
		Strategy:  strategy,
		Prefix:    strings.TrimSpace(format.Prefix),
		Separator: format.Separator,
		Pad:       format.Pad,
	}
	if _, err := pool.format().StrategyContract(); err != nil {
		return PoolKey{}, err
	}
	if pool.Prefix == "" || pool.Separator == "" {
		return PoolKey{}, fmt.Errorf("identifier pool requires prefix and separator")
	}
	switch pool.Strategy {
	case ontology.IdentifierStrategySequential:
		if pool.Pad < 1 {
			return PoolKey{}, fmt.Errorf("SEQUENTIAL identifier pool requires positive pad")
		}
	case ontology.IdentifierStrategyDateTime:
		if pool.Pad != 0 {
			return PoolKey{}, fmt.Errorf("DATETIME identifier pool requires zero pad")
		}
	}
	return pool, nil
}

// String returns the structured pool tuple used for stable sorting and keys.
func (p PoolKey) String() string {
	encoded, _ := json.Marshal([]any{p.Strategy, p.Prefix, p.Separator, p.Pad})
	return string(encoded)
}

func (p PoolKey) format() *ontology.IdentifierFormat {
	return &ontology.IdentifierFormat{Strategy: p.Strategy, Prefix: p.Prefix, Separator: p.Separator, Pad: p.Pad}
}

// ClaimKind distinguishes authored preferred values from aliases.
type ClaimKind string

const (
	ClaimPreferred ClaimKind = "preferred"
	ClaimAlias     ClaimKind = "alias"
)

// Claim is one node's assertion of a value within an identifier pool.
type Claim struct {
	Node  CanonicalNodeKey `json:"node"`
	Pool  PoolKey          `json:"pool"`
	Value string           `json:"value"`
	Kind  ClaimKind        `json:"kind"`
}

// ID returns the stable evidence lookup key for this exact claim.
func (c Claim) ID() string {
	encoded, _ := json.Marshal([]any{c.Pool.String(), c.Value, c.Node.String(), c.Kind})
	return string(encoded)
}

func (c Claim) membershipID() string {
	encoded, _ := json.Marshal([]any{c.Pool.String(), IdentifierComparisonKey(c.Value), c.Node.String(), c.Kind})
	return string(encoded)
}

// CollisionKind describes the kinds of claims sharing one value.
type CollisionKind string

const (
	CollisionPreferredPreferred CollisionKind = "preferred_preferred"
	CollisionPreferredAlias     CollisionKind = "preferred_alias"
	CollisionAliasAlias         CollisionKind = "alias_alias"
)

// Collision is a deterministic group of distinct nodes claiming one value.
type Collision struct {
	Pool      PoolKey       `json:"pool"`
	Value     string        `json:"value"`
	Kind      CollisionKind `json:"kind"`
	Claimants []Claim       `json:"claimants"`
}

// ProvenanceEvidence is injected historical evidence. The planner never runs
// Git itself and never substitutes filesystem timestamps.
type ProvenanceEvidence struct {
	AuthorDate time.Time `json:"authorDate,omitempty"`
	FullOID    string    `json:"fullOid,omitempty"`
	Complete   bool      `json:"complete"`
	Reason     string    `json:"reason,omitempty"`
}

// KeeperBasis discloses whether Git evidence or the deterministic fallback won.
type KeeperBasis string

const (
	KeeperByGit          KeeperBasis = "git_author_date_oid_key"
	KeeperByCanonicalKey KeeperBasis = "canonical_node_key"
)

// PlannedLoser describes the repair required for a non-keeper claim. Alias-only
// losers have no replacement because removing the colliding alias is sufficient.
type PlannedLoser struct {
	Claim       Claim  `json:"claim"`
	Replacement string `json:"replacement,omitempty"`
}

// PlannedCollision is one stable, independently adaptable collision plan.
// Key is a domain identity, not an EFF-0047 ActionID or IssueKey.
type PlannedCollision struct {
	Key            string              `json:"key"`
	Pool           PoolKey             `json:"pool"`
	Value          string              `json:"value"`
	Kind           CollisionKind       `json:"kind"`
	Keeper         Claim               `json:"keeper"`
	KeeperBasis    KeeperBasis         `json:"keeperBasis"`
	KeeperEvidence *ProvenanceEvidence `json:"keeperEvidence,omitempty"`
	FallbackReason string              `json:"fallbackReason,omitempty"`
	Losers         []PlannedLoser      `json:"losers"`
}

// Plan is the byte-stable pure reconciliation result. Operational counters and
// timings live outside this value so they cannot perturb its fingerprint.
type Plan struct {
	Version     string             `json:"version"`
	Collisions  []PlannedCollision `json:"collisions"`
	Fingerprint string             `json:"fingerprint"`
	sealed      string
}

// StageTimings records end-to-end reconciliation stages without entering the
// deterministic Plan fingerprint. Later orchestration fills stages it owns.
type StageTimings struct {
	Inventory               time.Duration `json:"inventory"`
	GitProvenance           time.Duration `json:"gitProvenance"`
	ReferenceDiscovery      time.Duration `json:"referenceDiscovery"`
	TransactionConstruction time.Duration `json:"transactionConstruction"`
	Apply                   time.Duration `json:"apply"`
	PostValidation          time.Duration `json:"postValidation"`
}

// RunDiagnostics carries observable, deliberately non-fingerprinted planning
// and apply evidence. It is separate from Plan so the same content and local
// history always produce byte-identical canonical plans.
type RunDiagnostics struct {
	Timings            StageTimings       `json:"timings"`
	Git                GitProvenanceStats `json:"git"`
	HistoryComplete    bool               `json:"historyComplete"`
	FallbackCollisions int                `json:"fallbackCollisions"`
}

// ReconciliationResult is the public report envelope for a deterministic plan
// plus runtime evidence. It does not own transaction, lease, or lifecycle types.
type ReconciliationResult struct {
	Plan        *Plan          `json:"plan"`
	Diagnostics RunDiagnostics `json:"diagnostics"`
}
