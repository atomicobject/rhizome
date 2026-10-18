// Package application owns transport-neutral search policy, source assessment,
// and answer assembly. Resource-owning adapters may prepare different runtime
// dependencies, but they must pass the same effective policy to the shared
// engine and consume these results without reinterpreting relevance.
package application

import (
	"errors"
	"fmt"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/search"
)

type Profile string

const (
	ProfileInteractive Profile = "interactive"
	ProfileAgent       Profile = "agent"
)

type EffectivePolicy struct {
	Profile         Profile       `json:"profile"`
	VisibleLimit    int           `json:"visibleLimit"`
	CandidateWindow int           `json:"candidateWindow"`
	MaxPerOwner     int           `json:"maxPerOwner"`
	BudgetChars     int           `json:"budgetChars"`
	Deadline        time.Duration `json:"-"`
	DeadlineMillis  int64         `json:"deadlineMillis"`
}

func ResolveEffectivePolicy(profile Profile, intent search.Intent, explicitLimit, explicitBudget, explicitMaxPerOwner int) (EffectivePolicy, error) {
	switch profile {
	case "", ProfileInteractive:
		profile = ProfileInteractive
	case ProfileAgent:
	default:
		return EffectivePolicy{}, fmt.Errorf("unknown search profile %q", profile)
	}
	policy := EffectivePolicy{Profile: profile, VisibleLimit: 10, CandidateWindow: 100, MaxPerOwner: 2, BudgetChars: 8_000, Deadline: 3 * time.Second}
	if profile == ProfileAgent {
		policy.VisibleLimit = 20
		policy.CandidateWindow = 240
		policy.MaxPerOwner = 3
		policy.BudgetChars = 24_000
		policy.Deadline = 8 * time.Second
	}
	if search.IsPrecisionIntent(intent) {
		policy.MaxPerOwner = 1
		policy.CandidateWindow = min(policy.CandidateWindow, 100)
	}
	if explicitLimit > 0 {
		policy.VisibleLimit = explicitLimit
	}
	if explicitBudget > 0 {
		policy.BudgetChars = explicitBudget
	}
	if explicitMaxPerOwner > 0 {
		policy.MaxPerOwner = explicitMaxPerOwner
	}
	if policy.VisibleLimit > policy.CandidateWindow {
		return EffectivePolicy{}, errors.New("visible limit exceeds fixed candidate window")
	}
	policy.DeadlineMillis = policy.Deadline.Milliseconds()
	return policy, nil
}

type Availability string

const (
	AvailabilityComplete    Availability = "complete"
	AvailabilityPartial     Availability = "partial"
	AvailabilityUnavailable Availability = "unavailable"
)

type EvidenceEligibility string

const (
	EvidencePrimary    EvidenceEligibility = "primary"
	EvidenceSupporting EvidenceEligibility = "supporting"
)

type RelevanceLevel string

const (
	RelevanceStrong  RelevanceLevel = "strong"
	RelevanceUseful  RelevanceLevel = "useful"
	RelevanceWeak    RelevanceLevel = "weak"
	RelevanceUnknown RelevanceLevel = "unknown"
)

type SourceAssessment struct {
	Result         search.RankedResult `json:"result"`
	Eligibility    EvidenceEligibility `json:"eligibility"`
	Relationship   string              `json:"relationship,omitempty"`
	Relevance      RelevanceLevel      `json:"relevance"`
	RelevanceWhy   string              `json:"relevanceWhy,omitempty"`
	SupportedRoles []string            `json:"supportedRoles,omitempty"`
	FacetSupport   []FacetSupport      `json:"facetSupport,omitempty"`
}

// FacetSupport binds a source assessment to the specific query facet it
// supports. A source that is strong for one facet does not imply coverage of a
// different facet merely because both were present in the request.
type FacetSupport struct {
	Text              string              `json:"text"`
	Mode              search.Intent       `json:"mode"`
	Relevance         RelevanceLevel      `json:"relevance"`
	Eligibility       EvidenceEligibility `json:"eligibility,omitempty"`
	Relationship      string              `json:"relationship,omitempty"`
	TargetEstablished bool                `json:"targetEstablished,omitempty"`
	// LeadsFacet marks the facet's own top-ranked result, which is what
	// covers a facet when no source can be strong for a paraphrased question.
	LeadsFacet     bool     `json:"leadsFacet,omitempty"`
	SupportedRoles []string `json:"supportedRoles,omitempty"`
}

type RequiredFacet struct {
	Text string
	Mode search.Intent
}

type TargetResolution struct {
	Status     search.TargetStatus      `json:"status"`
	Confidence float64                  `json:"confidence,omitempty"`
	Candidates []search.TargetCandidate `json:"candidates,omitempty"`
	Selected   *search.TargetCandidate  `json:"selected,omitempty"`
}

type CountSummary struct {
	RetrievedCandidates int `json:"retrievedCandidates"`
	GroupedSources      int `json:"groupedSources"`
	ReturnedSources     int `json:"returnedSources"`
	OmittedBodies       int `json:"omittedBodies"`
	RemainingWindow     int `json:"remainingWindow"`
}

type Result struct {
	Sources      []SourceAssessment  `json:"sources"`
	Target       TargetResolution    `json:"target"`
	Answer       answer.Response     `json:"answer"`
	Warnings     []search.Warning    `json:"warnings,omitempty"`
	Lanes        []search.LaneStatus `json:"lanes,omitempty"`
	Availability Availability        `json:"availability"`
	Counts       CountSummary        `json:"counts"`
}
