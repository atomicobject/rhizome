package reference

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// IdentifierReviewCandidateInput binds review discovery to the same sealed
// source scan and exact structured-field ranges used by automatic planning.
type IdentifierReviewCandidateInput struct {
	NotePath           string                               `json:"notePath"`
	Content            string                               `json:"content"`
	LinkSnapshot       *obsidian.StructuredLinkScanSnapshot `json:"-"`
	Rewrites           []IdentifierRewrite                  `json:"rewrites"`
	HandledFieldRanges []ontology.ByteRange                 `json:"handledFieldRanges,omitempty"`
}

// IdentifierReviewCandidate is source-bound evidence only. Rewrites retains
// every semantic collision membership sharing the authored token.
type IdentifierReviewCandidate struct {
	NotePath string                          `json:"notePath"`
	Region   obsidian.IdentifierReviewRegion `json:"region"`
	Range    ontology.ByteRange              `json:"range"`
	Value    string                          `json:"value"`
	Rewrites []IdentifierRewrite             `json:"rewrites"`
}

// IdentifierReviewCandidatePlan deliberately has no edits. REVIEW_ONLY
// diagnostics are the only successful output.
type IdentifierReviewCandidatePlan struct {
	NotePath            string                        `json:"notePath"`
	SourceFingerprint   string                        `json:"sourceFingerprint"`
	Rewrites            []IdentifierRewrite           `json:"rewrites"`
	HandledFieldRanges  []ontology.ByteRange          `json:"handledFieldRanges,omitempty"`
	Candidates          []IdentifierReviewCandidate   `json:"candidates"`
	Diagnostics         []IdentifierRewriteDiagnostic `json:"diagnostics,omitempty"`
	BlockingDiagnostics []IdentifierRewriteDiagnostic `json:"blockingDiagnostics,omitempty"`
	Fingerprint         string                        `json:"fingerprint"`

	sealed string
}

// PlanIdentifierReviewCandidates discovers exact prose/code tokens while
// excluding already handled structured fields and links.
func PlanIdentifierReviewCandidates(input IdentifierReviewCandidateInput) IdentifierReviewCandidatePlan {
	plan := IdentifierReviewCandidatePlan{}
	notePath, err := paths.CleanRelPath(strings.TrimSpace(input.NotePath))
	if err != nil || notePath.String() == "" {
		plan.BlockingDiagnostics = append(plan.BlockingDiagnostics, reviewInputDiagnostic(input.NotePath, "review candidate source path must be vault-relative"))
		return finalizeIdentifierReviewCandidatePlan(plan)
	}
	plan.NotePath = notePath.String()
	rewrites := append([]IdentifierRewrite(nil), input.Rewrites...)
	sort.Slice(rewrites, func(i, j int) bool { return rewriteSortKey(rewrites[i]) < rewriteSortKey(rewrites[j]) })
	valid, _, rewriteDiagnostics := validateIdentifierRewrites(rewrites)
	plan.Rewrites = append([]IdentifierRewrite(nil), valid...)
	plan.BlockingDiagnostics = append(plan.BlockingDiagnostics, rewriteDiagnostics...)
	if len(rewriteDiagnostics) > 0 || input.LinkSnapshot == nil {
		if input.LinkSnapshot == nil {
			plan.BlockingDiagnostics = append(plan.BlockingDiagnostics, reviewInputDiagnostic(plan.NotePath, "complete sealed link scan is required for review discovery"))
		}
		return finalizeIdentifierReviewCandidatePlan(plan)
	}

	for _, sourceRange := range input.HandledFieldRanges {
		if !sourceRange.Valid(len(input.Content)) || sourceRange.Len() == 0 {
			plan.BlockingDiagnostics = append(plan.BlockingDiagnostics, reviewInputDiagnostic(plan.NotePath, "handled structured field range is outside source content"))
			return finalizeIdentifierReviewCandidatePlan(plan)
		}
	}
	plan.HandledFieldRanges = canonicalReviewRanges(input.HandledFieldRanges)
	handled := make([]obsidian.StructuredLinkSpan, 0, len(plan.HandledFieldRanges))
	for _, sourceRange := range plan.HandledFieldRanges {
		handled = append(handled, obsidian.StructuredLinkSpan{Start: sourceRange.Start, End: sourceRange.End, Present: true})
	}
	identifiers := make([]string, 0, len(valid))
	for _, rewrite := range valid {
		identifiers = append(identifiers, rewrite.OldIdentifier)
	}
	discovered, err := obsidian.ScanIdentifierReviewCandidates(input.Content, *input.LinkSnapshot, identifiers, handled)
	if err != nil {
		plan.BlockingDiagnostics = append(plan.BlockingDiagnostics, reviewInputDiagnostic(plan.NotePath, err.Error()))
		return finalizeIdentifierReviewCandidatePlan(plan)
	}
	plan.SourceFingerprint = obsidian.StructuredLinkSourceFingerprint(input.Content)
	for _, raw := range discovered {
		members := reviewCandidateRewrites(raw.Value, valid)
		if len(members) == 0 {
			continue
		}
		candidate := IdentifierReviewCandidate{
			NotePath: plan.NotePath, Region: raw.Region,
			Range: ontology.ByteRange{Start: raw.Span.Start, End: raw.Span.End}, Value: raw.Value,
			Rewrites: members,
		}
		plan.Candidates = append(plan.Candidates, candidate)
		refs := make([]ontology.NodeRef, 0, len(members))
		for _, rewrite := range members {
			refs = append(refs, rewrite.OldRef)
		}
		message := "plain-text identifier mention requires review and is never auto-edited"
		if raw.Region == obsidian.IdentifierReviewCode {
			message = "source-code identifier token requires review and is never auto-edited"
		}
		plan.Diagnostics = append(plan.Diagnostics, IdentifierRewriteDiagnostic{
			Kind: IdentifierRewriteDiagnosticReviewOnly, OwnerRef: ontology.NodeRef{NotePath: plan.NotePath},
			Range: candidate.Range, Value: candidate.Value, Candidates: sortedUniqueRefs(refs), Message: message,
		})
	}
	return finalizeIdentifierReviewCandidatePlan(plan)
}

func canonicalReviewRanges(input []ontology.ByteRange) []ontology.ByteRange {
	out := append([]ontology.ByteRange(nil), input...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].End < out[j].End
	})
	merged := out[:0]
	for _, sourceRange := range out {
		if len(merged) == 0 || sourceRange.Start > merged[len(merged)-1].End {
			merged = append(merged, sourceRange)
			continue
		}
		if sourceRange.End > merged[len(merged)-1].End {
			merged[len(merged)-1].End = sourceRange.End
		}
	}
	return merged
}

func reviewCandidateRewrites(value string, rewrites []IdentifierRewrite) []IdentifierRewrite {
	var out []IdentifierRewrite
	for _, rewrite := range rewrites {
		if identifierEqual(value, rewrite.OldIdentifier) {
			out = append(out, rewrite)
		}
	}
	sort.Slice(out, func(i, j int) bool { return rewriteSortKey(out[i]) < rewriteSortKey(out[j]) })
	return out
}

func reviewInputDiagnostic(notePath, message string) IdentifierRewriteDiagnostic {
	return IdentifierRewriteDiagnostic{
		Kind: IdentifierRewriteDiagnosticInvalidInput, OwnerRef: ontology.NodeRef{NotePath: notePath}, Message: message,
	}
}

func finalizeIdentifierReviewCandidatePlan(plan IdentifierReviewCandidatePlan) IdentifierReviewCandidatePlan {
	plan = cloneIdentifierReviewCandidatePlan(plan)
	plan.Fingerprint = identifierReviewCandidatePlanFingerprint(plan)
	plan.sealed = plan.Fingerprint
	return plan
}

// ValidatedSnapshot returns a detached plan only while all public evidence
// still matches the private planner seal.
func (plan IdentifierReviewCandidatePlan) ValidatedSnapshot() (*IdentifierReviewCandidatePlan, error) {
	fingerprint := identifierReviewCandidatePlanFingerprint(plan)
	if plan.sealed == "" || plan.Fingerprint != plan.sealed || fingerprint != plan.sealed {
		return nil, fmt.Errorf("identifier review candidate plan changed after planning")
	}
	snapshot := cloneIdentifierReviewCandidatePlan(plan)
	return &snapshot, nil
}

func identifierReviewCandidatePlanFingerprint(plan IdentifierReviewCandidatePlan) string {
	encoded, _ := json.Marshal(struct {
		NotePath            string
		SourceFingerprint   string
		Rewrites            []IdentifierRewrite
		HandledFieldRanges  []ontology.ByteRange
		Candidates          []IdentifierReviewCandidate
		Diagnostics         []IdentifierRewriteDiagnostic
		BlockingDiagnostics []IdentifierRewriteDiagnostic
	}{plan.NotePath, plan.SourceFingerprint, plan.Rewrites, plan.HandledFieldRanges, plan.Candidates, plan.Diagnostics, plan.BlockingDiagnostics})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func cloneIdentifierReviewCandidatePlan(plan IdentifierReviewCandidatePlan) IdentifierReviewCandidatePlan {
	out := plan
	out.Rewrites = append([]IdentifierRewrite(nil), plan.Rewrites...)
	out.HandledFieldRanges = append([]ontology.ByteRange(nil), plan.HandledFieldRanges...)
	out.Candidates = append([]IdentifierReviewCandidate(nil), plan.Candidates...)
	for index := range out.Candidates {
		out.Candidates[index].Rewrites = append([]IdentifierRewrite(nil), plan.Candidates[index].Rewrites...)
	}
	out.Diagnostics = cloneIdentifierReviewDiagnostics(plan.Diagnostics)
	out.BlockingDiagnostics = cloneIdentifierReviewDiagnostics(plan.BlockingDiagnostics)
	return out
}

func cloneIdentifierReviewDiagnostics(input []IdentifierRewriteDiagnostic) []IdentifierRewriteDiagnostic {
	out := append([]IdentifierRewriteDiagnostic(nil), input...)
	for index := range out {
		out[index].Candidates = append([]ontology.NodeRef(nil), input[index].Candidates...)
	}
	return out
}
