package sqlite

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	ValidationCompletionComplete   = "complete"
	ValidationCompletionIncomplete = "incomplete"

	ValidationCheckOutcomeCompleted     = "completed"
	ValidationCheckOutcomeFailed        = "failed"
	ValidationCheckOutcomeBlocked       = "blocked"
	ValidationCheckOutcomeNotApplicable = "not_applicable"
	ValidationCheckOutcomeSkipped       = "skipped"

	ValidationLocationUnitUTF8Bytes = "utf8_bytes"
	ValidationLocationUnitLine      = "line"
)

var (
	ErrValidationGenerationExpired = errors.New("validation generation expired")
	ErrValidationPageLimit         = errors.New("invalid validation diagnostic page limit")
	ErrValidationPageCursor        = errors.New("invalid validation diagnostic cursor")
	ErrValidationPageFilter        = errors.New("invalid validation diagnostic filter")
	ErrValidationPageSort          = errors.New("invalid validation diagnostic sort")
	ErrValidationScopeRequest      = errors.New("invalid validation scope summary request")
)

const (
	ValidationDiagnosticDefaultPageSize = 100
	ValidationDiagnosticMaxPageSize     = 200
	ValidationDiagnosticSortStable      = "diagnostic_order"
	// ValidationDiagnosticSortFile orders by each diagnostic's file key
	// (primary path, else first affected path, else "" for vault-wide) and
	// reports the filtered total of every file key on the page.
	ValidationDiagnosticSortFile        = "file"
	ValidationRepairAvailabilityAny     = "any"
	ValidationRepairAvailabilityPresent = "present"
	ValidationRepairAvailabilityAbsent  = "absent"
	// Applicable repairs are safe or needs_confirmation actions a reviewer can
	// apply; agent_required guidance does not count.
	ValidationRepairAvailabilityApplicable   = "applicable"
	ValidationRepairAvailabilityInapplicable = "inapplicable"
	ValidationScopeGlobal                    = "global"
	ValidationScopeFile                      = "file"
	ValidationScopeNote                      = "note"
	ValidationScopeNode                      = "node"
	ValidationScopeType                      = "type"
	ValidationScopeInterface                 = "interface"
	ValidationScopeBatchMax                  = 200
)

type ValidationDiagnosticLocation struct {
	Unit     string `json:"unit"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	NodeID   string `json:"nodeId,omitempty"`
	Field    string `json:"field,omitempty"`
	Relation string `json:"relation,omitempty"`
}

type ValidationDiagnostic struct {
	IssueKey           string                        `json:"issueKey"`
	Check              string                        `json:"check"`
	Code               string                        `json:"code,omitempty"`
	Message            string                        `json:"message,omitempty"`
	Evidence           json.RawMessage               `json:"evidence,omitempty"`
	PrimaryPath        string                        `json:"primaryPath,omitempty"`
	AffectedPaths      []string                      `json:"affectedPaths,omitempty"`
	AffectedNotePaths  []string                      `json:"affectedNotePaths,omitempty"`
	Type               string                        `json:"type,omitempty"`
	Field              string                        `json:"field,omitempty"`
	Source             string                        `json:"source,omitempty"`
	Target             string                        `json:"target,omitempty"`
	Location           *ValidationDiagnosticLocation `json:"location,omitempty"`
	ActionIDs          []string                      `json:"actionIds,omitempty"`
	AffectedNodeIDs    []string                      `json:"affectedNodeIds,omitempty"`
	AffectedTypes      []string                      `json:"affectedTypes,omitempty"`
	AffectedInterfaces []string                      `json:"affectedInterfaces,omitempty"`
	Variant            *ValidationIssueVariant       `json:"variant,omitempty"`
}

// ValidationIssueVariant is the specific case of a diagnostic within its code.
type ValidationIssueVariant struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// validationIssueVariant maps stored columns back to an optional variant; an
// empty key means the diagnostic has none.
func validationIssueVariant(key, label string) *ValidationIssueVariant {
	if key == "" {
		return nil
	}
	return &ValidationIssueVariant{Key: key, Label: label}
}

type ValidationActionSnapshot struct {
	ID             string   `json:"id"`
	Check          string   `json:"check"`
	IssueCode      string   `json:"issueCode,omitempty"`
	Kind           string   `json:"kind"`
	Safety         string   `json:"safety"`
	Title          string   `json:"title"`
	Summary        string   `json:"summary,omitempty"`
	Question       string   `json:"question,omitempty"`
	InstanceCount  int      `json:"instanceCount,omitempty"`
	IssueKeys      []string `json:"issueKeys,omitempty"`
	AffectedPaths  []string `json:"affectedPaths,omitempty"`
	CandidatePaths []string `json:"candidatePaths,omitempty"`
}

type ValidationCheckSnapshot struct {
	Check      string   `json:"check"`
	Outcome    string   `json:"outcome"`
	IssueCount int      `json:"issueCount"`
	Summary    string   `json:"summary,omitempty"`
	Notes      []string `json:"notes,omitempty"`
	Error      string   `json:"error,omitempty"`
	DurationMs int64    `json:"durationMs"`
}

type ValidationSnapshot struct {
	IssueCodes            []string                   `json:"issueCodes"`
	VaultIdentity         string                     `json:"vaultIdentity"`
	Generation            int64                      `json:"generation"`
	Scope                 string                     `json:"scope"`
	SelectedChecks        []string                   `json:"selectedChecks"`
	SchemaIdentity        string                     `json:"schemaIdentity,omitempty"`
	ConfigIdentity        string                     `json:"configIdentity,omitempty"`
	InputRevision         string                     `json:"inputRevision,omitempty"`
	StartedAt             int64                      `json:"startedAt"`
	FinishedAt            int64                      `json:"finishedAt"`
	DurationMs            int64                      `json:"durationMs"`
	Completion            string                     `json:"completion"`
	StaleReason           string                     `json:"staleReason,omitempty"`
	IssueCount            int                        `json:"issueCount"`
	ErrorCount            int                        `json:"errorCount"`
	AffectedFileCount     int                        `json:"affectedFileCount"`
	AffectedNoteCount     int                        `json:"affectedNoteCount"`
	RepairActionCount     int                        `json:"repairActionCount"`
	RepairPlanFingerprint string                     `json:"repairPlanFingerprint,omitempty"`
	Checks                []ValidationCheckSnapshot  `json:"checks"`
	Diagnostics           []ValidationDiagnostic     `json:"diagnostics,omitempty"`
	Actions               []ValidationActionSnapshot `json:"actions,omitempty"`
}

type ValidationStateSnapshot struct {
	State       ValidationState
	Snapshot    ValidationSnapshot
	HasSnapshot bool
}

type ValidationDiagnosticPageRequest struct {
	Generation int64
	Limit      int
	Cursor     string
	Filter     ValidationDiagnosticFilter
	Sort       string
}

type ValidationDiagnosticFilter struct {
	Check string `json:"check,omitempty"`
	Code  string `json:"code,omitempty"`
	// Variant is a variant key within Code; it requires Code.
	Variant            string `json:"variant,omitempty"`
	Path               string `json:"path,omitempty"`
	Text               string `json:"text,omitempty"`
	ScopeKind          string `json:"scopeKind,omitempty"`
	ScopeKey           string `json:"scopeKey,omitempty"`
	RepairAvailability string `json:"repairAvailability,omitempty"`
	// InterfaceImplementors maps an interface name to the concrete types that
	// implement it. The store has no schema, so the caller resolves it; an
	// interface scope then also matches notes whose current type is one of
	// them. It is resolution context, not part of the filter identity.
	InterfaceImplementors map[string][]string `json:"-"`
}

type ValidationDiagnosticPage struct {
	Generation     int64                  `json:"generation"`
	FilterIdentity string                 `json:"filterIdentity"`
	Sort           string                 `json:"sort"`
	Returned       int                    `json:"returned"`
	Total          int                    `json:"total"`
	Diagnostics    []ValidationDiagnostic `json:"diagnostics"`
	NextCursor     string                 `json:"nextCursor,omitempty"`
	// FileTotals is set for file sort only: the filtered issue total of every
	// file key on this page.
	FileTotals map[string]int `json:"fileTotals,omitempty"`
}

type ValidationScope struct {
	Kind string `json:"kind"`
	Key  string `json:"key,omitempty"`
}

type ValidationScopeSummary struct {
	Scope             ValidationScope `json:"scope"`
	IssueCount        int             `json:"issueCount"`
	AffectedFileCount int             `json:"affectedFileCount"`
	AffectedNoteCount int             `json:"affectedNoteCount"`
	RepairActionCount int             `json:"repairActionCount"`
}

type ValidationScopeSummaryRequest struct {
	Generation int64                      `json:"generation"`
	Scopes     []ValidationScope          `json:"scopes"`
	Filter     ValidationDiagnosticFilter `json:"filter,omitempty"`
}

type ValidationScopeSummaryResponse struct {
	Generation int64                    `json:"generation"`
	Summaries  []ValidationScopeSummary `json:"summaries"`
}

// ValidationIssueGroupRequest asks for issue counts grouped by check, code,
// and variant within one scope (global when the kind is empty) and filter.
type ValidationIssueGroupRequest struct {
	Generation int64                      `json:"generation"`
	Scope      ValidationScope            `json:"scope"`
	Filter     ValidationDiagnosticFilter `json:"filter,omitempty"`
}

type ValidationIssueGroup struct {
	Check   string                  `json:"check"`
	Code    string                  `json:"code,omitempty"`
	Variant *ValidationIssueVariant `json:"variant,omitempty"`
	// IssueCount and AffectedFileCount equal the scope summary for the same
	// filter narrowed to this check, code, and variant.
	IssueCount        int `json:"issueCount"`
	AffectedFileCount int `json:"affectedFileCount"`
	// ApplicableRepairCount counts issues with a safe or needs_confirmation action.
	ApplicableRepairCount int `json:"applicableRepairCount"`
	// OtherVariants is set on the one row per code that rolls up the variants
	// beyond ValidationIssueGroupVariantLimit; that row has no Variant.
	OtherVariants int `json:"otherVariants,omitempty"`
}

// ValidationIssueGroupVariantLimit bounds the variant rows returned per code.
const ValidationIssueGroupVariantLimit = 25

type ValidationIssueGroupResponse struct {
	Generation int64                  `json:"generation"`
	Groups     []ValidationIssueGroup `json:"groups"`
}

func normalizeValidationDiagnosticPageRequest(request ValidationDiagnosticPageRequest) (ValidationDiagnosticPageRequest, string, error) {
	if request.Generation <= 0 {
		return ValidationDiagnosticPageRequest{}, "", fmt.Errorf("%w: generation must be positive", ErrValidationPageFilter)
	}
	if request.Limit == 0 {
		request.Limit = ValidationDiagnosticDefaultPageSize
	}
	if request.Limit < 1 || request.Limit > ValidationDiagnosticMaxPageSize {
		return ValidationDiagnosticPageRequest{}, "", fmt.Errorf("%w: limit must be between 1 and %d", ErrValidationPageLimit, ValidationDiagnosticMaxPageSize)
	}
	request.Filter.Check = strings.TrimSpace(request.Filter.Check)
	request.Filter.Code = strings.TrimSpace(request.Filter.Code)
	request.Filter.Variant = strings.TrimSpace(request.Filter.Variant)
	if request.Filter.Variant != "" && request.Filter.Code == "" {
		return ValidationDiagnosticPageRequest{}, "", fmt.Errorf("%w: variant requires code", ErrValidationPageFilter)
	}
	request.Filter.Path = strings.TrimSpace(request.Filter.Path)
	request.Filter.Text = strings.TrimSpace(request.Filter.Text)
	request.Filter.ScopeKind = strings.TrimSpace(request.Filter.ScopeKind)
	request.Filter.ScopeKey = strings.TrimSpace(request.Filter.ScopeKey)
	if (request.Filter.ScopeKind == "") != (request.Filter.ScopeKey == "") {
		return ValidationDiagnosticPageRequest{}, "", fmt.Errorf("%w: scope kind and key must be provided together", ErrValidationPageFilter)
	}
	if request.Filter.ScopeKind != "" {
		switch request.Filter.ScopeKind {
		case ValidationScopeFile, ValidationScopeNote, ValidationScopeNode, ValidationScopeType, ValidationScopeInterface:
		default:
			return ValidationDiagnosticPageRequest{}, "", fmt.Errorf("%w: unknown scope kind %q", ErrValidationPageFilter, request.Filter.ScopeKind)
		}
	}
	request.Filter.RepairAvailability = strings.TrimSpace(request.Filter.RepairAvailability)
	if request.Filter.RepairAvailability == "" {
		request.Filter.RepairAvailability = ValidationRepairAvailabilityAny
	}
	switch request.Filter.RepairAvailability {
	case ValidationRepairAvailabilityAny, ValidationRepairAvailabilityPresent, ValidationRepairAvailabilityAbsent,
		ValidationRepairAvailabilityApplicable, ValidationRepairAvailabilityInapplicable:
	default:
		return ValidationDiagnosticPageRequest{}, "", fmt.Errorf("%w: unknown repair availability %q", ErrValidationPageFilter, request.Filter.RepairAvailability)
	}
	request.Sort = strings.TrimSpace(request.Sort)
	if request.Sort == "" {
		request.Sort = ValidationDiagnosticSortStable
	}
	if request.Sort != ValidationDiagnosticSortStable && request.Sort != ValidationDiagnosticSortFile {
		return ValidationDiagnosticPageRequest{}, "", fmt.Errorf("%w: unknown sort %q", ErrValidationPageSort, request.Sort)
	}
	encoded, err := json.Marshal(request.Filter)
	if err != nil {
		return ValidationDiagnosticPageRequest{}, "", err
	}
	digest := sha256.Sum256(append(encoded, []byte("\n"+request.Sort)...))
	return request, hex.EncodeToString(digest[:]), nil
}
