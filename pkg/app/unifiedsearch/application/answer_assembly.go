package application

import (
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	answeradapt "github.com/atomicobject/rhizome/pkg/app/answer/adapt"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/queryframe"
)

func targetSpecEstablished(spec search.QuerySpec) bool {
	return targetResolutionEstablished(spec.TargetStatus) && spec.ResolutionConfidence >= 0.9 && spec.ResolvedTarget != nil
}

func ResolveNavigationTarget(current TargetResolution, sources []SourceAssessment) TargetResolution {
	if targetResolutionEstablished(current.Status) || current.Status == search.TargetStatusAmbiguous {
		return current
	}
	var candidates []search.TargetCandidate
	for _, source := range sources {
		if !search.HasEvidence(source.Result.Evidence, "note_title_exact", "path_exact") {
			continue
		}
		result := source.Result
		candidates = append(candidates, search.TargetCandidate{Path: result.Path, Symbol: result.Symbol, FQN: result.FQN, Kind: result.Type, Reason: "exact_navigation_match"})
	}
	switch len(candidates) {
	case 0:
		return current
	case 1:
		selected := candidates[0]
		return TargetResolution{Status: search.TargetStatusInferredPath, Confidence: 1, Candidates: candidates, Selected: &selected}
	default:
		return TargetResolution{Status: search.TargetStatusAmbiguous, Confidence: 0.3, Candidates: candidates}
	}
}

// ResolvePrecisionEvidenceTarget binds an explicit file seed to the one target
// proved by the returned structural evidence. File seeds can name a module with
// several declarations, so conflicting or incomplete proof remains unresolved.
func ResolvePrecisionEvidenceTarget(intent search.Intent, current TargetResolution, sources []SourceAssessment) TargetResolution {
	if !search.IsPrecisionIntent(intent) || current.Selected != nil {
		return current
	}
	targets := map[string]search.TargetCandidate{}
	for _, source := range sources {
		if source.Eligibility != EvidencePrimary || source.Relationship != relationshipForIntent(intent) {
			continue
		}
		for _, evidence := range source.Result.Evidence {
			if !search.HasPrimaryEvidence(intent, []search.Evidence{evidence}) || evidence.Details == nil {
				continue
			}
			fqn := strings.TrimSpace(evidence.Details["target_fqn"])
			if fqn == "" {
				continue
			}
			targets[fqn] = search.TargetCandidate{FQN: fqn, Reason: "structural_evidence_target"}
		}
	}
	if len(targets) != 1 {
		return current
	}
	for _, selected := range targets {
		current.Selected = &selected
		current.Candidates = dedupeTargetCandidates(append(current.Candidates, selected))
	}
	return current
}

func dedupeTargetCandidates(candidates []search.TargetCandidate) []search.TargetCandidate {
	seen := map[string]struct{}{}
	out := make([]search.TargetCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		key := strings.Join([]string{candidate.FQN, candidate.Path, candidate.Symbol}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func AvailabilityFromLanes(lanes []search.LaneStatus) Availability {
	if len(lanes) == 0 {
		return AvailabilityUnavailable
	}
	available, unavailable := 0, 0
	for _, lane := range lanes {
		switch lane.Status {
		case search.LaneStateRan, search.LaneStateEmpty:
			available++
		case search.LaneStateCanceled, search.LaneStateTimedOut, search.LaneStateDegraded:
			unavailable++
		}
	}
	if available == 0 {
		return AvailabilityUnavailable
	}
	if unavailable > 0 {
		return AvailabilityPartial
	}
	return AvailabilityComplete
}

func BuildAssessedAnswer(intent search.Intent, query string, target TargetResolution, warnings []search.Warning, availability Availability, sources []SourceAssessment) answer.Response {
	return buildAssessedAnswer(intent, query, nil, target, warnings, availability, sources)
}

func buildAssessedAnswer(intent search.Intent, query string, facets []RequiredFacet, target TargetResolution, warnings []search.Warning, availability Availability, sources []SourceAssessment) answer.Response {
	inputs := make([]answer.Input, 0, len(sources))
	strong, useful := 0, 0
	for _, source := range sources {
		if source.Relevance == RelevanceWeak || source.Relevance == RelevanceUnknown {
			continue
		}
		if source.Relevance == RelevanceStrong {
			strong++
		} else {
			useful++
		}
		r := source.Result
		facetProofs := make([]answer.FacetProof, 0, len(source.FacetSupport))
		for _, facet := range source.FacetSupport {
			facetProofs = append(facetProofs, answer.FacetProof{
				Key: strings.TrimSpace(facet.Text) + "\x00" + string(facet.Mode), Mode: facet.Mode, Strong: facet.Covers(),
				Eligibility: string(facet.Eligibility), Relationship: facet.Relationship, TargetEstablished: facet.TargetEstablished,
			})
		}
		directSpecificity := queryframe.ScoreFields(queryframe.Extract(query), queryframe.Fields{Path: r.Path, Title: r.Title, Symbol: r.Symbol, FQN: r.FQN}).Value
		inputs = append(inputs, answer.Input{Type: r.Type, Path: r.Path, Title: r.Title, Symbol: r.Symbol, FQN: r.FQN, Score: r.FinalScore, Specificity: queryframe.SpecificityScore(r.Evidence), DirectSpecificity: directSpecificity, Granularity: r.Granularity, NodeRef: answeradapt.NodeRef(r.NodeRef), SupportingOnly: search.IsPrecisionIntent(intent) && source.Eligibility != EvidencePrimary, SupportedRoles: append([]string(nil), source.SupportedRoles...), Relevance: string(source.Relevance), Eligibility: string(source.Eligibility), Relationship: source.Relationship, FacetProofs: facetProofs, IdentityMatch: search.HasEvidence(r.Evidence, "path_exact", "note_title_exact", "symbol_exact", "definition_anchor")})
	}
	var packet answer.Response
	if len(facets) == 0 {
		packet = answer.Build(intent, query, target.Status, warnings, inputs)
	} else {
		keys := make([]string, 0, len(facets))
		for _, facet := range facets {
			mode := facet.Mode
			if mode == "" {
				mode = intent
			}
			keys = append(keys, strings.TrimSpace(facet.Text)+"\x00"+string(mode))
		}
		packet = answer.BuildForFacets(intent, query, target.Status, warnings, inputs, keys)
	}
	return finalizeAssessedConfidence(intent, target, availability, strong, useful, sources, packet)
}

func finalizeAssessedConfidence(intent search.Intent, target TargetResolution, availability Availability, strong, useful int, sources []SourceAssessment, packet answer.Response) answer.Response {
	strongByIdentity := make(map[string]bool, len(sources))
	byIdentity := make(map[string]SourceAssessment, len(sources))
	for _, source := range sources {
		identity := resultIdentity(source.Result)
		strongByIdentity[identity] = strongByIdentity[identity] || source.Relevance == RelevanceStrong
		if existing, ok := byIdentity[identity]; !ok || existing.Relevance != RelevanceStrong {
			byIdentity[identity] = source
		}
	}
	allSelectedStrong := len(packet.MustRead) > 0
	mustReadHasIdentityOrTwoLanes := false
	for _, item := range packet.MustRead {
		identity := itemIdentity(item)
		if !strongByIdentity[identity] {
			allSelectedStrong = false
		}
		if source, ok := byIdentity[identity]; ok && (source.IdentityMatch() || source.StrongLanes() >= 2) {
			mustReadHasIdentityOrTwoLanes = true
		}
	}
	switch {
	case target.Status == search.TargetStatusAmbiguous || target.Status == search.TargetStatusUnresolved:
		packet.Confidence = answer.ConfidenceReport{Level: "low", Reason: "target resolution is not unique"}
	case strong == 0 && useful == 0:
		packet.Confidence = answer.ConfidenceReport{Level: "low", Reason: "no source has direct relevance support"}
		packet.MustRead = nil
	case availability == AvailabilityUnavailable:
		packet.Confidence = answer.ConfidenceReport{Level: "low", Reason: "required retrieval evidence is unavailable"}
	case search.IsPrecisionIntent(intent) && (!targetResolutionEstablished(target.Status) || target.Confidence < 0.9 || target.Selected == nil):
		packet.Confidence = answer.ConfidenceReport{Level: "low", Reason: "exact target resolution is not established"}
	case strong == 0:
		packet.Confidence = answer.ConfidenceReport{Level: "low", Reason: "no source has identity-level or corroborated support"}
	case !allSelectedStrong || availability == AvailabilityPartial || len(packet.Coverage.Missing) > 0:
		packet.Confidence = answer.ConfidenceReport{Level: "medium", Reason: "relevant evidence is useful but incomplete", MissingSignals: append([]string(nil), packet.Coverage.Missing...)}
	case !mustReadHasIdentityOrTwoLanes:
		packet.Confidence = answer.ConfidenceReport{Level: "medium", Reason: "strong evidence rests on one lane without an identity match"}
	default:
		packet.Confidence = answer.ConfidenceReport{Level: "high", Reason: "strong relevant evidence covers the requested task"}
	}
	if packet.Confidence.Level == "low" && !search.IsPrecisionIntent(intent) && packet.Confidence.Reason == "no source has identity-level or corroborated support" {
		packet = demoteMustReadWithoutQueryAboutness(byIdentity, packet)
	}
	return answer.RefreshSummary(intent, packet)
}

// queryAboutnessEvidence lists the evidence types that show a source is about
// the query itself. Graph popularity (hub, authority, pagerank, anchor edges,
// seeds) only shows that a source is well connected. A body-term hit
// (intel_fts_match) counts only when the query-specificity support score says
// the source carries a real share of the query, which is what separates a
// code answer from a file that merely mentions one of the words.
var queryAboutnessEvidence = []string{"note_title_match", "intel_doc_match", "symbol_match", "query_match", "note_vector_similarity"}

const (
	// lowConfidenceMustReadFloor is the FinalScore below which a low-confidence
	// page keeps no must-read. Measured across the repository and fixture
	// development splits: no-answer controls score at most 0.84, and judged
	// answers almost never score below 0.87.
	lowConfidenceMustReadFloor = 0.85
	// bodyTermSupportFloor is the query-specificity support a body-term-only hit
	// needs before it counts as being about the query.
	bodyTermSupportFloor = 0.45
)

// sourceIsAboutQuery reports whether a source's own evidence ties it to the
// query.
func sourceIsAboutQuery(source SourceAssessment) bool {
	if source.IdentityMatch() {
		return true
	}
	support := 0.0
	bodyHit := false
	for _, evidence := range source.Result.Evidence {
		for _, aboutness := range queryAboutnessEvidence {
			if evidence.Type == aboutness {
				return true
			}
		}
		switch evidence.Type {
		case "intel_fts_match":
			bodyHit = true
		case "query_specificity":
			if evidence.Source == "queryframe" && evidence.Details != nil {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(evidence.Details["support_score"]), 64); err == nil && parsed > support {
					support = parsed
				}
			}
		}
	}
	return bodyHit && support >= bodyTermSupportFloor
}

// demoteMustReadWithoutQueryAboutness moves must-read items that no source
// shows to be about the query, or that score too low to carry a page on their
// own, to the front of supporting. A low-confidence page that keeps nothing
// says so instead of naming an unrelated source.
func demoteMustReadWithoutQueryAboutness(byIdentity map[string]SourceAssessment, packet answer.Response) answer.Response {
	kept := make([]answer.Item, 0, len(packet.MustRead))
	demoted := make([]answer.Item, 0, len(packet.MustRead))
	for _, item := range packet.MustRead {
		source, ok := byIdentity[itemIdentity(item)]
		if ok && source.Result.FinalScore >= lowConfidenceMustReadFloor && sourceIsAboutQuery(source) {
			kept = append(kept, item)
			continue
		}
		demoted = append(demoted, item)
	}
	if len(demoted) == 0 {
		return packet
	}
	seen := make(map[string]struct{}, len(packet.Supporting))
	for _, item := range packet.Supporting {
		seen[itemIdentity(item)] = struct{}{}
	}
	front := make([]answer.Item, 0, len(demoted))
	for _, item := range demoted {
		identity := itemIdentity(item)
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		front = append(front, item)
	}
	packet.Supporting = append(front, packet.Supporting...)
	if len(kept) == 0 {
		packet.MustRead = nil
		packet.Confidence.Reason = "no source is about the query"
		return packet
	}
	packet.MustRead = kept
	return packet
}

// BuildAssessedAnswerForFacets applies the same pure answer assembly while
// requiring independently supported coverage for every distinct query facet.
func BuildAssessedAnswerForFacets(intent search.Intent, query string, facets []RequiredFacet, target TargetResolution, warnings []search.Warning, availability Availability, sources []SourceAssessment) answer.Response {
	required := make(map[string]RequiredFacet, len(facets))
	for _, facet := range facets {
		facet.Text = strings.TrimSpace(facet.Text)
		if facet.Text == "" {
			continue
		}
		if facet.Mode == "" {
			facet.Mode = intent
		}
		required[facet.Text+"\x00"+string(facet.Mode)] = facet
	}
	if len(required) == 0 || (len(required) == 1 && singleOrdinaryFacet(intent, query, required)) {
		return buildAssessedAnswer(intent, query, nil, target, warnings, availability, sources)
	}
	packet := buildAssessedAnswer(intent, query, facets, target, warnings, availability, sources)
	return applyRequiredFacetCoverage(intent, facets, sources, packet)
}

func targetResolutionEstablished(status search.TargetStatus) bool {
	switch status {
	case search.TargetStatusExplicitPath, search.TargetStatusInferredPath, search.TargetStatusInferredSymbol:
		return true
	default:
		return false
	}
}

// DemoteNonTargetDefinitions keeps only the resolved target's own declaration
// as primary definition evidence. Same-named members in the same file also
// carry definition anchors, but they are not the definition the caller asked
// for and must not count as primary results.
func DemoteNonTargetDefinitions(intent search.Intent, target TargetResolution, sources []SourceAssessment) []SourceAssessment {
	if intent != search.IntentGoToDef || target.Selected == nil || strings.TrimSpace(target.Selected.FQN) == "" {
		return sources
	}
	want := strings.TrimSpace(target.Selected.FQN)
	out := make([]SourceAssessment, len(sources))
	for i, source := range sources {
		if source.Eligibility == EvidencePrimary && strings.TrimSpace(source.Result.FQN) != want {
			source.Eligibility = EvidenceSupporting
			source.Relationship = ""
			if source.Relevance == RelevanceStrong {
				source.Relevance = RelevanceUseful
				source.RelevanceWhy = "same-named declaration is not the resolved definition"
			}
		}
		out[i] = source
	}
	return out
}
