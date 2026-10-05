// Package validate runs vault-wide validation checks and produces fix suggestions.
// Both the CLI product runner (cmd/validation_product_runner.go) and the web
// server (pkg/app/web/) import from here.
package validate

import (
	"context"
	"encoding/json"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/validate/identifierreconcile"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Check name constants.
const (
	CheckBrokenLinks     = "broken_links"
	CheckLinkHygiene     = "link_hygiene"
	CheckOntology        = "ontology"
	CheckCodeFrontmatter = "code_frontmatter"
	CheckCodeAnchors     = "code_anchors"
	CheckIdentifiers     = "identifiers"
	// CheckAliases keeps source compatibility for legacy Go callers. Public
	// aliases/identifier selectors canonicalize to CheckIdentifiers.
	CheckAliases          = CheckIdentifiers
	CheckQueryRecipes     = "query_recipes"
	CheckViews            = "views"
	CheckSkillOverlays    = "skill_overlays"
	CheckCompanionDocs    = "companion_docs"
	CheckFrozenScopeDrift = "frozen_scope_drift"
	// CheckFragileExternal is opt-in only and never appears in DefaultChecks.
	// It audits heading-text external links that should be upgraded or repaired
	// deliberately when headings drift.
	// Coderefs: [[heading-rename-safety-and-fragile-external-links#^spec-0054-us1]]
	CheckFragileExternal = "fragile_external_link"
	// CheckOrphanBlockIDs is opt-in only and never appears in DefaultChecks.
	// Cleanup of unreferenced anchors must be deliberate per SPEC-0023's
	// usage-driven block-id lifecycle.
	// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us4]]
	CheckOrphanBlockIDs = "orphan_block_ids"
	// CheckPlaceholderLinks lists unresolved links whose target never existed.
	// It is audit-only and never appears in DefaultChecks.
	CheckPlaceholderLinks = "placeholder_links"
)

// DefaultChecks is the ordered list of checks to run when none are specified.
// Its contents are derived from the validation check registry.
var DefaultChecks = defaultCheckNames()

// FixSafety categorizes how a fix should be applied.
type FixSafety string

const (
	FixSafetySafe    FixSafety = "safe"
	FixSafetyConfirm FixSafety = "needs_confirmation"
	FixSafetyAgent   FixSafety = "agent_required"
)

// Fix action/edit kind constants.
const (
	FixKindAppendAlias      = "append_alias"
	FixKindSetFrontmatter   = "set_frontmatter"
	FixKindRewriteLinkGroup = "rewrite_broken_link_group"
	// FixKindReviewBrokenLink records a broken-link operation whose target is
	// not deterministic enough to edit. Agent-required actions of this kind
	// intentionally carry evidence and no edits.
	FixKindReviewBrokenLink  = "review_broken_link"
	FixKindRewriteLinkTarget = "rewrite_link_target"
	// FixKindReplaceCodeAnchorTarget replaces one uniquely identified
	// code-anchors selector target after explicit confirmation.
	FixKindReplaceCodeAnchorTarget = "replace_code_anchor_target"
	FixKindAddSectionScaffold      = "add_section_scaffold"
	FixKindOntologySetScalar       = "ontology_set_scalar"
	FixKindOntologySetLink         = "ontology_set_link"
	FixKindOntologyAddSection      = "ontology_add_section"
	FixKindEnsureBlockID           = "ensure_block_id"
	FixKindUpgradeToBlockID        = "upgrade_to_block_id"
	// FixKindRemoveBlockID removes an unreferenced `^block-id` anchor as part
	// of an opt-in orphan-cleanup pass. Never auto-emitted by default checks.
	// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us4]]
	FixKindRemoveBlockID = "remove_block_id"
)

// Issue is a single validation problem. The optional Data payload carries
// code-specific structured data; the client unions on Code to render typed
// widgets. Data must always be JSON-serializable (use mustMarshal).
type Issue struct {
	Key     string          `json:"issueKey,omitempty"`
	Code    string          `json:"code,omitempty"`
	Path    string          `json:"path,omitempty"`
	Type    string          `json:"type,omitempty"`
	Field   string          `json:"field,omitempty"`
	Source  string          `json:"source,omitempty"`
	Target  string          `json:"target,omitempty"`
	Message string          `json:"message,omitempty"`
	Line    int             `json:"line,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	// AffectedPaths and AffectedNotePaths are explicit diagnostic memberships.
	// Snapshot publication never infers cross-file membership from action edits
	// or opaque issue data.
	AffectedPaths      []string       `json:"affectedPaths,omitempty"`
	AffectedNotePaths  []string       `json:"affectedNotePaths,omitempty"`
	AffectedNodeIDs    []string       `json:"affectedNodeIds,omitempty"`
	AffectedTypes      []string       `json:"affectedTypes,omitempty"`
	AffectedInterfaces []string       `json:"affectedInterfaces,omitempty"`
	Location           *IssueLocation `json:"location,omitempty"`
	// Variant names the finding's case within its code, so one decision
	// resolves every finding in it. Nil for codes without cases. It is
	// presentation and grouping evidence only; StableIssueKey excludes it.
	Variant *IssueVariant `json:"variant,omitempty"`
}

// IssueVariant is the specific case of a finding within its issue code. Key
// is stable and comparable within (check, code); Label is display text.
type IssueVariant struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// newIssueVariant returns nil for an empty key, so codes without cases stay
// variant-free.
func newIssueVariant(key, label string) *IssueVariant {
	if key == "" {
		return nil
	}
	return &IssueVariant{Key: key, Label: label}
}

// CheckResult is the outcome of a single validation check.
type CheckResult struct {
	Name         string            `json:"name"`
	OK           bool              `json:"ok"`
	Skipped      bool              `json:"skipped,omitempty"`
	IssueCount   int               `json:"issueCount"`
	Summary      string            `json:"summary,omitempty"`
	Notes        []string          `json:"notes,omitempty"`
	Error        string            `json:"error,omitempty"`
	DurationMs   int64             `json:"durationMs"`
	Issues       []Issue           `json:"issues,omitempty"`
	Buckets      []MigrationBucket `json:"migrationBuckets,omitempty"`
	Fixes        []FixAction       `json:"fixes,omitempty"`
	allIssueKeys []string
	fullIssues   []Issue
	// identifierReconciliation remains internal until suite aggregation lifts
	// the additive public report envelope onto Result.
	identifierReconciliation *identifierreconcile.ReconciliationResult
	identifierRepair         *identifierRepairPayload
}

// MigrationBucket groups repeated validation issues that share one migration
// shape. Buckets are computed over the full issue set before --max-issues
// truncation so migration views can show the real blast radius.
type MigrationBucket struct {
	Code       string    `json:"code"`
	Type       string    `json:"type,omitempty"`
	Field      string    `json:"field,omitempty"`
	Safety     FixSafety `json:"safety"`
	Summary    string    `json:"summary"`
	IssueCount int       `json:"issueCount"`
	Paths      []string  `json:"paths,omitempty"`
	Targets    []string  `json:"targets,omitempty"`
}

// FixEdit is a single file-level edit within a fix action.
type FixEdit struct {
	Kind       string   `json:"kind"`
	NotePath   string   `json:"notePath,omitempty"`
	SourcePath string   `json:"sourcePath,omitempty"`
	Property   string   `json:"property,omitempty"`
	Value      string   `json:"value,omitempty"`
	Values     []string `json:"values,omitempty"`
	OldTarget  string   `json:"oldTarget,omitempty"`
	NewTarget  string   `json:"newTarget,omitempty"`
	// PreserveDisplay keeps a retargeted wikilink's visible text by adding the
	// original target as its alias when it had none.
	PreserveDisplay bool   `json:"preserveDisplay,omitempty"`
	StartByte       int    `json:"startByte,omitempty"`
	EndByte         int    `json:"endByte,omitempty"`
	NodeID          string `json:"nodeId,omitempty"`
	Structural      string `json:"structuralFingerprint,omitempty"`
	BlockID         string `json:"blockId,omitempty"`
}

// FixAction is a suggested fix for one or more issues.
type FixAction struct {
	ID        string    `json:"id"`
	Check     string    `json:"check"`
	IssueCode string    `json:"issueCode,omitempty"`
	Kind      string    `json:"kind"`
	Safety    FixSafety `json:"safety"`
	// Confidence grades candidate evidence behind a retarget: "high" for a
	// normalized title match or git rename, "low" for partial title matches.
	Confidence     string    `json:"confidence,omitempty"`
	Title          string    `json:"title"`
	Summary        string    `json:"summary,omitempty"`
	Question       string    `json:"question,omitempty"`
	InstanceCount  int       `json:"instanceCount,omitempty"`
	IssueKeys      []string  `json:"issueKeys,omitempty"`
	OperationIDs   []string  `json:"operationIds,omitempty"`
	AffectedPaths  []string  `json:"affectedPaths,omitempty"`
	CandidatePaths []string  `json:"candidatePaths,omitempty"`
	Edits          []FixEdit `json:"edits,omitempty"`
}

// FixExecution records which fixes were applied or skipped.
type FixExecution struct {
	RecoveredNamespaces             []NamespaceOutcome                `json:"recoveredNamespaces,omitempty"`
	Requested                       bool                              `json:"requested"`
	NonInteractive                  bool                              `json:"nonInteractive,omitempty"`
	PlanFingerprint                 string                            `json:"planFingerprint,omitempty"`
	PlannedTransactions             int                               `json:"plannedTransactions,omitempty"`
	AppliedTransactions             int                               `json:"appliedTransactions,omitempty"`
	PlannedWrites                   int                               `json:"plannedWrites,omitempty"`
	AppliedWrites                   int                               `json:"appliedWrites,omitempty"`
	Applied                         []string                          `json:"applied,omitempty"`
	Skipped                         []string                          `json:"skipped,omitempty"`
	Failed                          []string                          `json:"failed,omitempty"`
	RemainingIssueKeys              []string                          `json:"remainingIssueKeys,omitempty"`
	RemainingFindings               int                               `json:"remainingFindings,omitempty"`
	ReplanCommand                   string                            `json:"replanCommand,omitempty"`
	Transactions                    []RepairTransactionExecution      `json:"transactions,omitempty"`
	Refresh                         *PostApplyRefreshResult           `json:"refresh,omitempty"`
	FollowUps                       []RepairFollowUp                  `json:"followUps,omitempty"`
	IdentifierReconciliationTimings *identifierreconcile.StageTimings `json:"-"`
	resolvedRecoveryIssueKeys       []string
}

// RepairCompletionArtifact is immutable evidence published with a repair
// transaction. Its target must be beneath the vault's .rhizome/edit-receipts
// directory and must not already exist.
type RepairCompletionArtifact struct {
	Path    string
	Content []byte
}

// RepairTransactionExecution is complete per-component repair evidence.
type RepairTransactionExecution struct {
	TransactionID string   `json:"transactionId"`
	Status        string   `json:"status"`
	Reason        string   `json:"reason,omitempty"`
	OperationIDs  []string `json:"operationIds,omitempty"`
	AffectedPaths []string `json:"affectedPaths,omitempty"`
}

// NextAction is one recommended follow-up after a validation run.
type NextAction struct {
	Category         string   `json:"category"`
	Title            string   `json:"title"`
	Message          string   `json:"message"`
	Command          string   `json:"command,omitempty"`
	Count            int      `json:"count,omitempty"`
	IssueKeys        []string `json:"issueKeys,omitempty"`
	NonFixableReason string   `json:"nonFixableReason,omitempty"`
}

// NextActions summarizes validation follow-up work in a machine-readable shape.
type NextActions struct {
	SafeAutoFixCommand     string       `json:"safeAutoFixCommand,omitempty"`
	SafeFixCount           int          `json:"safeFixCount,omitempty"`
	NeedsConfirmationCount int          `json:"needsConfirmationCount,omitempty"`
	AgentRequiredCount     int          `json:"agentRequiredCount,omitempty"`
	UnclassifiedIssueCount int          `json:"unclassifiedIssueCount,omitempty"`
	CheckErrorCount        int          `json:"checkErrorCount,omitempty"`
	ClassificationRequired bool         `json:"classificationRequired,omitempty"`
	Actions                []NextAction `json:"actions,omitempty"`
}

// Result is the full output of a validation suite run.
type Result struct {
	OK                       bool                                      `json:"ok"`
	IssueCount               int                                       `json:"issueCount"`
	ErrorCount               int                                       `json:"errorCount"`
	DurationMs               int64                                     `json:"durationMs"`
	VaultName                string                                    `json:"-"`
	ApplyCommand             string                                    `json:"-"`
	SelectedChecks           []string                                  `json:"selectedChecks"`
	Checks                   []CheckResult                             `json:"checks"`
	FixPlan                  *FixPlan                                  `json:"fixPlan,omitempty"`
	NextActions              *NextActions                              `json:"nextActions,omitempty"`
	FixExecution             *FixExecution                             `json:"fixExecution,omitempty"`
	RepairJournals           []RepairJournalEvidence                   `json:"repairJournals,omitempty"`
	IdentifierReconciliation *identifierreconcile.ReconciliationResult `json:"identifierReconciliation,omitempty"`
}

// Options controls which checks to run and how fixes are handled.
type Options struct {
	Checks         []string
	SkipAnchors    bool
	SkipEmbeds     bool
	IncludeImages  bool
	MaxIssues      int
	Fix            bool
	NonInteractive bool
	Confirm        func(string) (bool, error)
	// ApplySelection, when non-empty, replaces safety-tier selection: apply
	// exactly the reviewed actions whose ID or any stable issue key matches an
	// entry. Every entry must match, and agent_required actions are rejected.
	ApplySelection     []string
	AllowHistorical    bool
	PostApplyRefresher PostApplyRefresher
	ReplanCommand      string
	// ApplyCommand is the caller's exact positional fix command, including the
	// original selector and vault/scope flags. Internal callers may leave it
	// empty and receive conservative per-check fallback guidance.
	ApplyCommand               string
	CompletionArtifact         *RepairCompletionArtifact
	repairHooks                *repairExecutionHooks
	leaseHeldRepairPlanner     func(context.Context, *IndexLockLease) (*FixPlan, error)
	postApplyCheck             repairPostApplyCheck
	repairSessionPostcheck     func(Result)
	postApplyJournalValidation bool
	ScopeNote                  string
	ScopeTarget                string
	ScopeRef                   string
	// RunContext provides vault primitives. When nil, RunSuiteOnce returns an error.
	RunContext *RunContext
	// postcheckPaths limits an edit-session save's held postcheck to the notes
	// it wrote. Empty means the whole vault.
	postcheckPaths []string
}

// RunContext provides the vault primitives needed by all checks.
type RunContext struct {
	VaultDef   obsidian.VaultDefinition
	VaultPath  string
	VaultMgr   obsidian.VaultManager
	NoteReader obsidian.NoteReader
	// NoteMetadata is the explicit provider-aware projection dependency used
	// by every validation path that refreshes an ontology runtime.
	NoteMetadata notemeta.Indexer
	MaxIssues    int
	// sourceSnapshot and brokenLinkCandidates are immutable execution-only
	// caches. The RunContext returned to repair callers keeps the live reader.
	sourceSnapshot       *validationRunSnapshot
	brokenLinkCandidates *brokenLinkMatcherIndex
	// unresolvedLinks shares one vault scan and one git history read between
	// broken-links and placeholder-links within a suite run.
	unresolvedLinks *sharedUnresolvedLinkScan
	// postcheckPaths is Options.postcheckPaths for the running suite; nil
	// means every note is in scope.
	postcheckPaths map[string]struct{}
}

// inPostcheckScope reports whether findings about any of notePaths belong in
// this run.
func (r RunContext) inPostcheckScope(notePaths ...string) bool {
	if r.postcheckPaths == nil {
		return true
	}
	for _, notePath := range notePaths {
		if _, ok := r.postcheckPaths[notePath]; ok {
			return true
		}
	}
	return false
}
