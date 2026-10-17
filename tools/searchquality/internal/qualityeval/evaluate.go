// Package qualityeval evaluates frozen search result artifacts without running retrieval.
package qualityeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"sort"
	"strings"
)

func Fingerprint(c Corpus) (string, error) {
	b, e := json.Marshal(c)
	if e != nil {
		return "", e
	}
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:]), nil
}

func Evaluate(c Corpus, r Run) (Report, error) { return EvaluateSplit(c, r, "") }

func EvaluateSplit(corpus Corpus, run Run, split string) (Report, error) {
	return EvaluateSelection(corpus, run, Selection{Split: split})
}

// EvaluateJudgmentRescore evaluates preserved retrieval results against updated
// source judgments while proving the executed query contract did not change.
// The original corpus must match the run fingerprint exactly. Only judgments
// and their assessment metadata may differ in the updated corpus.
func EvaluateJudgmentRescore(original, updated Corpus, run Run, selection Selection) (Report, error) {
	originalFingerprint, err := validateCorpus(original)
	if err != nil {
		return Report{}, fmt.Errorf("validate original corpus: %w", err)
	}
	if run.CorpusFingerprint == "" || run.CorpusFingerprint != originalFingerprint {
		return Report{}, fmt.Errorf("original corpus fingerprint mismatch: run=%q corpus=%q", run.CorpusFingerprint, originalFingerprint)
	}
	if !sameExecutionCorpusContract(original, updated) {
		return Report{}, fmt.Errorf("judgment rescore changed the corpus execution contract")
	}
	updatedFingerprint, err := validateCorpus(updated)
	if err != nil {
		return Report{}, fmt.Errorf("validate updated corpus: %w", err)
	}
	rescoredRun, projected, err := projectUniqueJudgmentIdentities(updated, run, selection)
	if err != nil {
		return Report{}, err
	}
	rescoredRun.CorpusFingerprint = updatedFingerprint
	report, err := EvaluateSelection(updated, rescoredRun, selection)
	if err != nil {
		return Report{}, err
	}
	report.RescoredFromCorpusFingerprint = originalFingerprint
	if projected > 0 {
		report.JudgmentIdentityProjection = &IdentityProjection{
			Method:          "unique-candidate-identity-v1",
			AliasesReplaced: projected,
		}
	}
	return report, nil
}

func projectUniqueJudgmentIdentities(corpus Corpus, run Run, selection Selection) (Run, int, error) {
	cases := make(map[string]Case, len(corpus.Cases))
	for _, c := range corpus.Cases {
		if (selection.Split == "" || c.Split == selection.Split) && selectedValue(c.Corpus, selection.Corpora) && selectedValue(c.Family, selection.Families) {
			cases[c.ID] = c
		}
	}
	out := run
	out.Results = append([]QueryResult(nil), run.Results...)
	projected := 0
	for i := range out.Results {
		c, selected := cases[out.Results[i].ID]
		if !selected {
			continue
		}
		result, count, err := projectResultJudgmentIdentities(c, out.Results[i])
		if err != nil {
			return Run{}, 0, fmt.Errorf("result %s: %w", out.Results[i].ID, err)
		}
		out.Results[i] = result
		projected += count
	}
	return out, projected, nil
}

func projectResultJudgmentIdentities(c Case, result QueryResult) (QueryResult, int, error) {
	identitiesByAlias := map[string]map[string]struct{}{}
	unknownIdentityByAlias := map[string]bool{}
	for _, candidate := range result.Candidates {
		identity := strings.TrimSpace(candidate.Identity)
		if identity == "" {
			unknownIdentityByAlias[candidate.Source] = true
			continue
		}
		if identitiesByAlias[candidate.Source] == nil {
			identitiesByAlias[candidate.Source] = map[string]struct{}{}
		}
		identitiesByAlias[candidate.Source][identity] = struct{}{}
	}
	replacements := map[string]string{}
	for alias, identities := range identitiesByAlias {
		if _, fileJudged := c.Judgments[alias]; fileJudged {
			continue
		}
		var judged []string
		for identity := range identities {
			if _, ok := c.Judgments[identity]; ok {
				judged = append(judged, identity)
			}
		}
		if len(judged) == 0 {
			continue
		}
		if unknownIdentityByAlias[alias] || len(identities) != 1 || len(judged) != 1 {
			sort.Strings(judged)
			return QueryResult{}, 0, fmt.Errorf("ambiguous judgment identity projection for source %q across %d candidate identities (unknown=%t)", alias, len(identities), unknownIdentityByAlias[alias])
		}
		replacements[alias] = judged[0]
	}
	if len(replacements) == 0 {
		return result, 0, nil
	}
	replace := func(source string) string {
		if identity := replacements[source]; identity != "" {
			return identity
		}
		return source
	}
	out := result
	out.Sources = append([]string(nil), result.Sources...)
	out.MustRead = append([]string(nil), result.MustRead...)
	out.Roles = append([]RoleAssignment(nil), result.Roles...)
	out.Relationships = append([]EvidenceAssignment(nil), result.Relationships...)
	out.Facets = append([]EvidenceAssignment(nil), result.Facets...)
	out.Candidates = append([]CandidateTrace(nil), result.Candidates...)
	for i := range out.Sources {
		out.Sources[i] = replace(out.Sources[i])
	}
	for i := range out.MustRead {
		out.MustRead[i] = replace(out.MustRead[i])
	}
	for i := range out.Roles {
		out.Roles[i].Source = replace(out.Roles[i].Source)
	}
	for _, assignments := range [][]EvidenceAssignment{out.Relationships, out.Facets} {
		for i := range assignments {
			assignments[i].Source = replace(assignments[i].Source)
		}
	}
	for i := range out.Candidates {
		out.Candidates[i].Source = replace(out.Candidates[i].Source)
	}
	if duplicate := firstDuplicate(out.Sources); duplicate != "" {
		return QueryResult{}, 0, fmt.Errorf("judgment identity projection repeats source %q", duplicate)
	}
	if duplicate := firstDuplicate(out.MustRead); duplicate != "" {
		return QueryResult{}, 0, fmt.Errorf("judgment identity projection repeats must-read source %q", duplicate)
	}
	return out, len(replacements), nil
}

func sameExecutionCorpusContract(a, b Corpus) bool {
	strip := func(c Corpus) Corpus {
		out := c
		out.Cases = append([]Case(nil), c.Cases...)
		for i := range out.Cases {
			out.Cases[i].Judgments = nil
			out.Cases[i].JudgmentMethod = ""
			out.Cases[i].JudgedAt = ""
		}
		return out
	}
	aJSON, aErr := json.Marshal(strip(a))
	bJSON, bErr := json.Marshal(strip(b))
	return aErr == nil && bErr == nil && string(aJSON) == string(bJSON)
}

func EvaluateSelection(corpus Corpus, run Run, selection Selection) (Report, error) {
	fingerprint, err := validateCorpus(corpus)
	if err != nil {
		return Report{}, err
	}
	if run.CorpusFingerprint == "" || run.CorpusFingerprint != fingerprint {
		return Report{}, fmt.Errorf("corpus fingerprint mismatch: run=%q corpus=%q", run.CorpusFingerprint, fingerprint)
	}
	selected := make([]Case, 0, len(corpus.Cases))
	for _, c := range corpus.Cases {
		if (selection.Split == "" || c.Split == selection.Split) && selectedValue(c.Corpus, selection.Corpora) && selectedValue(c.Family, selection.Families) {
			selected = append(selected, c)
		}
	}
	if len(selected) == 0 {
		return Report{}, fmt.Errorf("evaluation selection has no cases")
	}
	results, err := validateRun(run, selected)
	if err != nil {
		return Report{}, err
	}
	report := Report{Revision: run.Revision, CorpusFingerprint: fingerprint, Mode: run.Mode, Profile: run.Profile, Split: selection.Split, RunFailureCount: run.FailureCount, ByCorpus: map[string]Metrics{}, ByFamily: map[string]Metrics{}}
	all := make([]scoredCase, 0, len(selected))
	byCorpus := map[string][]scoredCase{}
	byFamily := map[string][]scoredCase{}
	for _, c := range selected {
		result, ok := results[c.ID]
		if !ok {
			report.Failures = append(report.Failures, Failure{ID: c.ID, Stage: "runner", Reason: "missing result"})
		}
		s := scoreCase(c, result)
		all = append(all, s)
		byCorpus[c.Corpus] = append(byCorpus[c.Corpus], s)
		byFamily[c.Family] = append(byFamily[c.Family], s)
		if s.unjudged > 0 {
			report.Failures = append(report.Failures, Failure{ID: c.ID, Stage: "judgment", Reason: fmt.Sprintf("%d unjudged sources in top 10", s.unjudged)})
		}
		if c.Control == "no_answer" && result.Confidence == "high" {
			report.Failures = append(report.Failures, Failure{ID: c.ID, Stage: "confidence", Reason: "no-answer control reported high confidence"})
		}
		if result.ContinuationError != "" {
			report.Failures = append(report.Failures, Failure{ID: c.ID, Stage: "continuation", Reason: result.ContinuationError})
		}
		if ok {
			report.Failures = append(report.Failures, caseFailures(c, s)...)
		}
	}
	report.Overall = aggregate(all)
	for k, v := range byCorpus {
		report.ByCorpus[k] = aggregate(v)
	}
	for k, v := range byFamily {
		report.ByFamily[k] = aggregate(v)
	}
	sort.Slice(report.Failures, func(i, j int) bool {
		if report.Failures[i].ID != report.Failures[j].ID {
			return report.Failures[i].ID < report.Failures[j].ID
		}
		return report.Failures[i].Stage < report.Failures[j].Stage
	})
	return report, nil
}

func caseFailures(c Case, scored scoredCase) []Failure {
	var failures []Failure
	add := func(stage, reason string) {
		failures = append(failures, Failure{ID: c.ID, Stage: stage, Reason: reason})
	}
	if scored.recallEligible && scored.recall < 1 {
		add("source_selection", fmt.Sprintf("required source recall@20 %.3f", scored.recall))
	}
	if scored.navEligible && scored.nav1 < 1 {
		add("navigation", fmt.Sprintf("required source first appears after rank 1; success@3 %.3f", scored.nav3))
	}
	if scored.mustReadEligible && scored.mustRead < 1 {
		add("answer_selection", fmt.Sprintf("must-read precision %.3f", scored.mustRead))
	}
	if scored.rolesEligible && scored.roles < 1 {
		add("role_coverage", fmt.Sprintf("required role coverage %.3f", scored.roles))
	}
	if scored.relationshipsEligible && scored.relationships < 1 {
		add("relationship_coverage", fmt.Sprintf("required relationship coverage %.3f", scored.relationships))
	}
	if scored.facetsEligible && scored.facets < 1 {
		add("facet_coverage", fmt.Sprintf("required facet coverage %.3f", scored.facets))
	}
	if scored.primaryEligible && scored.primaryPurity < 1 {
		add("primary_result_purity", fmt.Sprintf("correct target and relationship among primary results %.3f", scored.primaryPurity))
	}
	if scored.high && !scored.highCorrect && c.Control != "no_answer" {
		add("confidence", "high confidence lacks fully relevant, covered, and resolved evidence")
	}
	return failures
}

func selectedValue(value string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateCorpus(c Corpus) (string, error) {
	if c.Version == "" || c.SourceRevision == "" {
		return "", fmt.Errorf("corpus version and sourceRevision are required")
	}
	seen := map[string]struct{}{}
	for _, x := range c.Cases {
		if x.ID == "" {
			return "", fmt.Errorf("corpus case id is required")
		}
		if _, ok := seen[x.ID]; ok {
			return "", fmt.Errorf("duplicate corpus case id %q", x.ID)
		}
		seen[x.ID] = struct{}{}
		for source, j := range x.Judgments {
			if source == "" || j.Grade < 0 || j.Grade > 3 {
				return "", fmt.Errorf("case %s has invalid judgment for %q", x.ID, source)
			}
		}
	}
	if c.Protocol == "search-engine-excellence-v1" {
		if err := validateExcellenceComposition(c.Cases); err != nil {
			return "", err
		}
	}
	return Fingerprint(c)
}

func validateExcellenceComposition(cases []Case) error {
	wantFamilies := map[string]int{
		"navigation":           64,
		"structural_precision": 64,
		"conceptual_discovery": 96,
		"multi_facet":          48,
		"control":              48,
	}
	if len(cases) < 320 {
		return fmt.Errorf("excellence corpus has %d cases; need at least 320", len(cases))
	}
	byCorpus, byFamily, bySplit := map[string]int{}, map[string]int{}, map[string]int{}
	noAnswer := 0
	for _, c := range cases {
		byCorpus[c.Corpus]++
		byFamily[c.Family]++
		bySplit[c.Split]++
		if c.Control == "no_answer" {
			noAnswer++
		}
		if c.JudgmentMethod == "" || c.JudgedAt == "" {
			return fmt.Errorf("case %s lacks judgment provenance", c.ID)
		}
	}
	if len(byCorpus) != 4 {
		return fmt.Errorf("excellence corpus has %d corpus families; need 4", len(byCorpus))
	}
	for corpus, count := range byCorpus {
		if count < 80 {
			return fmt.Errorf("corpus %s has %d cases; need at least 80", corpus, count)
		}
	}
	for family, want := range wantFamilies {
		if byFamily[family] < want {
			return fmt.Errorf("family %s has %d cases; need at least %d", family, byFamily[family], want)
		}
	}
	if noAnswer < 24 {
		return fmt.Errorf("excellence corpus has %d no-answer controls; need at least 24", noAnswer)
	}
	if bySplit["development"] == 0 || bySplit["held-out"] == 0 {
		return fmt.Errorf("excellence corpus requires development and held-out splits")
	}
	return nil
}

func validateRun(run Run, selected []Case) (map[string]QueryResult, error) {
	allowed := map[string]struct{}{}
	for _, c := range selected {
		allowed[c.ID] = struct{}{}
	}
	out := map[string]QueryResult{}
	for _, r := range run.Results {
		if _, ok := allowed[r.ID]; !ok {
			continue
		}
		if _, ok := out[r.ID]; ok {
			return nil, fmt.Errorf("duplicate result id %q", r.ID)
		}
		if d := firstDuplicate(r.Sources); d != "" {
			return nil, fmt.Errorf("result %s repeats source %q", r.ID, d)
		}
		if d := firstDuplicate(r.MustRead); d != "" {
			return nil, fmt.Errorf("result %s repeats must-read source %q", r.ID, d)
		}
		sources := map[string]struct{}{}
		for _, s := range r.Sources {
			sources[s] = struct{}{}
		}
		for _, s := range r.MustRead {
			if _, ok := sources[s]; !ok {
				return nil, fmt.Errorf("result %s must-read source %q is absent from sources", r.ID, s)
			}
		}
		for _, a := range r.Roles {
			if a.Role == "" || a.Source == "" {
				return nil, fmt.Errorf("result %s has incomplete role assignment", r.ID)
			}
			if _, ok := sources[a.Source]; !ok {
				return nil, fmt.Errorf("result %s role source %q is absent from sources", r.ID, a.Source)
			}
		}
		for _, assignments := range [][]EvidenceAssignment{r.Relationships, r.Facets} {
			for _, assignment := range assignments {
				if assignment.Value == "" || assignment.Source == "" {
					return nil, fmt.Errorf("result %s has incomplete evidence assignment", r.ID)
				}
				if _, ok := sources[assignment.Source]; !ok {
					return nil, fmt.Errorf("result %s evidence source %q is absent from sources", r.ID, assignment.Source)
				}
			}
		}
		for _, candidate := range r.Candidates {
			if candidate.Source == "" {
				return nil, fmt.Errorf("result %s has candidate without source", r.ID)
			}
			if _, ok := sources[candidate.Source]; !ok {
				return nil, fmt.Errorf("result %s candidate source %q is absent from sources", r.ID, candidate.Source)
			}
			if candidate.Eligibility != "" && candidate.Eligibility != "primary" && candidate.Eligibility != "supporting" {
				return nil, fmt.Errorf("result %s candidate source %q has invalid eligibility %q", r.ID, candidate.Source, candidate.Eligibility)
			}
		}
		out[r.ID] = r
	}
	return out, nil
}

func firstDuplicate(v []string) string {
	seen := map[string]struct{}{}
	for _, x := range v {
		if _, ok := seen[x]; ok {
			return x
		}
		seen[x] = struct{}{}
	}
	return ""
}
