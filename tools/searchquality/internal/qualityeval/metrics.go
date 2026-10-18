// Package qualityeval evaluates frozen search result artifacts without running retrieval.
package qualityeval

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
)

type scoredCase struct {
	ndcg, recall, nav1, nav3, mustRead, roles, relationships, facets, primaryPurity                                                    float64
	ndcgEligible, recallEligible, navEligible, mustReadEligible, rolesEligible, relationshipsEligible, facetsEligible, primaryEligible bool
	high, highCorrect, highAnswerable, answerable                                                                                      bool
	unjudged                                                                                                                           int
}

func scoreCase(c Case, r QueryResult) scoredCase {
	s := scoredCase{answerable: c.Control != "no_answer"}
	s.ndcg, s.unjudged, s.ndcgEligible = ndcg(c.Judgments, r.Sources, 10)
	s.recallEligible = len(c.RequiredSources) > 0
	if s.recallEligible {
		s.recall = recall(c.RequiredSources, r.Sources, 20)
	}
	if c.Family == "navigation" && len(c.RequiredSources) > 0 && c.Control == "" {
		s.navEligible = true
		s.nav1 = containsAny(r.Sources, c.RequiredSources, 1)
		s.nav3 = containsAny(r.Sources, c.RequiredSources, 3)
	}
	s.mustReadEligible = s.answerable
	if s.mustReadEligible {
		s.mustRead = precision(c.Judgments, r.MustRead)
	}
	s.rolesEligible = len(c.RequiredRoles) > 0
	if s.rolesEligible {
		s.roles = roleCoverage(c, r.Roles)
	}
	s.relationshipsEligible = len(c.Relationships) > 0
	if s.relationshipsEligible {
		s.relationships = evidenceCoverage(c.Relationships, c.Judgments, r.Relationships, func(j Judgment) []string { return j.SupportedRelationships })
	}
	s.facetsEligible = len(c.Facets) > 1
	if s.facetsEligible {
		requiredFacets := make([]string, 0, len(c.Facets))
		for _, facet := range c.Facets {
			requiredFacets = append(requiredFacets, facet.Text)
		}
		s.facets = evidenceCoverage(requiredFacets, c.Judgments, r.Facets, func(j Judgment) []string { return j.SupportedFacets })
	}
	s.primaryEligible = c.Family == "structural_precision" && len(c.Relationships) > 0
	if s.primaryEligible {
		s.primaryPurity = primaryResultPurity(c, r)
	}
	s.high = r.Confidence == "high"
	s.highAnswerable = s.high && s.answerable
	relevant := !s.mustReadEligible || s.mustRead == 1
	covered := (!s.rolesEligible || s.roles == 1) && (!s.relationshipsEligible || s.relationships == 1) && (!s.facetsEligible || s.facets == 1)
	s.highCorrect = s.high && s.answerable && relevant && covered && r.Status != "ambiguous" && r.Status != "unresolved"
	return s
}

func aggregate(cases []scoredCase) Metrics {
	m := Metrics{Cases: len(cases)}
	for _, c := range cases {
		m.UnjudgedTop10 += c.unjudged
		if c.ndcgEligible {
			m.NDCG10 += c.ndcg
			m.NDCG10Cases++
		}
		if c.recallEligible {
			m.RequiredRecall20 += c.recall
			m.RequiredRecall20Cases++
		}
		if c.navEligible {
			m.NavigationSuccess1 += c.nav1
			m.NavigationSuccess3 += c.nav3
			m.NavigationCases++
		}
		if c.mustReadEligible {
			m.MustReadPrecision += c.mustRead
			m.MustReadPrecisionCases++
		}
		if c.rolesEligible {
			m.RequiredRoleCoverage += c.roles
			m.RequiredRoleCoverageCases++
		}
		if c.relationshipsEligible {
			m.RequiredRelationshipCoverage += c.relationships
			m.RequiredRelationshipCoverageCases++
		}
		if c.facetsEligible {
			m.RequiredFacetCoverage += c.facets
			m.RequiredFacetCoverageCases++
		}
		if c.primaryEligible {
			m.PrimaryResultPurity += c.primaryPurity
			m.PrimaryResultPurityCases++
		}
		if c.highAnswerable {
			m.HighConfidenceCases++
			if c.highCorrect {
				m.HighConfidencePrecision++
			}
		}
		if c.answerable {
			m.AnswerableCases++
			if c.highAnswerable {
				m.HighConfidenceCoverage++
			}
		}
	}
	if m.NDCG10Cases > 0 {
		m.NDCG10 /= float64(m.NDCG10Cases)
	}
	if m.RequiredRecall20Cases > 0 {
		m.RequiredRecall20 /= float64(m.RequiredRecall20Cases)
	}
	if m.NavigationCases > 0 {
		m.NavigationSuccess1 /= float64(m.NavigationCases)
		m.NavigationSuccess3 /= float64(m.NavigationCases)
	}
	if m.MustReadPrecisionCases > 0 {
		m.MustReadPrecision /= float64(m.MustReadPrecisionCases)
	}
	if m.RequiredRoleCoverageCases > 0 {
		m.RequiredRoleCoverage /= float64(m.RequiredRoleCoverageCases)
	}
	if m.RequiredRelationshipCoverageCases > 0 {
		m.RequiredRelationshipCoverage /= float64(m.RequiredRelationshipCoverageCases)
	}
	if m.RequiredFacetCoverageCases > 0 {
		m.RequiredFacetCoverage /= float64(m.RequiredFacetCoverageCases)
	}
	if m.PrimaryResultPurityCases > 0 {
		m.PrimaryResultPurity /= float64(m.PrimaryResultPurityCases)
	}
	if m.HighConfidenceCases > 0 {
		m.HighConfidencePrecision /= float64(m.HighConfidenceCases)
	}
	if m.AnswerableCases > 0 {
		m.HighConfidenceCoverage /= float64(m.AnswerableCases)
	}
	return m
}

func ndcg(j map[string]Judgment, sources []string, k int) (float64, int, bool) {
	grades := []int{}
	for _, x := range j {
		if x.Grade > 0 {
			grades = append(grades, x.Grade)
		}
	}
	if len(grades) == 0 {
		return 0, 0, false
	}
	dcg, u := 0.0, 0
	for i, s := range sources {
		if i >= k {
			break
		}
		x, ok := j[s]
		if !ok {
			u++
			continue
		}
		dcg += (math.Pow(2, float64(x.Grade)) - 1) / math.Log2(float64(i)+2)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(grades)))
	ideal := 0.0
	for i, g := range grades {
		if i >= k {
			break
		}
		ideal += (math.Pow(2, float64(g)) - 1) / math.Log2(float64(i)+2)
	}
	return dcg / ideal, u, true
}

func recall(required, got []string, limit int) float64 {
	if limit <= 0 || limit > len(got) {
		limit = len(got)
	}
	seen := map[string]struct{}{}
	for _, v := range got[:limit] {
		seen[v] = struct{}{}
	}
	n := 0
	for _, v := range required {
		if _, ok := seen[v]; ok {
			n++
		}
	}
	return float64(n) / float64(len(required))
}

func precision(j map[string]Judgment, selected []string) float64 {
	if len(selected) == 0 {
		return 0
	}
	n := 0
	for _, s := range selected {
		if j[s].Grade >= 2 {
			n++
		}
	}
	return float64(n) / float64(len(selected))
}

func roleCoverage(c Case, a []RoleAssignment) float64 {
	covered := map[string]struct{}{}
	for _, x := range a {
		judgment := c.Judgments[x.Source]
		if judgment.Grade >= 2 && containsStringFold(judgment.SupportedRoles, x.Role) {
			covered[x.Role] = struct{}{}
		}
	}
	n := 0
	for _, role := range c.RequiredRoles {
		if _, ok := covered[role]; ok {
			n++
		}
	}
	return float64(n) / float64(len(c.RequiredRoles))
}

func evidenceCoverage(required []string, judgments map[string]Judgment, assignments []EvidenceAssignment, supported func(Judgment) []string) float64 {
	if len(required) == 0 {
		return 0
	}
	covered := map[string]struct{}{}
	for _, assignment := range assignments {
		judgment := judgments[assignment.Source]
		if judgment.Grade >= 2 && containsStringFold(supported(judgment), assignment.Value) {
			covered[strings.ToLower(strings.TrimSpace(assignment.Value))] = struct{}{}
		}
	}
	n := 0
	for _, value := range required {
		if _, ok := covered[strings.ToLower(strings.TrimSpace(value))]; ok {
			n++
		}
	}
	return float64(n) / float64(len(required))
}

func primaryResultPurity(c Case, r QueryResult) float64 {
	resolved := r.Status == "explicit_path" || r.Status == "inferred_path" || r.Status == "inferred_symbol"
	targetCorrect := resolved && (strings.TrimSpace(c.ExpectedTarget) == "" || containsExactIdentity(r.TargetIdentities, c.ExpectedTarget))
	primary, correct := 0, 0
	for _, candidate := range r.Candidates {
		if candidate.Eligibility != "primary" || candidate.Page > 1 {
			continue
		}
		primary++
		judgment, ok := c.Judgments[candidate.Identity]
		if !ok || judgment.Grade < 2 || !targetCorrect || !containsStringFold(c.Relationships, candidate.Relationship) || !containsStringFold(judgment.SupportedRelationships, candidate.Relationship) {
			continue
		}
		correct++
	}
	if primary == 0 {
		return 0
	}
	return float64(correct) / float64(primary)
}

func containsExactIdentity(values []string, target string) bool {
	target = filepath.ToSlash(strings.TrimSpace(target))
	for _, value := range values {
		if filepath.ToSlash(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}

func containsStringFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func containsAny(got, required []string, limit int) float64 {
	if limit > len(got) {
		limit = len(got)
	}
	for _, v := range got[:limit] {
		for _, w := range required {
			if v == w {
				return 1
			}
		}
	}
	return 0
}
